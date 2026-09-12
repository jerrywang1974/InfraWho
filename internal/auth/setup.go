package auth

import (
	"net/http"
	"strings"
	"time"
)

type setupStatusResponse struct {
	NeedsBootstrap    bool `json:"needs_bootstrap"`
	MasterKeyReady    bool `json:"master_key_ready"`
	ChecklistComplete bool `json:"checklist_complete"`
	ShowBanner        bool `json:"show_banner"`
}

type bootstrapRequest struct {
	Username                    string `json:"username"`
	Password                    string `json:"password"`
	DisplayName                 string `json:"display_name"`
	AcknowledgeKEKOffline       bool   `json:"acknowledge_kek_offline"`
	AcknowledgeKEKIrrecoverable bool   `json:"acknowledge_kek_irrecoverable"`
	AcknowledgeBackupPlanned    bool   `json:"acknowledge_backup_planned"`
}

type acknowledgeRequest struct {
	AcknowledgeKEKOffline       bool `json:"acknowledge_kek_offline"`
	AcknowledgeKEKIrrecoverable bool `json:"acknowledge_kek_irrecoverable"`
	AcknowledgeBackupPlanned    bool `json:"acknowledge_backup_planned"`
}

func (h *Handler) SetupStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}
	n, err := h.store.UserCount()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Could not read setup status")
		return
	}
	complete, err := h.store.ChecklistComplete()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Could not read setup status")
		return
	}
	writeJSON(w, http.StatusOK, setupStatusResponse{
		NeedsBootstrap:    n == 0,
		MasterKeyReady:    h.masterKeyReady(),
		ChecklistComplete: complete,
		ShowBanner:        !complete,
	})
}

func (h *Handler) Bootstrap(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}
	if !CheckOrigin(r, h.trustedOrigins) {
		writeError(w, http.StatusForbidden, "forbidden", "Origin check failed")
		return
	}
	n, err := h.store.UserCount()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Bootstrap failed")
		return
	}
	if n > 0 {
		writeError(w, http.StatusConflict, "conflict", "Already bootstrapped")
		return
	}
	if !h.masterKeyReady() {
		writeError(w, http.StatusServiceUnavailable, "not_ready", "Master key is required before setup can complete")
		return
	}

	var req bootstrapRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "validation_error", "Invalid JSON body")
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	req.DisplayName = strings.TrimSpace(req.DisplayName)
	if req.Username == "" {
		writeError(w, http.StatusBadRequest, "validation_error", "Username is required")
		return
	}
	if len(req.Password) < minPasswordLen {
		writeError(w, http.StatusBadRequest, "validation_error", "Password must be at least 8 characters")
		return
	}
	if !req.AcknowledgeKEKOffline || !req.AcknowledgeKEKIrrecoverable || !req.AcknowledgeBackupPlanned {
		writeError(w, http.StatusBadRequest, "validation_error", "All install checklist acknowledgements are required")
		return
	}

	hash, err := HashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Could not hash password")
		return
	}
	user, err := h.store.CreateUser(req.Username, req.DisplayName, RoleAdmin, hash)
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "conflict", "Username already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "Could not create admin user")
		return
	}
	now := time.Now().UTC()
	if err := h.store.SetChecklistComplete(now); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Could not persist checklist")
		return
	}

	raw, sess, err := h.store.CreateSession(user.ID, now)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Admin created but session failed")
		return
	}
	setSessionCookie(w, raw, h.cookieSecure, sess.AbsoluteExpiresAt)
	uid := user.ID
	_ = h.store.WriteAudit(&uid, "LOGIN", "system", nil, "success", clientIP(r), r.UserAgent(), `{"bootstrap":true}`)
	writeJSON(w, http.StatusCreated, meResponse{
		ID:           user.ID,
		Username:     user.Username,
		DisplayName:  user.DisplayName,
		Role:         string(user.Role),
		StepUpActive: false,
	})
}

func (h *Handler) Acknowledge(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}
	if !CheckOrigin(r, h.trustedOrigins) {
		writeError(w, http.StatusForbidden, "forbidden", "Origin check failed")
		return
	}
	u, ok := UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
		return
	}
	if u.Role != RoleAdmin {
		writeError(w, http.StatusForbidden, "forbidden", "Admin role required")
		return
	}
	if !h.masterKeyReady() {
		writeError(w, http.StatusServiceUnavailable, "not_ready", "Master key is required before setup can complete")
		return
	}
	var req acknowledgeRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "validation_error", "Invalid JSON body")
		return
	}
	if !req.AcknowledgeKEKOffline || !req.AcknowledgeKEKIrrecoverable || !req.AcknowledgeBackupPlanned {
		writeError(w, http.StatusBadRequest, "validation_error", "All install checklist acknowledgements are required")
		return
	}
	if err := h.store.SetChecklistComplete(time.Now().UTC()); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Could not persist checklist")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique") || strings.Contains(msg, "constraint")
}
