package assets

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jerrywang1974/InfraWho/internal/auth"
)

// Handler serves /api/v1/assets endpoints.
type Handler struct {
	store      *Store
	trustProxy bool
}

// Options configures the assets handler.
type Options struct {
	TrustProxy bool
}

func NewHandler(store *Store, opts Options) *Handler {
	return &Handler{store: store, trustProxy: opts.TrustProxy}
}

type createRequest struct {
	Name          string   `json:"name"`
	Hostname      string   `json:"hostname"`
	AssetType     string   `json:"asset_type"`
	OSFamily      string   `json:"os_family"`
	OSDetail      string   `json:"os_detail"`
	Environment   string   `json:"environment"`
	Purpose       string   `json:"purpose"`
	PrimaryIP     string   `json:"primary_ip"`
	AdditionalIPs []string `json:"additional_ips"`
	Location      string   `json:"location"`
	Hypervisor    string   `json:"hypervisor"`
	OwnerID       *string  `json:"owner_id"`
	BackupOwnerID *string  `json:"backup_owner_id"`
	Status        string   `json:"status"`
	ConfigNotes   string   `json:"config_notes"`
	Tags          []string `json:"tags"`
}

type patchRequest struct {
	Name             *string          `json:"name"`
	Hostname         *string          `json:"hostname"`
	AssetType        *string          `json:"asset_type"`
	OSFamily         *string          `json:"os_family"`
	OSDetail         *string          `json:"os_detail"`
	Environment      *string          `json:"environment"`
	Purpose          *string          `json:"purpose"`
	PrimaryIP        *string          `json:"primary_ip"`
	AdditionalIPs    *[]string        `json:"additional_ips"`
	Location         *string          `json:"location"`
	Hypervisor       *string          `json:"hypervisor"`
	OwnerID          *string          `json:"owner_id"`
	BackupOwnerID    *string          `json:"backup_owner_id"`
	Status           *string          `json:"status"`
	ConfigNotes      *string          `json:"config_notes"`
	Tags             *[]string        `json:"tags"`
	DeletedAt        *json.RawMessage `json:"deleted_at"`
	OwnerIDSet       bool             `json:"-"`
	BackupOwnerIDSet bool             `json:"-"`
}

type listResponse struct {
	Items  []Asset `json:"items"`
	Limit  int     `json:"limit"`
	Offset int     `json:"offset"`
	Total  int     `json:"total"`
}

// Register mounts asset routes on mux. wrapOrigin should apply CSRF Origin checks.
func (h *Handler) Register(mux *http.ServeMux, wrapOrigin func(http.Handler) http.Handler) {
	mux.Handle("GET /api/v1/assets", auth.RequireAuth(http.HandlerFunc(h.List)))
	mux.Handle("POST /api/v1/assets", wrapOrigin(auth.RequireRole(auth.RoleOperator, http.HandlerFunc(h.Create))))
	mux.Handle("GET /api/v1/assets/{id}", auth.RequireAuth(http.HandlerFunc(h.Get)))
	mux.Handle("PATCH /api/v1/assets/{id}", wrapOrigin(auth.RequireRole(auth.RoleOperator, http.HandlerFunc(h.Patch))))
	mux.Handle("DELETE /api/v1/assets/{id}", wrapOrigin(auth.RequireRole(auth.RoleAdmin, auth.RequireStepUp(http.HandlerFunc(h.SoftDelete)))))
	mux.Handle("POST /api/v1/assets/{id}/purge", wrapOrigin(auth.RequireRole(auth.RoleAdmin, auth.RequireStepUp(http.HandlerFunc(h.Purge)))))
}

func (h *Handler) ip(r *http.Request) string {
	return clientIP(r, h.trustProxy)
}

