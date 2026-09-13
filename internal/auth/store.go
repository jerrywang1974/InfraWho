package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ErrAlreadyBootstrapped is returned when a first-admin create races past an existing user.
var ErrAlreadyBootstrapped = errors.New("already bootstrapped")

const (
	timeLayout = "2006-01-02T15:04:05.000Z"

	settingChecklistKey = "install_wizard_completed"
)

// Store persists users, sessions, and install-wizard settings.
type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func formatTime(t time.Time) string {
	return t.UTC().Format(timeLayout)
}

func parseTime(s string) (time.Time, error) {
	if t, err := time.Parse(timeLayout, s); err == nil {
		return t, nil
	}
	// Accept RFC3339 variants written by strftime defaults.
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.UTC(), nil
	}
	return time.Parse(time.RFC3339, s)
}

func (s *Store) UserCount() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

func (s *Store) CreateUser(username, displayName string, role Role, passwordHash string) (*User, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return nil, fmt.Errorf("username is required")
	}
	if !role.Valid() {
		return nil, fmt.Errorf("invalid role %q", role)
	}
	if displayName == "" {
		displayName = username
	}
	now := formatTime(time.Now())
	u := &User{
		ID:           uuid.NewString(),
		Username:     username,
		DisplayName:  displayName,
		Role:         role,
		PasswordHash: passwordHash,
	}
	_, err := s.db.Exec(
		`INSERT INTO users (id, username, display_name, role, password_hash, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		u.ID, u.Username, u.DisplayName, string(u.Role), u.PasswordHash, now, now,
	)
	if err != nil {
		return nil, err
	}
	u.CreatedAt, _ = parseTime(now)
	u.UpdatedAt = u.CreatedAt
	return u, nil
}

func (s *Store) FindUserByUsername(username string) (*User, error) {
	row := s.db.QueryRow(
		`SELECT id, username, display_name, role, password_hash, created_at, updated_at
		 FROM users WHERE username = ?`,
		strings.TrimSpace(username),
	)
	return scanUser(row)
}

// FindUserByID returns a principal without password_hash (safe for request context).
func (s *Store) FindUserByID(id string) (*User, error) {
	row := s.db.QueryRow(
		`SELECT id, username, display_name, role, created_at, updated_at
		 FROM users WHERE id = ?`,
		id,
	)
	return scanUserNoHash(row)
}

// UserPasswordHash loads only the password hash for step-up / credential checks.
func (s *Store) UserPasswordHash(id string) (string, error) {
	var hash string
	err := s.db.QueryRow(`SELECT password_hash FROM users WHERE id = ?`, id).Scan(&hash)
	return hash, err
}

type scannable interface {
	Scan(dest ...any) error
}

func scanUser(row scannable) (*User, error) {
	var u User
	var role string
	var created, updated string
	if err := row.Scan(&u.ID, &u.Username, &u.DisplayName, &role, &u.PasswordHash, &created, &updated); err != nil {
		return nil, err
	}
	u.Role = Role(role)
	var err error
	if u.CreatedAt, err = parseTime(created); err != nil {
		return nil, err
	}
	if u.UpdatedAt, err = parseTime(updated); err != nil {
		return nil, err
	}
	return &u, nil
}

func scanUserNoHash(row scannable) (*User, error) {
	var u User
	var role string
	var created, updated string
	if err := row.Scan(&u.ID, &u.Username, &u.DisplayName, &role, &created, &updated); err != nil {
		return nil, err
	}
	u.Role = Role(role)
	var err error
	if u.CreatedAt, err = parseTime(created); err != nil {
		return nil, err
	}
	if u.UpdatedAt, err = parseTime(updated); err != nil {
		return nil, err
	}
	return &u, nil
}

// BootstrapAdmin creates the first admin and checklist under a SQLite write lock.
func (s *Store) BootstrapAdmin(username, displayName, passwordHash string, now time.Time) (*User, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return nil, fmt.Errorf("username is required")
	}
	if displayName == "" {
		displayName = username
	}
	ctx := context.Background()
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(ctx, `ROLLBACK`)
		}
	}()

	var n int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		return nil, err
	}
	if n > 0 {
		return nil, ErrAlreadyBootstrapped
	}

	ts := formatTime(now)
	u := &User{
		ID:           uuid.NewString(),
		Username:     username,
		DisplayName:  displayName,
		Role:         RoleAdmin,
		PasswordHash: passwordHash,
	}
	if _, err := conn.ExecContext(ctx,
		`INSERT INTO users (id, username, display_name, role, password_hash, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		u.ID, u.Username, u.DisplayName, string(u.Role), u.PasswordHash, ts, ts,
	); err != nil {
		return nil, err
	}
	if _, err := conn.ExecContext(ctx,
		`INSERT INTO app_settings (key, value, updated_at) VALUES (?, '1', ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		settingChecklistKey, ts,
	); err != nil {
		return nil, err
	}
	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return nil, err
	}
	committed = true
	u.CreatedAt, _ = parseTime(ts)
	u.UpdatedAt = u.CreatedAt
	return u, nil
}

// CreateSession inserts a session and returns the raw cookie token.
func (s *Store) CreateSession(userID string, now time.Time) (raw string, sess *Session, err error) {
	raw, hash, err := newSessionToken()
	if err != nil {
		return "", nil, err
	}
	sess = &Session{
		ID:                uuid.NewString(),
		UserID:            userID,
		TokenHash:         hash,
		IdleExpiresAt:     now.Add(IdleTTL),
		AbsoluteExpiresAt: now.Add(AbsoluteTTL),
		CreatedAt:         now,
	}
	_, err = s.db.Exec(
		`INSERT INTO sessions (id, user_id, token_hash, idle_expires_at, absolute_expires_at, step_up_expires_at, created_at)
		 VALUES (?, ?, ?, ?, ?, NULL, ?)`,
		sess.ID, sess.UserID, sess.TokenHash,
		formatTime(sess.IdleExpiresAt), formatTime(sess.AbsoluteExpiresAt), formatTime(sess.CreatedAt),
	)
	if err != nil {
		return "", nil, err
	}
	return raw, sess, nil
}

func (s *Store) FindSessionByToken(raw string) (*Session, error) {
	hash := hashToken(raw)
	row := s.db.QueryRow(
		`SELECT id, user_id, token_hash, idle_expires_at, absolute_expires_at, step_up_expires_at, created_at
		 FROM sessions WHERE token_hash = ?`,
		hash,
	)
	var sess Session
	var idle, abs, created string
	var stepUp sql.NullString
	if err := row.Scan(&sess.ID, &sess.UserID, &sess.TokenHash, &idle, &abs, &stepUp, &created); err != nil {
		return nil, err
	}
	var err error
	if sess.IdleExpiresAt, err = parseTime(idle); err != nil {
		return nil, err
	}
	if sess.AbsoluteExpiresAt, err = parseTime(abs); err != nil {
		return nil, err
	}
	if sess.CreatedAt, err = parseTime(created); err != nil {
		return nil, err
	}
	if stepUp.Valid && stepUp.String != "" {
		t, err := parseTime(stepUp.String)
		if err != nil {
			return nil, err
		}
		sess.StepUpExpiresAt = &t
	}
	return &sess, nil
}

func (s *Store) TouchSession(id string, idleExpiresAt time.Time) error {
	_, err := s.db.Exec(
		`UPDATE sessions SET idle_expires_at = ? WHERE id = ?`,
		formatTime(idleExpiresAt), id,
	)
	return err
}

func (s *Store) SetStepUp(id string, expiresAt time.Time) error {
	_, err := s.db.Exec(
		`UPDATE sessions SET step_up_expires_at = ? WHERE id = ?`,
		formatTime(expiresAt), id,
	)
	return err
}

func (s *Store) DeleteSession(id string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE id = ?`, id)
	return err
}

