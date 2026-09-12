package accounts

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jerrywang1974/InfraWho/internal/audit"
	"github.com/jerrywang1974/InfraWho/internal/vault"
)

const timeLayout = "2006-01-02T15:04:05.000Z"

var (
	ErrNotFound          = errors.New("account not found")
	ErrAssetNotFound     = errors.New("asset not found")
	ErrAssetDeleted      = errors.New("asset is soft-deleted")
	ErrNoSecret          = errors.New("account has no secret")
	ErrAuthTypeImmutable = errors.New("auth_type cannot be changed while a secret exists; use rotate-secret")
	ErrKEKUnavailable    = errors.New("master key unavailable")
)

// Account is the API model (never includes ciphertext).
type Account struct {
	ID            string  `json:"id"`
	AssetID       string  `json:"asset_id"`
	Username      string  `json:"username"`
	AuthType      string  `json:"auth_type"`
	Description   string  `json:"description"`
	LastRotatedAt *string `json:"last_rotated_at,omitempty"`
	HasSecret     bool    `json:"has_secret"`
	CreatedAt     string  `json:"created_at"`
	UpdatedAt     string  `json:"updated_at"`
}

// SecretPayload is the sealed vault material for an account.
type SecretPayload struct {
	AccountID  string
	Nonce      []byte
	Ciphertext []byte
	WrappedDEK []byte
	KeyVersion string
}

// Store persists accounts and secret_payloads.
type Store struct {
	db         *sql.DB
	keyVersion string
	loadKEK    func() ([]byte, error)
}

// StoreOptions configures account persistence.
type StoreOptions struct {
	KeyVersion string
	LoadKEK    func() ([]byte, error)
}