func (h *Handler) audit(r *http.Request, action, outcome string, resourceID *string) {
	var actorID *string
	if u, ok := auth.UserFromContext(r.Context()); ok {
		actorID = &u.ID
	}
	_ = h.store.WriteAudit(actorID, action, "asset", resourceID, outcome, h.ip(r), r.UserAgent(), "{}")
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	limit, offset = clampLimitOffset(limit, offset)

	includeDeleted := false
	switch strings.ToLower(strings.TrimSpace(q.Get("include_deleted"))) {
	case "1", "true", "yes":
		includeDeleted = true
	}

	items, total, err := h.store.List(ListFilter{
		Limit:          limit,
		Offset:         offset,
		Tag:            strings.TrimSpace(q.Get("tag")),
		Environment:    strings.TrimSpace(q.Get("environment")),
		Status:         strings.TrimSpace(q.Get("status")),
		Q:              q.Get("q"),
		IncludeDeleted: includeDeleted,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Could not list assets")
		return
	}
	writeJSON(w, http.StatusOK, listResponse{
		Items:  items,
		Limit:  limit,
		Offset: offset,
		Total:  total,
	})
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "validation_error", "Invalid JSON body")
		return
	}
	if err := validateCreate(&req); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "validation_error", err.Error())
		return
	}
	a, err := h.store.Create(&req, time.Now().UTC())
	if err != nil {
		if errors.Is(err, ErrConflict) {
			writeError(w, http.StatusConflict, "conflict", "Hostname already exists")
			return
		}
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusUnprocessableEntity, "validation_error", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "Could not create asset")
		return
	}
	h.audit(r, "ASSET_CREATE", "success", &a.ID)
	writeJSON(w, http.StatusCreated, a)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "validation_error", "Asset id is required")
		return
	}
	d, err := h.store.GetDetail(id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "Asset not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "Could not load asset")
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (h *Handler) Patch(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "validation_error", "Asset id is required")
		return
	}

	var raw map[string]json.RawMessage
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&raw); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "validation_error", "Invalid JSON body")
		return
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
	if _, ok := raw["owner_id"]; ok {
		req.OwnerIDSet = true
	}
	if _, ok := raw["backup_owner_id"]; ok {
		req.BackupOwnerIDSet = true
	}
	if _, ok := raw["deleted_at"]; ok {
		req.DeletedAt = new(json.RawMessage)
		v := raw["deleted_at"]
		req.DeletedAt = &v
	}

	if err := validatePatch(&req); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "validation_error", err.Error())
		return
	}

	a, err := h.store.Update(id, &req, time.Now().UTC())
	if err != nil {
		switch {
		case errors.Is(err, ErrNotFound):
			if strings.Contains(err.Error(), "owner") {
				writeError(w, http.StatusUnprocessableEntity, "validation_error", err.Error())
				return
			}
			writeError(w, http.StatusNotFound, "not_found", "Asset not found")
		case errors.Is(err, ErrAlreadyDeleted):
			writeError(w, http.StatusConflict, "conflict", "Asset is soft-deleted")
		case errors.Is(err, ErrConflict):
			writeError(w, http.StatusConflict, "conflict", "Hostname already exists")
		default:
			writeError(w, http.StatusInternalServerError, "internal_error", "Could not update asset")
		}
		return
	}
	h.audit(r, "ASSET_UPDATE", "success", &a.ID)
	writeJSON(w, http.StatusOK, a)
}

func (h *Handler) SoftDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "validation_error", "Asset id is required")
		return
	}
	a, err := h.store.SoftDelete(id, time.Now().UTC())
	if err != nil {
		switch {
		case errors.Is(err, ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "Asset not found")
		case errors.Is(err, ErrAlreadyDeleted):
			writeError(w, http.StatusConflict, "conflict", "Asset is already soft-deleted")
		default:
			writeError(w, http.StatusInternalServerError, "internal_error", "Could not soft-delete asset")
		}
		return
	}
	h.audit(r, "ASSET_SOFT_DELETE", "success", &a.ID)
	writeJSON(w, http.StatusOK, a)
}

func (h *Handler) Purge(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "validation_error", "Asset id is required")
		return
	}
	if err := h.store.Purge(id); err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "Asset not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "Could not purge asset")
		return
	}
	h.audit(r, "ASSET_PURGE", "success", &id)
	w.WriteHeader(http.StatusNoContent)
}
