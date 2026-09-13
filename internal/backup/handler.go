package backup

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/jerrywang1974/InfraWho/internal/audit"
	"github.com/jerrywang1974/InfraWho/internal/auth"
	"github.com/jerrywang1974/InfraWho/internal/ratelimit"
)

const (
	// ExportSecretsSessionLimit caps plaintext secret exports per session window.
	ExportSecretsSessionLimit = 5
	ExportSecretsWindow       = time.Minute
	// MaxImportBodyBytes caps portable import documents (32 MiB).
	MaxImportBodyBytes = 32 << 20
)

// Auditor records audit events.
type Auditor interface {
	Write(in audit.WriteInput) error
}

// Handler serves POST /api/v1/export and POST /api/v1/import.
type Handler struct {
	store                *Store
	audit                Auditor
	limiter              *ratelimit.Limiter
	trustProxy           bool
	featureExportSecrets bool
}

// Options configures the backup handler.
type Options struct {
	TrustProxy           bool
	FeatureExportSecrets bool
	Limiter              *ratelimit.Limiter
}

func NewHandler(store *Store, auditor Auditor, opts Options) *Handler {
	lim := opts.Limiter
	if lim == nil {
		lim = ratelimit.New()
	}
	return &Handler{
		store:                store,
		audit:                auditor,
		limiter:              lim,
		trustProxy:           opts.TrustProxy,
		featureExportSecrets: opts.FeatureExportSecrets,
	}
}

// Register mounts export/import routes. wrapOrigin applies CSRF Origin checks.
func (h *Handler) Register(mux *http.ServeMux, wrapOrigin func(http.Handler) http.Handler) {
	mux.Handle("POST /api/v1/export", wrapOrigin(auth.RequireRole(auth.RoleOperator, http.HandlerFunc(h.Export))))
	mux.Handle("POST /api/v1/import", wrapOrigin(auth.RequireRole(auth.RoleAdmin, http.HandlerFunc(h.Import))))
}

func (h *Handler) ip(r *http.Request) string {
	return clientIP(r, h.trustProxy)
}

func (h *Handler) actorID(r *http.Request) *string {
	if u, ok := auth.UserFromContext(r.Context()); ok {
		return &u.ID
	}
	return nil
}

func (h *Handler) auditBase(r *http.Request) audit.WriteInput {
	return audit.WriteInput{
		ActorID:   h.actorID(r),
		IP:        h.ip(r),
		UserAgent: r.UserAgent(),
	}
}

func (h *Handler) auditDeniedSecrets(r *http.Request, reason, code string) {
	in := h.auditBase(r)
	in.Action = audit.ActionExportWithSecrets
	in.ResourceType = "export"
	in.Outcome = audit.OutcomeDenied
	in.Metadata = `{"include_secrets":true,"reason":"` + reason + `","code":"` + code + `"}`
	if err := h.audit.Write(in); err != nil {
		log.Printf("backup: denied export audit failed reason=%s: %v", reason, err)
	}
}