func NewStore(db *sql.DB, opts StoreOptions) *Store {
	kv := strings.TrimSpace(opts.KeyVersion)
	if kv == "" {
		kv = "kek-v1"
	}
	return &Store{
		db:         db,
		keyVersion: kv,
		loadKEK:    opts.LoadKEK,
	}
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

func (s *Store) kek() ([]byte, error) {
	if s.loadKEK == nil {
		return nil, ErrKEKUnavailable
	}
	kek, err := s.loadKEK()
	if err != nil || len(kek) == 0 {
		return nil, ErrKEKUnavailable
	}
	return kek, nil
}

type createRequest struct {
	Username    string  `json:"username"`
	AuthType    string  `json:"auth_type"`
	Description string  `json:"description"`
	Secret      *string `json:"secret"`
}

type patchRequest struct {
	Username    *string `json:"username"`
	AuthType    *string `json:"auth_type"`
	Description *string `json:"description"`
}

func (p *patchRequest) hasFieldUpdates() bool {
	return p.Username != nil || p.AuthType != nil || p.Description != nil
}

type rotateRequest struct {
	Secret   string  `json:"secret"`
	AuthType *string `json:"auth_type"`
}

func scanAccount(row interface {
	Scan(dest ...any) error
}, hasSecret bool) (*Account, error) {
	var a Account
	var rotated sql.NullString
	if err := row.Scan(
		&a.ID, &a.AssetID, &a.Username, &a.AuthType, &a.Description,
		&rotated, &a.CreatedAt, &a.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if rotated.Valid && rotated.String != "" {
		a.LastRotatedAt = &rotated.String
	}
	a.HasSecret = hasSecret
	return &a, nil
}

const accountColumns = `id, asset_id, username, auth_type, description, last_rotated_at, created_at, updated_at`

func (s *Store) hasSecretTx(q interface {
	QueryRow(query string, args ...any) *sql.Row
}, accountID string) (bool, error) {
	var n int
	err := q.QueryRow(`SELECT COUNT(*) FROM secret_payloads WHERE account_id = ?`, accountID).Scan(&n)
	return n > 0, err
}

// Get returns one account by id.
func (s *Store) Get(id string) (*Account, error) {
	row := s.db.QueryRow(`SELECT `+accountColumns+` FROM accounts WHERE id = ?`, id)
	a, err := scanAccount(row, false)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	has, err := s.hasSecretTx(s.db, id)
	if err != nil {
		return nil, err
	}
	a.HasSecret = has
	return a, nil
}

func (s *Store) loadSecret(accountID string) (*SecretPayload, error) {
	var p SecretPayload
	err := s.db.QueryRow(
		`SELECT account_id, nonce, ciphertext, wrapped_dek, key_version FROM secret_payloads WHERE account_id = ?`,
		accountID,
	).Scan(&p.AccountID, &p.Nonce, &p.Ciphertext, &p.WrappedDEK, &p.KeyVersion)
	if err == sql.ErrNoRows {
		return nil, ErrNoSecret
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// Create inserts an account under assetID; optional secret is sealed in the same transaction.
func (s *Store) Create(assetID string, in *createRequest, now time.Time) (*Account, error) {
	var env *vault.Envelope
	var kek []byte
	var err error
	if in.Secret != nil {
		kek, err = s.kek()
		if err != nil {
			return nil, err
		}
	}

	ctx := context.Background()
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, err
	}

	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		_ = conn.Close()
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(ctx, `ROLLBACK`)
		}
		_ = conn.Close()
	}()

	var deleted sql.NullString
	err = conn.QueryRowContext(ctx, `SELECT deleted_at FROM assets WHERE id = ?`, assetID).Scan(&deleted)
	if err == sql.ErrNoRows {
		return nil, ErrAssetNotFound
	}
	if err != nil {
		return nil, err
	}
	if deleted.Valid && deleted.String != "" {
		return nil, ErrAssetDeleted
	}

	id, err := newID()
	if err != nil {
		return nil, err
	}
	ts := formatTime(now)
	username := strings.TrimSpace(in.Username)
	var lastRotated *string

	if in.Secret != nil {
		env, err = vault.Seal([]byte(*in.Secret), id, in.AuthType, kek)
		if err != nil {
			return nil, err
		}
	}

	_, err = conn.ExecContext(ctx,
		`INSERT INTO accounts (id, asset_id, username, auth_type, description, last_rotated_at, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, assetID, username, in.AuthType, in.Description, nil, ts, ts,
	)
	if err != nil {
		return nil, err
	}

	if env != nil {
		lastRotated = &ts
		_, err = conn.ExecContext(ctx,
			`UPDATE accounts SET last_rotated_at = ? WHERE id = ?`,
			ts, id,
		)
		if err != nil {
			return nil, err
		}
		_, err = conn.ExecContext(ctx,
			`INSERT INTO secret_payloads (account_id, nonce, ciphertext, wrapped_dek, key_version, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			id, env.Nonce, env.Ciphertext, env.WrappedDEK, s.keyVersion, ts, ts,
		)
		if err != nil {
			return nil, err
		}
	}

	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return nil, err
	}
	committed = true
	// Build response without re-entering the MaxOpenConns(1) pool while conn is held.
	return &Account{
		ID:            id,
		AssetID:       assetID,
		Username:      username,
		AuthType:      in.AuthType,
		Description:   in.Description,
		LastRotatedAt: lastRotated,
		HasSecret:     env != nil,
		CreatedAt:     ts,
		UpdatedAt:     ts,
	}, nil
}

// Update patches account metadata. auth_type is rejected when a secret_payload exists.
func (s *Store) Update(id string, in *patchRequest, now time.Time) (*Account, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	row := tx.QueryRow(`SELECT `+accountColumns+` FROM accounts WHERE id = ?`, id)
	cur, err := scanAccount(row, false)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	has, err := s.hasSecretTx(tx, id)
	if err != nil {
		return nil, err
	}
	cur.HasSecret = has

	if in.AuthType != nil && *in.AuthType != cur.AuthType && has {
		return nil, ErrAuthTypeImmutable
	}

	if in.Username != nil {
		cur.Username = strings.TrimSpace(*in.Username)
	}
	if in.AuthType != nil {
		cur.AuthType = *in.AuthType
	}
	if in.Description != nil {
		cur.Description = *in.Description
	}

	ts := formatTime(now)
	_, err = tx.Exec(
		`UPDATE accounts SET username = ?, auth_type = ?, description = ?, updated_at = ? WHERE id = ?`,
		cur.Username, cur.AuthType, cur.Description, ts, id,
	)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.Get(id)
}

