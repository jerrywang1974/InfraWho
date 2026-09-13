package audit

import (
	"database/sql"
	"time"

	"github.com/google/uuid"
)

const timeLayout = "2006-01-02T15:04:05.000Z"

// Action names used by Phase 1 writers.
const (
	ActionCredentialReveal  = "CREDENTIAL_REVEAL"
	ActionCredentialRotate  = "CREDENTIAL_ROTATE"
	ActionRevealRateLimited = "REVEAL_RATE_LIMITED"
	ActionAccountCreate     = "ACCOUNT_CREATE"
	ActionAccountUpdate     = "ACCOUNT_UPDATE"
	ActionAccountDelete     = "ACCOUNT_DELETE"
	ActionExportMetadata    = "EXPORT_METADATA"
	ActionExportWithSecrets = "EXPORT_WITH_SECRETS"
	ActionImport            = "IMPORT"
)

// Outcomes for audit_events.outcome.
const (
	OutcomeSuccess = "success"
	OutcomeFailure = "failure"
	OutcomeDenied  = "denied"
)

// Event is one audit_events row.
type Event struct {
	ID           string  `json:"id"`
	ActorID      *string `json:"actor_id"`
	Action       string  `json:"action"`
	ResourceType string  `json:"resource_type"`
	ResourceID   *string `json:"resource_id"`
	Outcome      string  `json:"outcome"`
	IP           string  `json:"ip"`
	UserAgent    string  `json:"user_agent"`
	Metadata     string  `json:"metadata"`
	CreatedAt    string  `json:"created_at"`
}

// WriteInput is the payload for inserting an audit event.
type WriteInput struct {
	ActorID      *string
	Action       string
	ResourceType string
	ResourceID   *string
	Outcome      string
	IP           string
	UserAgent    string
	Metadata     string
}

// ListFilter controls GET /audit-events.
type ListFilter struct {
	Limit   int
	Offset  int
	Action  string
	ActorID string
	Outcome string
}

// Store persists audit_events.
type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func formatTime(t time.Time) string {
	return t.UTC().Format(timeLayout)
}

func newID() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", err
	}
	return id.String(), nil
}

type execer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

// Write inserts an audit event. Callers that must not ignore failures (reveal, delete) should propagate the error.
func (s *Store) Write(in WriteInput) error {
	return writeTx(s.db, in)
}

// WriteTx inserts within an existing transaction.
func WriteTx(tx *sql.Tx, in WriteInput) error {
	return writeTx(tx, in)
}

func writeTx(db execer, in WriteInput) error {
	metadata := in.Metadata
	if metadata == "" {
		metadata = "{}"
	}
	id, err := newID()
	if err != nil {
		id = uuid.NewString()
	}
	_, err = db.Exec(
		`INSERT INTO audit_events (id, actor_id, action, resource_type, resource_id, outcome, ip, user_agent, metadata, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, in.ActorID, in.Action, in.ResourceType, in.ResourceID, in.Outcome, in.IP, in.UserAgent, metadata, formatTime(time.Now()),
	)
	return err
}

const (
	defaultListLimit = 50
	maxListLimit     = 200
)

func clampLimitOffset(limit, offset int) (int, int) {
	if limit <= 0 {
		limit = defaultListLimit
	}
	if limit > maxListLimit {
		limit = maxListLimit
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

// List returns audit events newest-first with total count.
func (s *Store) List(f ListFilter) ([]Event, int, error) {
	f.Limit, f.Offset = clampLimitOffset(f.Limit, f.Offset)

	where := []string{"1=1"}
	args := []any{}
	if f.Action != "" {
		where = append(where, "action = ?")
		args = append(args, f.Action)
	}
	if f.ActorID != "" {
		where = append(where, "actor_id = ?")
		args = append(args, f.ActorID)
	}
	if f.Outcome != "" {
		where = append(where, "outcome = ?")
		args = append(args, f.Outcome)
	}
	whereSQL := ""
	for i, w := range where {
		if i == 0 {
			whereSQL = w
			continue
		}
		whereSQL += " AND " + w
	}

	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM audit_events WHERE `+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	listArgs := append(append([]any{}, args...), f.Limit, f.Offset)
	rows, err := s.db.Query(
		`SELECT id, actor_id, action, resource_type, resource_id, outcome, ip, user_agent, metadata, created_at
		 FROM audit_events WHERE `+whereSQL+`
		 ORDER BY created_at DESC, id DESC
		 LIMIT ? OFFSET ?`,
		listArgs...,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var items []Event
	for rows.Next() {
		var e Event
		var actor, resource sql.NullString
		if err := rows.Scan(
			&e.ID, &actor, &e.Action, &e.ResourceType, &resource, &e.Outcome,
			&e.IP, &e.UserAgent, &e.Metadata, &e.CreatedAt,
		); err != nil {
			return nil, 0, err
		}
		if actor.Valid {
			e.ActorID = &actor.String
		}
		if resource.Valid {
			e.ResourceID = &resource.String
		}
		items = append(items, e)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if items == nil {
		items = []Event{}
	}
	return items, total, nil
}