func (s *Store) DeleteExpiredSessions(now time.Time) error {
	ts := formatTime(now)
	_, err := s.db.Exec(
		`DELETE FROM sessions WHERE idle_expires_at < ? OR absolute_expires_at < ?`,
		ts, ts,
	)
	return err
}

func (s *Store) ChecklistComplete() (bool, error) {
	var value string
	err := s.db.QueryRow(`SELECT value FROM app_settings WHERE key = ?`, settingChecklistKey).Scan(&value)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return value == "1" || strings.EqualFold(value, "true"), nil
}

func (s *Store) SetChecklistComplete(now time.Time) error {
	_, err := s.db.Exec(
		`INSERT INTO app_settings (key, value, updated_at) VALUES (?, '1', ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		settingChecklistKey, formatTime(now),
	)
	return err
}

// WriteAudit inserts an audit_events row. Best-effort callers may ignore the error.
func (s *Store) WriteAudit(actorID *string, action, resourceType string, resourceID *string, outcome, ip, userAgent, metadata string) error {
	if metadata == "" {
		metadata = "{}"
	}
	id := uuid.NewString()
	_, err := s.db.Exec(
		`INSERT INTO audit_events (id, actor_id, action, resource_type, resource_id, outcome, ip, user_agent, metadata, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, actorID, action, resourceType, resourceID, outcome, ip, userAgent, metadata, formatTime(time.Now()),
	)
	return err
}
