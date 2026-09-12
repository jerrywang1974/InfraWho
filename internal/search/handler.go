package search

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/jerrywang1974/InfraWho/internal/auth"
)

// Handler serves GET /api/v1/search.
type Handler struct {
	store *Store
}

func NewHandler(store *Store) *Handler {
	return &Handler{store: store}
}

type listResponse struct {
	Items  []Hit `json:"items"`
	Limit  int   `json:"limit"`
	Offset int   `json:"offset"`
	Total  int   `json:"total"`
}

// Register mounts search routes. wrapOrigin unused (read-only) but kept for symmetry.
func (h *Handler) Register(mux *http.ServeMux, _ func(http.Handler) http.Handler) {
	mux.Handle("GET /api/v1/search", auth.RequireAuth(http.HandlerFunc(h.Search)))
}

func (h *Handler) Search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	limit, offset = clampLimitOffset(limit, offset)

	includeDeleted := false
	switch strings.ToLower(strings.TrimSpace(q.Get("include_deleted"))) {
	case "1", "true", "yes":
		includeDeleted = true
	}

	items, total, err := h.store.SearchAssets(Filter{
		Q:              q.Get("q"),
		Limit:          limit,
		Offset:         offset,
		Tag:            strings.TrimSpace(q.Get("tag")),
		Environment:    strings.TrimSpace(q.Get("environment")),
		Status:         strings.TrimSpace(q.Get("status")),
		IncludeDeleted: includeDeleted,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Could not search")
		return
	}
	writeJSON(w, http.StatusOK, listResponse{
		Items:  items,
		Limit:  limit,
		Offset: offset,
		Total:  total,
	})
}
