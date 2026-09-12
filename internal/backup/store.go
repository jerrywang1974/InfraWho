package backup

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jerrywang1974/InfraWho/internal/audit"
	"github.com/jerrywang1974/InfraWho/internal/vault"
)

const timeLayout = "2006-01-02T15:04:05.000Z"

var (
	ErrHostnameConflict = errors.New("hostname conflict")
	ErrKEKUnavailable   = errors.New("master key unavailable")
	ErrInvalidDocument  = errors.New("invalid import document")
)

// Store builds export documents and applies imports.
type Store struct {
	db         *sql.DB
	keyVersion string
	loadKEK    func() ([]byte, error)
}

// StoreOptions configures backup persistence.
type StoreOptions struct {
	KeyVersion string
	LoadKEK    func() ([]byte, error)
}

func NewStore(db *sql.DB, opts StoreOptions) *Store {
	kv := strings.TrimSpace(opts.KeyVersion)
	if kv == "" {
		kv = "kek-v1"
	}
	return &Store{db: db, keyVersion: kv, loadKEK: opts.LoadKEK}
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

func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

type assetRow struct {
	id      string
	asset   ExportAsset
	ipsJSON string
}

// Export builds a portable JSON document for active (non-deleted) assets.
// Asset rows are fully consumed before nested queries (MaxOpenConns(1) safe).
func (s *Store) Export(includeSecrets bool, now time.Time) (*Document, error) {
	var kek []byte
	var err error
	if includeSecrets {
		kek, err = s.kek()
		if err != nil {
			return nil, err
		}
	}

	rows, err := s.db.Query(
		`SELECT id, name, hostname, asset_type, os_family, os_detail, environment, purpose,
		        primary_ip, additional_ips, location, hypervisor, status, config_notes
		 FROM assets WHERE deleted_at IS NULL
		 ORDER BY hostname COLLATE NOCASE`,
	)
	if err != nil {
		return nil, err
	}

	var pending []assetRow
	for rows.Next() {
		var r assetRow
		if err := rows.Scan(
			&r.id, &r.asset.Name, &r.asset.Hostname, &r.asset.AssetType, &r.asset.OSFamily, &r.asset.OSDetail,
			&r.asset.Environment, &r.asset.Purpose, &r.asset.PrimaryIP, &r.ipsJSON, &r.asset.Location,
			&r.asset.Hypervisor, &r.asset.Status, &r.asset.ConfigNotes,
		); err != nil {
			_ = rows.Close()
			return nil, err
		}
		pending = append(pending, r)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	doc := &Document{
		Version:        DocumentVersion,
		ExportedAt:     formatTime(now),
		IncludeSecrets: includeSecrets,
		Assets:         []ExportAsset{},
	}

	for _, r := range pending {
		a := r.asset
		a.AdditionalIPs = decodeIPs(r.ipsJSON)
		tags, err := s.loadTags(r.id)
		if err != nil {
			return nil, err
		}
		a.Tags = tags
		accounts, err := s.loadAccounts(r.id, includeSecrets, kek)
		if err != nil {
			return nil, err
		}
		a.Accounts = accounts
		jobs, err := s.loadJobs(r.id)
		if err != nil {
			return nil, err
		}
		a.Jobs = jobs
		notes, err := s.loadNotes(r.id)
		if err != nil {
			return nil, err
		}
		a.Notes = notes
		doc.Assets = append(doc.Assets, a)
	}
	return doc, nil
}

func decodeIPs(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return []string{}
	}
	var ips []string
	if err := json.Unmarshal([]byte(raw), &ips); err != nil || ips == nil {
		return []string{}
	}
	return ips
}

func (s *Store) loadTags(assetID string) ([]string, error) {
	rows, err := s.db.Query(
		`SELECT t.name FROM tags t
		 JOIN asset_tags at ON at.tag_id = t.id
		 WHERE at.asset_id = ?
		 ORDER BY t.name COLLATE NOCASE`,
		assetID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

func (s *Store) loadAccounts(assetID string, includeSecrets bool, kek []byte) ([]ExportAccount, error) {
	rows, err := s.db.Query(
		`SELECT a.id, a.username, a.auth_type, a.description,
		        CASE WHEN sp.account_id IS NULL THEN 0 ELSE 1 END AS has_secret,
		        sp.nonce, sp.ciphertext, sp.wrapped_dek
		 FROM accounts a
		 LEFT JOIN secret_payloads sp ON sp.account_id = a.id
		 WHERE a.asset_id = ?
		 ORDER BY a.username COLLATE NOCASE`,
		assetID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []ExportAccount{}
	for rows.Next() {
		var acc ExportAccount
		var id string
		var hasSecret int
		var nonceB, ctB, wrappedB []byte
		if err := rows.Scan(&id, &acc.Username, &acc.AuthType, &acc.Description, &hasSecret, &nonceB, &ctB, &wrappedB); err != nil {
			return nil, err
		}
		acc.HasSecret = hasSecret != 0
		if includeSecrets && acc.HasSecret {
			plain, err := vault.Open(&vault.Envelope{
				Nonce:      nonceB,
				Ciphertext: ctB,
				WrappedDEK: wrappedB,
			}, id, acc.AuthType, kek)
			if err != nil {
				return nil, fmt.Errorf("decrypt account %s: %w", id, err)
			}
			sec := string(plain)
			acc.Secret = &sec
		}
		out = append(out, acc)
	}
	return out, rows.Err()
}

func (s *Store) loadJobs(assetID string) ([]ExportJob, error) {
	rows, err := s.db.Query(
		`SELECT name, scheduler_type, schedule_expr, command_or_path, description, enabled_doc
		 FROM scheduled_jobs WHERE asset_id = ? ORDER BY name COLLATE NOCASE`,
		assetID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ExportJob{}
	for rows.Next() {
		var j ExportJob
		var enabled int
		if err := rows.Scan(&j.Name, &j.SchedulerType, &j.ScheduleExpr, &j.CommandOrPath, &j.Description, &enabled); err != nil {
			return nil, err
		}
		j.EnabledDoc = enabled != 0
		out = append(out, j)
	}
	return out, rows.Err()
}

func (s *Store) loadNotes(assetID string) ([]ExportNote, error) {
	rows, err := s.db.Query(
		`SELECT title, body FROM asset_notes WHERE asset_id = ? ORDER BY created_at ASC`,
		assetID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ExportNote{}
	for rows.Next() {
		var n ExportNote
		if err := rows.Scan(&n.Title, &n.Body); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// DocumentHasSecrets reports whether any account carries a plaintext secret.
func DocumentHasSecrets(doc *Document) bool {
	if doc == nil {
		return false
	}
	for _, a := range doc.Assets {
		for _, acc := range a.Accounts {
			if acc.Secret != nil && *acc.Secret != "" {
				return true
			}
		}
	}
	return false
}

// Import inserts assets (and nested rows) from doc. Rejects on any active hostname conflict.
// Secrets are resealed under the current KEK. Audit is written in the same transaction.
func (s *Store) Import(doc *Document, now time.Time, aw audit.WriteInput) (*importResult, error) {
	if doc == nil {
		return nil, ErrInvalidDocument
	}
	if doc.Version != 0 && doc.Version != DocumentVersion {
		return nil, fmt.Errorf("%w: unsupported version %d", ErrInvalidDocument, doc.Version)
	}
	if doc.Assets == nil {
		doc.Assets = []ExportAsset{}
	}

	needKEK := DocumentHasSecrets(doc)
	var kek []byte
	var err error
	if needKEK {
		kek, err = s.kek()
		if err != nil {
			return nil, err
		}
	}

	// Pre-check hostnames for conflicts (case-insensitive among active assets).
	seen := map[string]struct{}{}
	for _, a := range doc.Assets {
		hn := strings.TrimSpace(a.Hostname)
		if hn == "" {
			return nil, fmt.Errorf("%w: asset hostname is required", ErrInvalidDocument)
		}
		key := strings.ToLower(hn)
		if _, ok := seen[key]; ok {
			return nil, fmt.Errorf("%w: duplicate hostname %q in import", ErrHostnameConflict, hn)
		}
		seen[key] = struct{}{}
		var n int
		err := s.db.QueryRow(
			`SELECT COUNT(*) FROM assets WHERE hostname = ? COLLATE NOCASE AND deleted_at IS NULL`,
			hn,
		).Scan(&n)
		if err != nil {
			return nil, err
		}
		if n > 0 {
			return nil, fmt.Errorf("%w: %s", ErrHostnameConflict, hn)
		}
	}

	ctx := context.Background()
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(ctx, `ROLLBACK`)
		}
		_ = conn.Close()
	}()
	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return nil, err
	}

	// Re-check conflicts under the write lock.
	for hn := range seen {
		var n int
		err := conn.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM assets WHERE hostname = ? COLLATE NOCASE AND deleted_at IS NULL`,
			hn,
		).Scan(&n)
		if err != nil {
			return nil, err
		}
		if n > 0 {
			return nil, fmt.Errorf("%w: %s", ErrHostnameConflict, hn)
		}
	}

	ts := formatTime(now)
	result := &importResult{}

	for _, a := range doc.Assets {
		assetID, err := newID()
		if err != nil {
			return nil, err
		}
		status := strings.TrimSpace(a.Status)
		if status == "" || status == "retired" {
			status = "active"
		}
		ips := a.AdditionalIPs
		if ips == nil {
			ips = []string{}
		}
		ipsJSON, err := json.Marshal(ips)
		if err != nil {
			return nil, err
		}
		_, err = conn.ExecContext(ctx,
			`INSERT INTO assets (
				id, name, hostname, asset_type, os_family, os_detail, environment, purpose,
				primary_ip, additional_ips, location, hypervisor, owner_id, backup_owner_id,
				status, config_notes, deleted_at, created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, NULL, ?, ?, NULL, ?, ?)`,
			assetID, strings.TrimSpace(a.Name), strings.TrimSpace(a.Hostname),
			a.AssetType, a.OSFamily, a.OSDetail, a.Environment, a.Purpose,
			a.PrimaryIP, string(ipsJSON), a.Location, a.Hypervisor,
			status, a.ConfigNotes, ts, ts,
		)
		if err != nil {
			if isUniqueViolation(err) {
				return nil, fmt.Errorf("%w: %s", ErrHostnameConflict, a.Hostname)
			}
			return nil, err
		}
		if err := s.replaceTagsTx(ctx, conn, assetID, a.Tags); err != nil {
			return nil, err
		}
		result.ImportedAssets++

		for _, acc := range a.Accounts {
			accID, err := newID()
			if err != nil {
				return nil, err
			}
			username := strings.TrimSpace(acc.Username)
			var lastRotated any
			_, err = conn.ExecContext(ctx,
				`INSERT INTO accounts (id, asset_id, username, auth_type, description, last_rotated_at, created_at, updated_at)
				 VALUES (?, ?, ?, ?, ?, NULL, ?, ?)`,
				accID, assetID, username, acc.AuthType, acc.Description, ts, ts,
			)
			if err != nil {
				return nil, err
			}
			if acc.Secret != nil && *acc.Secret != "" {
				env, err := vault.Seal([]byte(*acc.Secret), accID, acc.AuthType, kek)
				if err != nil {
					return nil, err
				}
				lastRotated = ts
				_, err = conn.ExecContext(ctx, `UPDATE accounts SET last_rotated_at = ? WHERE id = ?`, lastRotated, accID)
				if err != nil {
					return nil, err
				}
				_, err = conn.ExecContext(ctx,
					`INSERT INTO secret_payloads (account_id, nonce, ciphertext, wrapped_dek, key_version, created_at, updated_at)
					 VALUES (?, ?, ?, ?, ?, ?, ?)`,
					accID, env.Nonce, env.Ciphertext, env.WrappedDEK, s.keyVersion, ts, ts,
				)
				if err != nil {
					return nil, err
				}
			}
			result.ImportedAccounts++
		}

		for _, j := range a.Jobs {
			jobID, err := newID()
			if err != nil {
				return nil, err
			}
			enabled := 0
			if j.EnabledDoc {
				enabled = 1
			}
			_, err = conn.ExecContext(ctx,
				`INSERT INTO scheduled_jobs (id, asset_id, name, scheduler_type, schedule_expr, command_or_path, description, enabled_doc, created_at, updated_at)
				 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				jobID, assetID, j.Name, j.SchedulerType, j.ScheduleExpr, j.CommandOrPath, j.Description, enabled, ts, ts,
			)
			if err != nil {
				return nil, err
			}
			result.ImportedJobs++
		}

		for _, n := range a.Notes {
			noteID, err := newID()
			if err != nil {
				return nil, err
			}
			_, err = conn.ExecContext(ctx,
				`INSERT INTO asset_notes (id, asset_id, title, body, author_id, created_at, updated_at)
				 VALUES (?, ?, ?, ?, NULL, ?, ?)`,
				noteID, assetID, n.Title, n.Body, ts, ts,
			)
			if err != nil {
				return nil, err
			}
			result.ImportedNotes++
		}
	}

	aw.Action = audit.ActionImport
	aw.ResourceType = "export"
	aw.Outcome = audit.OutcomeSuccess
	if aw.Metadata == "" {
		aw.Metadata = fmt.Sprintf(
			`{"imported_assets":%d,"imported_accounts":%d,"has_secrets":%t}`,
			result.ImportedAssets, result.ImportedAccounts, needKEK,
		)
	}
	if err := writeAuditConn(ctx, conn, aw); err != nil {
		return nil, err
	}

	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return nil, err
	}
	committed = true
	return result, nil
}

func writeAuditConn(ctx context.Context, conn *sql.Conn, in audit.WriteInput) error {
	metadata := in.Metadata
	if metadata == "" {
		metadata = "{}"
	}
	id, err := newID()
	if err != nil {
		id = uuid.NewString()
	}
	_, err = conn.ExecContext(ctx,
		`INSERT INTO audit_events (id, actor_id, action, resource_type, resource_id, outcome, ip, user_agent, metadata, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, in.ActorID, in.Action, in.ResourceType, in.ResourceID, in.Outcome, in.IP, in.UserAgent, metadata, formatTime(time.Now()),
	)
	return err
}

func (s *Store) replaceTagsTx(ctx context.Context, conn *sql.Conn, assetID string, tags []string) error {
	if _, err := conn.ExecContext(ctx, `DELETE FROM asset_tags WHERE asset_id = ?`, assetID); err != nil {
		return err
	}
	for _, raw := range tags {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		var tagID string
		err := conn.QueryRowContext(ctx, `SELECT id FROM tags WHERE name = ? COLLATE NOCASE`, name).Scan(&tagID)
		if err == sql.ErrNoRows {
			tagID, err = newID()
			if err != nil {
				return err
			}
			_, err = conn.ExecContext(ctx, `INSERT INTO tags (id, name) VALUES (?, ?)`, tagID, name)
			if err != nil {
				if isUniqueViolation(err) {
					if err2 := conn.QueryRowContext(ctx, `SELECT id FROM tags WHERE name = ? COLLATE NOCASE`, name).Scan(&tagID); err2 != nil {
						return err2
					}
				} else {
					return err
				}
			}
		} else if err != nil {
			return err
		}
		if _, err := conn.ExecContext(ctx, `INSERT INTO asset_tags (asset_id, tag_id) VALUES (?, ?)`, assetID, tagID); err != nil {
			return err
		}
	}
	return nil
}
