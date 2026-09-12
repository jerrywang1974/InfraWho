package accounts

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/jerrywang1974/InfraWho/internal/audit"
	"github.com/jerrywang1974/InfraWho/internal/auth"
	"github.com/jerrywang1974/InfraWho/internal/ratelimit"
)

// Handler serves account and reveal endpoints.
type Handler struct {
	store      *Store
	audit      *audit.Store
	limiter    *ratelimit.Limiter
	trustProxy bool
}

// Options configures the accounts handler.
type Options struct {
	TrustProxy bool
	Limiter    *ratelimit.Limiter
}

func NewHandler(store *Store, auditStore *audit.Store, opts Options) *Handler {
	lim := opts.Limiter
	if lim == nil {
		lim = ratelimit.New()
	}
	return &Handler{
		store:      store,
		audit:      auditStore,
		limiter:    lim,
		trustProxy: opts.TrustProxy,
	}
}

var allowedPatchKeys = map[string]struct{}{
	"username": {}, "auth_type": {}, "description": {},
}

type revealResponse struct {
	AccountID  string `json:"account_id"`
	Username   string `json:"username"`
	AuthType   string `json:"auth_type"`
	Secret     string `json:"secret"`
	RevealedAt string `json:"revealed_at"`
}

// Register mounts account routes. wrapOrigin applies CSRF Origin checks.
func (h *Handler) Register(mux *http.ServeMux, wrapOrigin func(http.Handler) http.Handler) {
	mux.Handle("POST /api/v1/assets/{id}/accounts", wrapOrigin(auth.RequireRole(auth.RoleOperator, http.HandlerFunc(h.Create))))
	mux.Handle("PATCH /api/v1/accounts/{id}", wrapOrigin(auth.RequireRole(auth.RoleOperator, http.HandlerFunc(h.Patch))))
	mux.Handle("DELETE /api/v1/accounts/{id}", wrapOrigin(auth.RequireRole(auth.RoleOperator, http.HandlerFunc(h.Delete))))
	mux.Handle("POST /api/v1/accounts/{id}/reveal", wrapOrigin(auth.RequireRole(auth.RoleOperator, auth.RequireStepUp(http.HandlerFunc(h.Reveal)))))
	mux.Handle("POST /api/v1/accounts/{id}/rotate-secret", wrapOrigin(auth.RequireRole(auth.RoleOperator, http.HandlerFunc(h.RotateSecret))))
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

func (h *Handler) auditBestEffort(r *http.Request, action, outcome string, resourceID *string) {
	in := h.auditBase(r)
	in.Action = action
	in.ResourceType = "account"
	in.ResourceID = resourceID
	in.Outcome = outcome
	if err := h.audit.Write(in); err != nil {
		log.Printf("accounts: audit write failed action=%s: %v", action, err)
	}
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	assetID := r.PathValue("id")
	if assetID == "" {
		writeError(w, http.StatusBadRequest, "validation_error", "Asset id is required")
		return
	}
	var req createRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "validation_error", "Invalid JSON body")
		return
	}
	if err := validateCreate(&req); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "validation_error", err.Error())
		return
	}
	a, err := h.store.Create(assetID, &req, time.Now().UTC())
	if err != nil {
		switch {
		case errors.Is(err, ErrAssetNotFound):
			writeError(w, http.StatusNotFound, "not_found", "Asset not found")
		case errors.Is(err, ErrAssetDeleted):
			writeError(w, http.StatusConflict, "conflict", "Asset is soft-deleted")
		case errors.Is(err, ErrKEKUnavailable):
			writeError(w, http.StatusServiceUnavailable, "not_ready", "Master key unavailable")
		default:
			log.Printf("accounts: create failed asset=%s: %v", assetID, err)
			writeError(w, http.StatusInternalServerError, "internal_error", "Could not create account")
		}
		return
	}
	h.auditBestEffort(r, audit.ActionAccountCreate, audit.OutcomeSuccess, &a.ID)
	writeJSON(w, http.StatusCreated, a)
}

