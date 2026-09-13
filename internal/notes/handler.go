package notes

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/jerrywang1974/InfraWho/internal/auth"
)

// Handler serves nested asset notes and /api/v1/notes/{id} endpoints.
type Handler struct {
	store *Store
}

func NewHandler(store *Store) *Handler {
	return &Handler{store: store}
}

type createRequest struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

type patchRequest struct {
	Title *string `json:"title"`
	Body  *string `json:"body"`
}

var allowedPatchKeys = map[string]struct{}{
	"title": {}, "body": {},
}

func (p *patchRequest) hasFieldUpdates() bool {
	return p.Title != nil || p.Body != nil
}

type listResponse struct {
	Items  []Note `json:"items"`
	Limit  int    `json:"limit"`
	Offset int    `json:"offset"`
	Total  int    `json:"total"`
}

// Register mounts note routes on mux. wrapOrigin should apply CSRF Origin checks.
func (h *Handler) Register(mux *http.ServeMux, wrapOrigin func(http.Handler) http.Handler) {
	mux.Handle("GET /api/v1/assets/{id}/notes", auth.RequireAuth(http.HandlerFunc(h.List)))
	mux.Handle("POST /api/v1/assets/{id}/notes", wrapOrigin(auth.RequireRole(auth.RoleOperator, http.HandlerFunc(h.Create))))
	mux.Handle("PATCH /api/v1/notes/{id}", wrapOrigin(auth.RequireRole(auth.RoleOperator, http.HandlerFunc(h.Patch))))
	mux.Handle("DELETE /api/v1/notes/{id}", wrapOrigin(auth.RequireRole(auth.RoleOperator, http.HandlerFunc(h.Delete))))
}

func (h *Handler) actorID(r *http.Request) *string {
	if u, ok := auth.UserFromContext(r.Context()); ok {
		return &u.ID
	}
	return nil
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	assetID := r.PathValue("id")
	if assetID == "" {
		writeError(w, http.StatusBadRequest, "validation_error", "Asset id is required")
		return
	}
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	limit, offset = clampLimitOffset(limit, offset)

	items, total, err := h.store.List(assetID, limit, offset)
	if err != nil {
		if errors.Is(err, ErrAssetNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "Asset not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "Could not list notes")
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
	n, err := h.store.Create(assetID, &req, h.actorID(r), time.Now().UTC())
	if err != nil {
		switch {
		case errors.Is(err, ErrAssetNotFound):
			writeError(w, http.StatusNotFound, "not_found", "Asset not found")
		case errors.Is(err, ErrAssetDeleted):
			writeError(w, http.StatusConflict, "conflict", "Asset is soft-deleted")
		default:
			writeError(w, http.StatusInternalServerError, "internal_error", "Could not create note")
		}
		return
	}
	writeJSON(w, http.StatusCreated, n)
}

func (h *Handler) Patch(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "validation_error", "Note id is required")
		return
	}

	var raw map[string]json.RawMessage
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(&raw); err != nil {
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

	n, err := h.store.Update(id, &req, time.Now().UTC())
	if err != nil {
		switch {
		case errors.Is(err, ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "Note not found")
		case errors.Is(err, ErrAssetDeleted):
			writeError(w, http.StatusConflict, "conflict", "Asset is soft-deleted")
		case errors.Is(err, ErrAssetNotFound):
			writeError(w, http.StatusNotFound, "not_found", "Asset not found")
		default:
			writeError(w, http.StatusInternalServerError, "internal_error", "Could not update note")
		}
		return
	}
	writeJSON(w, http.StatusOK, n)
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "validation_error", "Note id is required")
		return
	}
	if err := h.store.Delete(id); err != nil {
		switch {
		case errors.Is(err, ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "Note not found")
		case errors.Is(err, ErrAssetDeleted):
			writeError(w, http.StatusConflict, "conflict", "Asset is soft-deleted")
		case errors.Is(err, ErrAssetNotFound):
			writeError(w, http.StatusNotFound, "not_found", "Asset not found")
		default:
			writeError(w, http.StatusInternalServerError, "internal_error", "Could not delete note")
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
