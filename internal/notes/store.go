package notes

import (
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
)

const timeLayout = "2006-01-02T15:04:05.000Z"

var (
	ErrNotFound      = errors.New("note not found")
	ErrAssetNotFound = errors.New("asset not found")
	ErrAssetDeleted  = errors.New("asset is soft-deleted")
)

// Store persists asset note rows.
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

// Note is the API model for an asset_notes row.
type Note struct {
	ID        string  `json:"id"`
	AssetID   string  `json:"asset_id"`
	Title     string  `json:"title"`
	Body      string  `json:"body"`
	AuthorID  *string `json:"author_id"`
	CreatedAt string  `json:"created_at"`
	UpdatedAt string  `json:"updated_at"`
}

func scanNote(row interface {
	Scan(dest ...any) error
}) (*Note, error) {
	var n Note
	var author sql.NullString
	if err := row.Scan(
		&n.ID, &n.AssetID, &n.Title, &n.Body, &author, &n.CreatedAt, &n.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if author.Valid && author.String != "" {
		n.AuthorID = &author.String
	}
	return &n, nil
}

const noteColumns = `id, asset_id, title, body, author_id, created_at, updated_at`

type queryRower interface {
	QueryRow(query string, args ...any) *sql.Row
}

func assetWritable(q queryRower, assetID string) error {
	var deleted sql.NullString
	err := q.QueryRow(`SELECT deleted_at FROM assets WHERE id = ?`, assetID).Scan(&deleted)
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

// List returns notes for an asset with offset pagination.
func (s *Store) List(assetID string, limit, offset int) ([]Note, int, error) {
	if err := s.assetExists(assetID); err != nil {
		return nil, 0, err
	}
	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM asset_notes WHERE asset_id = ?`, assetID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.Query(
		`SELECT `+noteColumns+` FROM asset_notes
		 WHERE asset_id = ?
		 ORDER BY created_at DESC, id
		 LIMIT ? OFFSET ?`,
		assetID, limit, offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]Note, 0)
	for rows.Next() {
		n, err := scanNote(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *n)
	}
	return items, total, rows.Err()
}

// Create inserts a note under a non-deleted asset.
// INSERT…SELECT ties the soft-delete check to the insert so a concurrent soft-delete cannot race.
func (s *Store) Create(assetID string, in *createRequest, authorID *string, now time.Time) (*Note, error) {
	id, err := newID()
	if err != nil {
		return nil, err
	}
	ts := formatTime(now)
	var author any
	if authorID != nil && *authorID != "" {
		author = *authorID
	}

	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.Exec(
		`INSERT INTO asset_notes (id, asset_id, title, body, author_id, created_at, updated_at)
		 SELECT ?, a.id, ?, ?, ?, ?, ?
		 FROM assets a
		 WHERE a.id = ? AND a.deleted_at IS NULL`,
		id, in.Title, in.Body, author, ts, ts, assetID,
	)
	if err != nil {
		return nil, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if n == 0 {
		if err := assetWritable(tx, assetID); err != nil {
			return nil, err
		}
		return nil, ErrAssetNotFound
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.Get(id)
}

// Get returns one note by id.
func (s *Store) Get(id string) (*Note, error) {
	row := s.db.QueryRow(`SELECT `+noteColumns+` FROM asset_notes WHERE id = ?`, id)
	n, err := scanNote(row)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return n, nil
}

// Update applies a PATCH to an existing note on a non-deleted asset.
func (s *Store) Update(id string, in *patchRequest, now time.Time) (*Note, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	row := tx.QueryRow(`SELECT `+noteColumns+` FROM asset_notes WHERE id = ?`, id)
	cur, err := scanNote(row)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := assetWritable(tx, cur.AssetID); err != nil {
		return nil, err
	}

	if in.Title != nil {
		cur.Title = *in.Title
	}
	if in.Body != nil {
		cur.Body = *in.Body
	}
	ts := formatTime(now)
	res, err := tx.Exec(
		`UPDATE asset_notes SET title = ?, body = ?, updated_at = ? WHERE id = ?`,
		cur.Title, cur.Body, ts, id,
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
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.Get(id)
}

// Delete removes a note by id when its parent asset is not soft-deleted.
func (s *Store) Delete(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var assetID string
	err = tx.QueryRow(`SELECT asset_id FROM asset_notes WHERE id = ?`, id).Scan(&assetID)
	if err == sql.ErrNoRows {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if err := assetWritable(tx, assetID); err != nil {
		return err
	}
	res, err := tx.Exec(`DELETE FROM asset_notes WHERE id = ?`, id)
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
	return tx.Commit()
}
