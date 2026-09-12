package assets

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jerrywang1974/InfraWho/internal/search"
)

const timeLayout = "2006-01-02T15:04:05.000Z"

var (
	ErrNotFound       = errors.New("asset not found")
	ErrConflict       = errors.New("asset conflict")
	ErrAlreadyDeleted = errors.New("asset already soft-deleted")
	ErrNotSoftDeleted = errors.New("asset must be soft-deleted before purge")
	ErrInvalidOwner   = errors.New("invalid owner reference")
)

// AuditWrite carries actor context for transactional audit inserts.
type AuditWrite struct {
	ActorID   *string
	Action    string
	IP        string
	UserAgent string
}

// Store persists assets and related tag rows.
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
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.UTC(), nil
	}
	return time.Parse(time.RFC3339, s)
}

func newID() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", err
	}
	return id.String(), nil
}

func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// Asset is the API/persistence model for an asset row plus tags.
type Asset struct {
	ID            string   `json:"id"`
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
	DeletedAt     *string  `json:"deleted_at,omitempty"`
	CreatedAt     string   `json:"created_at"`
	UpdatedAt     string   `json:"updated_at"`
}

// AccountSummary is non-secret account metadata on asset detail.
type AccountSummary struct {
	ID            string  `json:"id"`
	Username      string  `json:"username"`
	AuthType      string  `json:"auth_type"`
	Description   string  `json:"description"`
	LastRotatedAt *string `json:"last_rotated_at,omitempty"`
}

// JobSummary is a scheduled-job row summary on asset detail.
type JobSummary struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	SchedulerType string `json:"scheduler_type"`
	EnabledDoc    bool   `json:"enabled_doc"`
}

// NoteSummary is a note title summary on asset detail.
type NoteSummary struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	CreatedAt string `json:"created_at"`
}

// AssetDetail embeds an asset with related summaries (no secrets).
type AssetDetail struct {
	Asset
	Accounts []AccountSummary `json:"accounts"`
	Jobs     []JobSummary     `json:"jobs"`
	Notes    []NoteSummary    `json:"notes"`
}

// ListFilter controls GET /assets listing.
type ListFilter struct {
	Limit          int
	Offset         int
	Tag            string
	Environment    string
	Status         string
	Q              string
	IncludeDeleted bool
}