// Export handles POST /api/v1/export (K18 — not GET).
func (h *Handler) Export(w http.ResponseWriter, r *http.Request) {
	var req exportRequest
	if r.Body != nil && r.ContentLength != 0 {
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		_ = r.Body.Close()
		if err != nil {
			writeError(w, http.StatusBadRequest, "validation_error", "Could not read body")
			return
		}
		trimmed := strings.TrimSpace(string(body))
		if trimmed != "" && trimmed != "null" {
			if err := decodeJSONBytes(trimmed, &req); err != nil {
				writeError(w, http.StatusUnprocessableEntity, "validation_error", "Invalid JSON body")
				return
			}
		}
	}
	if req.Format == "" {
		req.Format = "json"
	}
	if !strings.EqualFold(req.Format, "json") {
		writeError(w, http.StatusUnprocessableEntity, "validation_error", "format must be json")
		return
	}

	if req.IncludeSecrets {
		if !h.featureExportSecrets {
			h.auditDeniedSecrets(r, "feature_disabled", "feature_disabled")
			writeError(w, http.StatusForbidden, "feature_disabled", "Secret export is disabled")
			return
		}
		u, ok := auth.UserFromContext(r.Context())
		if !ok || !u.Role.CanExportSecrets() {
			h.auditDeniedSecrets(r, "forbidden", "forbidden")
			writeError(w, http.StatusForbidden, "forbidden", "Insufficient role")
			return
		}
		sess, ok := auth.SessionFromContext(r.Context())
		if !ok || !sess.StepUpActive(time.Now().UTC()) {
			h.auditDeniedSecrets(r, "step_up_required", "step_up_required")
			writeError(w, http.StatusForbidden, "step_up_required", "Step-up authentication required")
			return
		}

		limitKey := "export_secrets:session:" + sess.ID
		allowed, needDenyAudit := h.limiter.AllowReport(limitKey, ExportSecretsSessionLimit, ExportSecretsWindow)
		if !allowed {
			if needDenyAudit {
				in := h.auditBase(r)
				in.Action = audit.ActionExportWithSecrets
				in.ResourceType = "export"
				in.Outcome = audit.OutcomeDenied
				in.Metadata = `{"include_secrets":true,"reason":"session_rate_limit"}`
				if err := h.audit.Write(in); err != nil {
					log.Printf("backup: export rate-limit audit failed: %v", err)
					writeError(w, http.StatusInternalServerError, "internal_error", "Could not record audit event")
					return
				}
				h.limiter.ConfirmDeny(limitKey)
			}
			writeError(w, http.StatusTooManyRequests, "rate_limited", "Secret export rate limit exceeded")
			return
		}
	}

	doc, err := h.store.Export(req.IncludeSecrets, time.Now().UTC())
	if err != nil {
		switch {
		case errors.Is(err, ErrKEKUnavailable):
			writeError(w, http.StatusServiceUnavailable, "not_ready", "Master key unavailable")
		default:
			log.Printf("backup: export failed: %v", err)
			writeError(w, http.StatusInternalServerError, "internal_error", "Could not export")
		}
		return
	}

	in := h.auditBase(r)
	in.ResourceType = "export"
	in.Outcome = audit.OutcomeSuccess
	if req.IncludeSecrets {
		in.Action = audit.ActionExportWithSecrets
		in.Metadata = `{"include_secrets":true}`
	} else {
		in.Action = audit.ActionExportMetadata
		in.Metadata = `{"include_secrets":false}`
	}
	if err := h.audit.Write(in); err != nil {
		log.Printf("backup: export audit failed: %v", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "Could not record audit event")
		return
	}

	writeJSON(w, http.StatusOK, doc)
}

// Import handles POST /api/v1/import (hostname conflict = reject).
func (h *Handler) Import(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, MaxImportBodyBytes)
	var doc Document
	if err := decodeJSON(r, &doc); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) || errors.Is(err, io.ErrUnexpectedEOF) && r.ContentLength > MaxImportBodyBytes {
			writeError(w, http.StatusRequestEntityTooLarge, "payload_too_large", "Import body exceeds size limit")
			return
		}
		// MaxBytesReader returns an error whose message contains "http: request body too large".
		if strings.Contains(err.Error(), "request body too large") {
			writeError(w, http.StatusRequestEntityTooLarge, "payload_too_large", "Import body exceeds size limit")
			return
		}
		writeError(w, http.StatusUnprocessableEntity, "validation_error", "Invalid JSON body")
		return
	}
	if doc.Assets == nil {
		doc.Assets = []ExportAsset{}
	}

	hasSecrets := DocumentHasSecrets(&doc)
	if hasSecrets {
		sess, ok := auth.SessionFromContext(r.Context())
		if !ok || !sess.StepUpActive(time.Now().UTC()) {
			writeError(w, http.StatusForbidden, "step_up_required", "Step-up authentication required")
			return
		}
	}

	result, err := h.store.Import(&doc, time.Now().UTC(), h.auditBase(r))
	if err != nil {
		switch {
		case errors.Is(err, ErrHostnameConflict):
			writeError(w, http.StatusConflict, "hostname_conflict", err.Error())
		case errors.Is(err, ErrInvalidDocument):
			writeError(w, http.StatusUnprocessableEntity, "validation_error", err.Error())
		case errors.Is(err, ErrKEKUnavailable):
			writeError(w, http.StatusServiceUnavailable, "not_ready", "Master key unavailable")
		default:
			log.Printf("backup: import failed: %v", err)
			writeError(w, http.StatusInternalServerError, "internal_error", "Could not import")
		}
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func decodeJSONBytes(s string, dst any) error {
	dec := json.NewDecoder(strings.NewReader(s))
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}
