package jobs

import (
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
)

const timeLayout = "2006-01-02T15:04:05.000Z"

var (
	ErrNotFound      = errors.New("job not found")
	ErrAssetNotFound = errors.New("asset not found")
	ErrAssetDeleted  = errors.New("asset is soft-deleted")
)

// Store persists scheduled job documentation rows.
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

// Job is the API model for a scheduled_jobs row.
type Job struct {
	ID            string `json:"id"`
	AssetID       string `json:"asset_id"`
	Name          string `json:"name"`
	SchedulerType string `json:"scheduler_type"`
	ScheduleExpr  string `json:"schedule_expr"`
	CommandOrPath string `json:"command_or_path"`
	Description   string `json:"description"`
	EnabledDoc    bool   `json:"enabled_doc"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

func scanJob(row interface {
	Scan(dest ...any) error
}) (*Job, error) {
	var j Job
	var enabled int
	if err := row.Scan(
		&j.ID, &j.AssetID, &j.Name, &j.SchedulerType, &j.ScheduleExpr,
		&j.CommandOrPath, &j.Description, &enabled, &j.CreatedAt, &j.UpdatedAt,
	); err != nil {
		return nil, err
	}
	j.EnabledDoc = enabled != 0
	return &j, nil
}

const jobColumns = `id, asset_id, name, scheduler_type, schedule_expr, command_or_path,
	description, enabled_doc, created_at, updated_at`

func (s *Store) assetWritable(assetID string) error {
	var deleted sql.NullString
	err := s.db.QueryRow(`SELECT deleted_at FROM assets WHERE id = ?`, assetID).Scan(&deleted)
	if err == sql.ErrNoRows {
		return ErrAssetNotFound
	}
	if err != nil {
		return err
	}
	if deleted.Valid && deleted.String != "" {
		return ErrAssetDeleted
	}
	return nil
}

func (s *Store) assetExists(assetID string) error {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM assets WHERE id = ?`, assetID).Scan(&n)
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrAssetNotFound
	}
	return nil
}

// List returns jobs for an asset with offset pagination.
func (s *Store) List(assetID string, limit, offset int) ([]Job, int, error) {
	if err := s.assetExists(assetID); err != nil {
		return nil, 0, err
	}
	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM scheduled_jobs WHERE asset_id = ?`, assetID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.Query(
		`SELECT `+jobColumns+` FROM scheduled_jobs
		 WHERE asset_id = ?
		 ORDER BY name COLLATE NOCASE, id
		 LIMIT ? OFFSET ?`,
		assetID, limit, offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]Job, 0)
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *j)
	}
	return items, total, rows.Err()
}

// Create inserts a job under a non-deleted asset.
func (s *Store) Create(assetID string, in *createRequest, now time.Time) (*Job, error) {
	if err := s.assetWritable(assetID); err != nil {
		return nil, err
	}
	id, err := newID()
	if err != nil {
		return nil, err
	}
	ts := formatTime(now)
	enabled := 0
	if in.EnabledDoc != nil && *in.EnabledDoc {
		enabled = 1
	}
	_, err = s.db.Exec(
		`INSERT INTO scheduled_jobs (
			id, asset_id, name, scheduler_type, schedule_expr, command_or_path,
			description, enabled_doc, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, assetID, in.Name, in.SchedulerType, in.ScheduleExpr, in.CommandOrPath,
		in.Description, enabled, ts, ts,
	)
	if err != nil {
		return nil, err
	}
	return s.Get(id)
}

// Get returns one job by id.
func (s *Store) Get(id string) (*Job, error) {
	row := s.db.QueryRow(`SELECT `+jobColumns+` FROM scheduled_jobs WHERE id = ?`, id)
	j, err := scanJob(row)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return j, nil
}

// Update applies a PATCH to an existing job.
func (s *Store) Update(id string, in *patchRequest, now time.Time) (*Job, error) {
	cur, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	if in.Name != nil {
		cur.Name = *in.Name
	}
	if in.SchedulerType != nil {
		cur.SchedulerType = *in.SchedulerType
	}
	if in.ScheduleExpr != nil {
		cur.ScheduleExpr = *in.ScheduleExpr
	}
	if in.CommandOrPath != nil {
		cur.CommandOrPath = *in.CommandOrPath
	}
	if in.Description != nil {
		cur.Description = *in.Description
	}
	if in.EnabledDoc != nil {
		cur.EnabledDoc = *in.EnabledDoc
	}
	enabled := 0
	if cur.EnabledDoc {
		enabled = 1
	}
	ts := formatTime(now)
	res, err := s.db.Exec(
		`UPDATE scheduled_jobs SET
			name = ?, scheduler_type = ?, schedule_expr = ?, command_or_path = ?,
			description = ?, enabled_doc = ?, updated_at = ?
		 WHERE id = ?`,
		cur.Name, cur.SchedulerType, cur.ScheduleExpr, cur.CommandOrPath,
		cur.Description, enabled, ts, id,
	)
	if err != nil {
		return nil, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, ErrNotFound
	}
	return s.Get(id)
}

// Delete removes a job by id.
func (s *Store) Delete(id string) error {
	res, err := s.db.Exec(`DELETE FROM scheduled_jobs WHERE id = ?`, id)
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
	return nil
}