func (s *Store) userExists(id string) (bool, error) {
	if id == "" {
		return false, nil
	}
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM users WHERE id = ?`, id).Scan(&n)
	return n > 0, err
}

func encodeIPs(ips []string) (string, error) {
	if ips == nil {
		ips = []string{}
	}
	b, err := json.Marshal(ips)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func decodeIPs(raw string) ([]string, error) {
	if raw == "" {
		return []string{}, nil
	}
	var ips []string
	if err := json.Unmarshal([]byte(raw), &ips); err != nil {
		return nil, err
	}
	if ips == nil {
		ips = []string{}
	}
	return ips, nil
}

func scanAsset(row interface {
	Scan(dest ...any) error
}) (*Asset, error) {
	var a Asset
	var additional string
	var owner, backup, deleted sql.NullString
	if err := row.Scan(
		&a.ID, &a.Name, &a.Hostname, &a.AssetType, &a.OSFamily, &a.OSDetail,
		&a.Environment, &a.Purpose, &a.PrimaryIP, &additional, &a.Location, &a.Hypervisor,
		&owner, &backup, &a.Status, &a.ConfigNotes, &deleted, &a.CreatedAt, &a.UpdatedAt,
	); err != nil {
		return nil, err
	}
	ips, err := decodeIPs(additional)
	if err != nil {
		return nil, err
	}
	a.AdditionalIPs = ips
	if owner.Valid {
		a.OwnerID = &owner.String
	}
	if backup.Valid {
		a.BackupOwnerID = &backup.String
	}
	if deleted.Valid && deleted.String != "" {
		a.DeletedAt = &deleted.String
	}
	a.Tags = []string{}
	return &a, nil
}

const assetColumns = `id, name, hostname, asset_type, os_family, os_detail, environment, purpose,
	primary_ip, additional_ips, location, hypervisor, owner_id, backup_owner_id, status,
	config_notes, deleted_at, created_at, updated_at`

func (s *Store) loadTags(assetID string) ([]string, error) {
	rows, err := s.db.Query(
		`SELECT t.name FROM tags t
		 INNER JOIN asset_tags at ON at.tag_id = t.id
		 WHERE at.asset_id = ?
		 ORDER BY t.name COLLATE NOCASE`,
		assetID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tags []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		tags = append(tags, name)
	}
	if tags == nil {
		tags = []string{}
	}
	return tags, rows.Err()
}

func (s *Store) replaceTagsTx(tx *sql.Tx, assetID string, tags []string) error {
	if _, err := tx.Exec(`DELETE FROM asset_tags WHERE asset_id = ?`, assetID); err != nil {
		return err
	}
	for _, name := range tags {
		tagID, err := upsertTagTx(tx, name)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(
			`INSERT INTO asset_tags (asset_id, tag_id) VALUES (?, ?)`,
			assetID, tagID,
		); err != nil {
			return err
		}
	}
	return nil
}

func upsertTagTx(tx *sql.Tx, name string) (string, error) {
	var id string
	err := tx.QueryRow(`SELECT id FROM tags WHERE name = ?`, name).Scan(&id)
	if err == nil {
		return id, nil
	}
	if err != sql.ErrNoRows {
		return "", err
	}
	id, err = newID()
	if err != nil {
		return "", err
	}
	_, err = tx.Exec(`INSERT INTO tags (id, name) VALUES (?, ?)`, id, name)
	if err != nil {
		if isUniqueViolation(err) {
			if err2 := tx.QueryRow(`SELECT id FROM tags WHERE name = ?`, name).Scan(&id); err2 != nil {
				return "", err2
			}
			return id, nil
		}
		return "", err
	}
	return id, nil
}

// Create inserts an asset and its tags.
func (s *Store) Create(in *createRequest, now time.Time) (*Asset, error) {
	if in.OwnerID != nil && *in.OwnerID != "" {
		ok, err := s.userExists(*in.OwnerID)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("%w: owner_id", ErrInvalidOwner)
		}
	}
	if in.BackupOwnerID != nil && *in.BackupOwnerID != "" {
		ok, err := s.userExists(*in.BackupOwnerID)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("%w: backup_owner_id", ErrInvalidOwner)
		}
	}

	id, err := newID()
	if err != nil {
		return nil, err
	}
	ipsJSON, err := encodeIPs(in.AdditionalIPs)
	if err != nil {
		return nil, err
	}
	ts := formatTime(now)

	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var owner, backup any
	if in.OwnerID != nil && *in.OwnerID != "" {
		owner = *in.OwnerID
	}
	if in.BackupOwnerID != nil && *in.BackupOwnerID != "" {
		backup = *in.BackupOwnerID
	}

	_, err = tx.Exec(
		`INSERT INTO assets (
			id, name, hostname, asset_type, os_family, os_detail, environment, purpose,
			primary_ip, additional_ips, location, hypervisor, owner_id, backup_owner_id,
			status, config_notes, deleted_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, ?, ?)`,
		id, in.Name, in.Hostname, in.AssetType, in.OSFamily, in.OSDetail, in.Environment, in.Purpose,
		in.PrimaryIP, ipsJSON, in.Location, in.Hypervisor, owner, backup,
		in.Status, in.ConfigNotes, ts, ts,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrConflict
		}
		return nil, err
	}
	if err := s.replaceTagsTx(tx, id, in.Tags); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.Get(id)
}

// Get returns one asset by id (including soft-deleted).
func (s *Store) Get(id string) (*Asset, error) {
	row := s.db.QueryRow(`SELECT `+assetColumns+` FROM assets WHERE id = ?`, id)
	a, err := scanAsset(row)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	tags, err := s.loadTags(id)
	if err != nil {
		return nil, err
	}
	a.Tags = tags
	return a, nil
}

// GetDetail returns asset plus related summaries.
func (s *Store) GetDetail(id string) (*AssetDetail, error) {
	a, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	d := &AssetDetail{Asset: *a, Accounts: []AccountSummary{}, Jobs: []JobSummary{}, Notes: []NoteSummary{}}

	accRows, err := s.db.Query(
		`SELECT id, username, auth_type, description, last_rotated_at
		 FROM accounts WHERE asset_id = ? ORDER BY username COLLATE NOCASE`,
		id,
	)
	if err != nil {
		return nil, err
	}
	defer accRows.Close()
	for accRows.Next() {
		var acc AccountSummary
		var rotated sql.NullString
		if err := accRows.Scan(&acc.ID, &acc.Username, &acc.AuthType, &acc.Description, &rotated); err != nil {
			return nil, err
		}
		if rotated.Valid && rotated.String != "" {
			acc.LastRotatedAt = &rotated.String
		}
		d.Accounts = append(d.Accounts, acc)
	}
	if err := accRows.Err(); err != nil {
		return nil, err
	}

	jobRows, err := s.db.Query(
		`SELECT id, name, scheduler_type, enabled_doc FROM scheduled_jobs
		 WHERE asset_id = ? ORDER BY name COLLATE NOCASE`,
		id,
	)
	if err != nil {
		return nil, err
	}
	defer jobRows.Close()
	for jobRows.Next() {
		var j JobSummary
		var enabled int
		if err := jobRows.Scan(&j.ID, &j.Name, &j.SchedulerType, &enabled); err != nil {
			return nil, err
		}
		j.EnabledDoc = enabled != 0
		d.Jobs = append(d.Jobs, j)
	}
	if err := jobRows.Err(); err != nil {
		return nil, err
	}

	noteRows, err := s.db.Query(
		`SELECT id, title, created_at FROM asset_notes WHERE asset_id = ? ORDER BY created_at DESC`,
		id,
	)
	if err != nil {
		return nil, err
	}
	defer noteRows.Close()
	for noteRows.Next() {
		var n NoteSummary
		if err := noteRows.Scan(&n.ID, &n.Title, &n.CreatedAt); err != nil {
			return nil, err
		}
		d.Notes = append(d.Notes, n)
	}
	if err := noteRows.Err(); err != nil {
		return nil, err
	}
	return d, nil
}

// Update applies a PATCH to a non-deleted asset.
func (s *Store) Update(id string, in *patchRequest, now time.Time) (*Asset, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	row := tx.QueryRow(`SELECT `+assetColumns+` FROM assets WHERE id = ?`, id)
	cur, err := scanAsset(row)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if cur.DeletedAt != nil {
		return nil, ErrAlreadyDeleted
	}

	if in.Name != nil {
		cur.Name = *in.Name
	}
	if in.Hostname != nil {
		cur.Hostname = *in.Hostname
	}
	if in.AssetType != nil {
		cur.AssetType = *in.AssetType
	}
	if in.OSFamily != nil {
		cur.OSFamily = *in.OSFamily
	}
	if in.OSDetail != nil {
		cur.OSDetail = *in.OSDetail
	}
	if in.Environment != nil {
		cur.Environment = *in.Environment
	}
	if in.Purpose != nil {
		cur.Purpose = *in.Purpose
	}
	if in.PrimaryIP != nil {
		cur.PrimaryIP = *in.PrimaryIP
	}
	if in.AdditionalIPs != nil {
		cur.AdditionalIPs = *in.AdditionalIPs
	}
	if in.Location != nil {
		cur.Location = *in.Location
	}
	if in.Hypervisor != nil {
		cur.Hypervisor = *in.Hypervisor
	}
	if in.ConfigNotes != nil {
		cur.ConfigNotes = *in.ConfigNotes
	}
	if in.Status != nil {
		cur.Status = *in.Status
	}
	if in.OwnerIDSet {
		if in.OwnerID != nil && *in.OwnerID != "" {
			var n int
			if err := tx.QueryRow(`SELECT COUNT(*) FROM users WHERE id = ?`, *in.OwnerID).Scan(&n); err != nil {
				return nil, err
			}
			if n == 0 {
				return nil, fmt.Errorf("%w: owner_id", ErrInvalidOwner)
			}
			cur.OwnerID = in.OwnerID
		} else {
			cur.OwnerID = nil
		}
	}
	if in.BackupOwnerIDSet {
		if in.BackupOwnerID != nil && *in.BackupOwnerID != "" {
			var n int
			if err := tx.QueryRow(`SELECT COUNT(*) FROM users WHERE id = ?`, *in.BackupOwnerID).Scan(&n); err != nil {
				return nil, err
			}
			if n == 0 {
				return nil, fmt.Errorf("%w: backup_owner_id", ErrInvalidOwner)
			}
			cur.BackupOwnerID = in.BackupOwnerID
		} else {
			cur.BackupOwnerID = nil
		}
	}

	ipsJSON, err := encodeIPs(cur.AdditionalIPs)
	if err != nil {
		return nil, err
	}
	ts := formatTime(now)
	var owner, backup any
	if cur.OwnerID != nil {
		owner = *cur.OwnerID
	}
	if cur.BackupOwnerID != nil {
		backup = *cur.BackupOwnerID
	}

	_, err = tx.Exec(
		`UPDATE assets SET
			name = ?, hostname = ?, asset_type = ?, os_family = ?, os_detail = ?,
			environment = ?, purpose = ?, primary_ip = ?, additional_ips = ?,
			location = ?, hypervisor = ?, owner_id = ?, backup_owner_id = ?,
			status = ?, config_notes = ?, updated_at = ?
		 WHERE id = ? AND deleted_at IS NULL`,
		cur.Name, cur.Hostname, cur.AssetType, cur.OSFamily, cur.OSDetail,
		cur.Environment, cur.Purpose, cur.PrimaryIP, ipsJSON,
		cur.Location, cur.Hypervisor, owner, backup,
		cur.Status, cur.ConfigNotes, ts, id,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrConflict
		}
		return nil, err
	}

	if in.Tags != nil {
		if err := s.replaceTagsTx(tx, id, *in.Tags); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.Get(id)
}

// SoftDelete sets status=retired and deleted_at. When audit is non-nil, the
// audit row is written in the same transaction (rolled back on audit failure).
func (s *Store) SoftDelete(id string, now time.Time, audit *AuditWrite) (*Asset, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	ts := formatTime(now)
	res, err := tx.Exec(
		`UPDATE assets SET status = 'retired', deleted_at = ?, updated_at = ?
		 WHERE id = ? AND deleted_at IS NULL`,
		ts, ts, id,
	)
	if err != nil {
		return nil, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if n == 0 {
		var deleted sql.NullString
		err := tx.QueryRow(`SELECT deleted_at FROM assets WHERE id = ?`, id).Scan(&deleted)
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		if deleted.Valid && deleted.String != "" {
			return nil, ErrAlreadyDeleted
		}
		return nil, ErrNotFound
	}
	if audit != nil {
		if err := writeAuditTx(tx, audit.ActorID, audit.Action, "asset", &id, "success", audit.IP, audit.UserAgent, "{}"); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.Get(id)
}

// Purge hard-deletes a soft-deleted asset and cascaded children.
// Active assets must be soft-deleted first (ErrNotSoftDeleted).
func (s *Store) Purge(id string, audit *AuditWrite) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var deleted sql.NullString
	err = tx.QueryRow(`SELECT deleted_at FROM assets WHERE id = ?`, id).Scan(&deleted)
	if err == sql.ErrNoRows {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if !deleted.Valid || deleted.String == "" {
		return ErrNotSoftDeleted
	}

	res, err := tx.Exec(`DELETE FROM assets WHERE id = ?`, id)
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
	if audit != nil {
		if err := writeAuditTx(tx, audit.ActorID, audit.Action, "asset", &id, "success", audit.IP, audit.UserAgent, "{}"); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// List returns assets matching filter plus total count (before limit/offset).
func (s *Store) List(f ListFilter) ([]Asset, int, error) {
	f.Limit, f.Offset = clampLimitOffset(f.Limit, f.Offset)

	where := []string{"1=1"}
	args := []any{}

	if !f.IncludeDeleted {
		where = append(where, "a.deleted_at IS NULL")
	}
	if f.Environment != "" {
		where = append(where, "a.environment = ?")
		args = append(args, f.Environment)
	}
	if f.Status != "" {
		where = append(where, "a.status = ?")
		args = append(args, f.Status)
	}
	if f.Tag != "" {
		where = append(where, `EXISTS (
			SELECT 1 FROM asset_tags at
			INNER JOIN tags t ON t.id = at.tag_id
			WHERE at.asset_id = a.id AND t.name = ?
		)`)
		args = append(args, f.Tag)
	}
	if q := strings.TrimSpace(f.Q); q != "" {
		ftsQ, ok := search.BuildFTSQuery(q)
		if !ok {
			return []Asset{}, 0, nil
		}
		// FTS over non-secret fields; secrets never enter assets_fts.
		where = append(where, `EXISTS (
			SELECT 1 FROM assets_fts
			WHERE assets_fts.asset_id = a.id AND assets_fts MATCH ?
		)`)
		args = append(args, ftsQ)
	}

	whereSQL := strings.Join(where, " AND ")

	var total int
	countSQL := `SELECT COUNT(*) FROM assets a WHERE ` + whereSQL
	if err := s.db.QueryRow(countSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	listArgs := append(append([]any{}, args...), f.Limit, f.Offset)
	rows, err := s.db.Query(
		`SELECT a.id, a.name, a.hostname, a.asset_type, a.os_family, a.os_detail, a.environment, a.purpose,
			a.primary_ip, a.additional_ips, a.location, a.hypervisor, a.owner_id, a.backup_owner_id, a.status,
			a.config_notes, a.deleted_at, a.created_at, a.updated_at
		 FROM assets a
		 WHERE `+whereSQL+`
		 ORDER BY a.updated_at DESC, a.id DESC
		 LIMIT ? OFFSET ?`,
		listArgs...,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var items []Asset
	for rows.Next() {
		a, err := scanAsset(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *a)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if items == nil {
		items = []Asset{}
	}
	ids := make([]string, len(items))
	for i := range items {
		ids[i] = items[i].ID
		items[i].Tags = []string{}
	}
	byAsset, err := s.loadTagsForAssets(ids)
	if err != nil {
		return nil, 0, err
	}
	for i := range items {
		if tags, ok := byAsset[items[i].ID]; ok {
			items[i].Tags = tags
		}
	}
	return items, total, nil
}

func (s *Store) loadTagsForAssets(ids []string) (map[string][]string, error) {
	out := make(map[string][]string, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	rows, err := s.db.Query(
		`SELECT at.asset_id, t.name FROM asset_tags at
		 INNER JOIN tags t ON t.id = at.tag_id
		 WHERE at.asset_id IN (`+strings.Join(placeholders, ",")+`)
		 ORDER BY at.asset_id, t.name COLLATE NOCASE`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var assetID, name string
		if err := rows.Scan(&assetID, &name); err != nil {
			return nil, err
		}
		out[assetID] = append(out[assetID], name)
	}
	return out, rows.Err()
}

// WriteAudit inserts an audit_events row.
func (s *Store) WriteAudit(actorID *string, action, resourceType string, resourceID *string, outcome, ip, userAgent, metadata string) error {
	return writeAuditTx(s.db, actorID, action, resourceType, resourceID, outcome, ip, userAgent, metadata)
}

type execContexter interface {
	Exec(query string, args ...any) (sql.Result, error)
}

func writeAuditTx(db execContexter, actorID *string, action, resourceType string, resourceID *string, outcome, ip, userAgent, metadata string) error {
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
		id, actorID, action, resourceType, resourceID, outcome, ip, userAgent, metadata, formatTime(time.Now()),
	)
	return err
}
