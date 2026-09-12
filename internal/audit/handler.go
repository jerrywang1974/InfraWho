package audit

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/jerrywang1974/InfraWho/internal/auth"
)

// Handler serves /api/v1/audit-events.
type Handler struct {
	store      *Store
	trustProxy bool
}

// Options configures the audit list handler.
type Options struct {
	TrustProxy bool
}

func NewHandler(store *Store, opts Options) *Handler {
	return &Handler{store: store, trustProxy: opts.TrustProxy}
}

type listResponse struct {
	Items  []Event `json:"items"`
	Limit  int     `json:"limit"`
	Offset int     `json:"offset"`
	Total  int     `json:"total"`
}

// Register mounts audit routes. Admin-only list.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.Handle("GET /api/v1/audit-events", auth.RequireRole(auth.RoleAdmin, http.HandlerFunc(h.List)))
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	items, total, err := h.store.List(ListFilter{
		Limit:   limit,
		Offset:  offset,
		Action:  strings.TrimSpace(q.Get("action")),
		ActorID: strings.TrimSpace(q.Get("actor_id")),
		Outcome: strings.TrimSpace(q.Get("outcome")),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Could not list audit events")
		return
	}
	limit, offset = clampLimitOffset(limit, offset)
	writeJSON(w, http.StatusOK, listResponse{
		Items:  items,
		Limit:  limit,
		Offset: offset,
		Total:  total,
	})
}

type errorBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	var body errorBody
	body.Error.Code = code
	body.Error.Message = message
	writeJSON(w, status, body)
}