// Delete removes an account (and cascaded secret_payload). Audit is written in the same transaction.
func (s *Store) Delete(id string, aw audit.WriteInput) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.Exec(`DELETE FROM accounts WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	aw.Action = audit.ActionAccountDelete
	aw.ResourceType = "account"
	aw.ResourceID = &id
	aw.Outcome = audit.OutcomeSuccess
	if err := audit.WriteTx(tx, aw); err != nil {
		return err
	}
	return tx.Commit()
}

// RotateSecret reseals plaintext under (optional) new auth_type and upserts secret_payload.
func (s *Store) RotateSecret(id string, in *rotateRequest, now time.Time, aw audit.WriteInput) (*Account, error) {
	kek, err := s.kek()
	if err != nil {
		return nil, err
	}

	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	row := tx.QueryRow(`SELECT `+accountColumns+` FROM accounts WHERE id = ?`, id)
	cur, err := scanAccount(row, false)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	authType := cur.AuthType
	if in.AuthType != nil {
		authType = *in.AuthType
	}

	env, err := vault.Seal([]byte(in.Secret), id, authType, kek)
	if err != nil {
		return nil, err
	}

	ts := formatTime(now)
	_, err = tx.Exec(
		`UPDATE accounts SET auth_type = ?, last_rotated_at = ?, updated_at = ? WHERE id = ?`,
		authType, ts, ts, id,
	)
	if err != nil {
		return nil, err
	}

	res, err := tx.Exec(
		`UPDATE secret_payloads SET nonce = ?, ciphertext = ?, wrapped_dek = ?, key_version = ?, updated_at = ?
		 WHERE account_id = ?`,
		env.Nonce, env.Ciphertext, env.WrappedDEK, s.keyVersion, ts, id,
	)
	if err != nil {
		return nil, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if n == 0 {
		_, err = tx.Exec(
			`INSERT INTO secret_payloads (account_id, nonce, ciphertext, wrapped_dek, key_version, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			id, env.Nonce, env.Ciphertext, env.WrappedDEK, s.keyVersion, ts, ts,
		)
		if err != nil {
			return nil, err
		}
	}

	aw.Action = audit.ActionCredentialRotate
	aw.ResourceType = "account"
	aw.ResourceID = &id
	aw.Outcome = audit.OutcomeSuccess
	if aw.Metadata == "" {
		aw.Metadata = `{"key_version":"` + s.keyVersion + `"}`
	}
	if err := audit.WriteTx(tx, aw); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.Get(id)
}

// Reveal decrypts the secret for account id. Caller must enforce RBAC, step-up, and rate limits.
func (s *Store) Reveal(id string) (plaintext []byte, acc *Account, err error) {
	kek, err := s.kek()
	if err != nil {
		return nil, nil, err
	}
	acc, err = s.Get(id)
	if err != nil {
		return nil, nil, err
	}
	if !acc.HasSecret {
		return nil, acc, ErrNoSecret
	}
	payload, err := s.loadSecret(id)
	if err != nil {
		return nil, nil, err
	}
	plain, err := vault.Open(&vault.Envelope{
		Nonce:      payload.Nonce,
		Ciphertext: payload.Ciphertext,
		WrappedDEK: payload.WrappedDEK,
	}, acc.ID, acc.AuthType, kek)
	if err != nil {
		return nil, nil, err
	}
	return plain, acc, nil
}