func (h *Handler) Patch(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "validation_error", "Account id is required")
		return
	}

	var raw map[string]json.RawMessage
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "validation_error", "Invalid JSON body")
		return
	}
	if len(raw) == 0 {
		writeError(w, http.StatusUnprocessableEntity, "validation_error", "PATCH body must include at least one field")
		return
	}
	for k := range raw {
		if _, ok := allowedPatchKeys[k]; !ok {
			writeError(w, http.StatusUnprocessableEntity, "validation_error", "Unknown field: "+k)
			return
		}
	}

	var req patchRequest
	b, err := json.Marshal(raw)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "validation_error", "Invalid JSON body")
		return
	}
	if err := json.Unmarshal(b, &req); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "validation_error", "Invalid JSON body")
		return
	}
	if err := validatePatch(&req); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "validation_error", err.Error())
		return
	}
	if !req.hasFieldUpdates() {
		writeError(w, http.StatusUnprocessableEntity, "validation_error", "PATCH body must include at least one updatable field")
		return
	}

	a, err := h.store.Update(id, &req, time.Now().UTC())
	if err != nil {
		switch {
		case errors.Is(err, ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "Account not found")
		case errors.Is(err, ErrAuthTypeImmutable):
			writeError(w, http.StatusUnprocessableEntity, "validation_error", err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "internal_error", "Could not update account")
		}
		return
	}
	h.auditBestEffort(r, audit.ActionAccountUpdate, audit.OutcomeSuccess, &a.ID)
	writeJSON(w, http.StatusOK, a)
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "validation_error", "Account id is required")
		return
	}
	if err := h.store.Delete(id, h.auditBase(r)); err != nil {
		switch {
		case errors.Is(err, ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "Account not found")
		default:
			log.Printf("accounts: delete failed id=%s: %v", id, err)
			writeError(w, http.StatusInternalServerError, "internal_error", "Could not delete account")
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) RotateSecret(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "validation_error", "Account id is required")
		return
	}
	var req rotateRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "validation_error", "Invalid JSON body")
		return
	}
	if err := validateRotate(&req); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "validation_error", err.Error())
		return
	}
	a, err := h.store.RotateSecret(id, &req, time.Now().UTC(), h.auditBase(r))
	if err != nil {
		switch {
		case errors.Is(err, ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "Account not found")
		case errors.Is(err, ErrKEKUnavailable):
			writeError(w, http.StatusServiceUnavailable, "not_ready", "Master key unavailable")
		default:
			log.Printf("accounts: rotate-secret failed id=%s: %v", id, err)
			writeError(w, http.StatusInternalServerError, "internal_error", "Could not rotate secret")
		}
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func (h *Handler) Reveal(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "validation_error", "Account id is required")
		return
	}

	sess, ok := auth.SessionFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}

	if !h.limiter.Allow("reveal:session:"+sess.ID, auth.RevealSessionLimit, auth.RevealWindow) {
		in := h.auditBase(r)
		in.Action = audit.ActionRevealRateLimited
		in.ResourceType = "account"
		in.ResourceID = &id
		in.Outcome = audit.OutcomeDenied
		in.Metadata = `{"reason":"session_rate_limit"}`
		if err := h.audit.Write(in); err != nil {
			log.Printf("accounts: reveal rate-limit audit failed id=%s: %v", id, err)
			writeError(w, http.StatusInternalServerError, "internal_error", "Could not record audit event")
			return
		}
		writeError(w, http.StatusTooManyRequests, "rate_limited", "Reveal rate limit exceeded")
		return
	}

	plain, acc, err := h.store.Reveal(id)
	if err != nil {
		switch {
		case errors.Is(err, ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "Account not found")
		case errors.Is(err, ErrNoSecret):
			writeError(w, http.StatusNotFound, "not_found", "Account has no secret")
		case errors.Is(err, ErrKEKUnavailable):
			writeError(w, http.StatusServiceUnavailable, "not_ready", "Master key unavailable")
		default:
			log.Printf("accounts: reveal decrypt failed id=%s: %v", id, err)
			writeError(w, http.StatusInternalServerError, "internal_error", "Could not reveal secret")
		}
		return
	}

	in := h.auditBase(r)
	in.Action = audit.ActionCredentialReveal
	in.ResourceType = "account"
	in.ResourceID = &id
	in.Outcome = audit.OutcomeSuccess
	if err := h.audit.Write(in); err != nil {
		log.Printf("accounts: reveal audit failed id=%s: %v", id, err)
		writeError(w, http.StatusInternalServerError, "internal_error", "Could not record audit event")
		return
	}

	now := time.Now().UTC()
	writeJSON(w, http.StatusOK, revealResponse{
		AccountID:  acc.ID,
		Username:   acc.Username,
		AuthType:   acc.AuthType,
		Secret:     string(plain),
		RevealedAt: now.Format(time.RFC3339),
	})
}
