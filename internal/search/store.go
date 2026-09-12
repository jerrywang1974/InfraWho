package search

import (
	"database/sql"
	"strings"
)

const (
	defaultListLimit = 50
	maxListLimit     = 200
)

// Store queries the assets_fts index.
type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// Filter narrows FTS hits to asset list semantics.
type Filter struct {
	Q              string
	Limit          int
	Offset         int
	Tag            string
	Environment    string
	Status         string
	IncludeDeleted bool
}

// Hit is one ranked asset match (no secrets).
type Hit struct {
	AssetID     string  `json:"asset_id"`
	Name        string  `json:"name"`
	Hostname    string  `json:"hostname"`
	Purpose     string  `json:"purpose"`
	Environment string  `json:"environment"`
	Status      string  `json:"status"`
	Rank        float64 `json:"rank"`
}

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

// SearchAssets returns assets matching q via FTS5, ordered by rank then updated_at.
// Empty/unusable q yields zero hits (not an unfiltered list).
func (s *Store) SearchAssets(f Filter) ([]Hit, int, error) {
	f.Limit, f.Offset = clampLimitOffset(f.Limit, f.Offset)

	ftsQ, ok := BuildFTSQuery(f.Q)
	if !ok {
		return []Hit{}, 0, nil
	}

	where := []string{"assets_fts MATCH ?"}
	args := []any{ftsQ}

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

	whereSQL := strings.Join(where, " AND ")

	var total int
	countSQL := `SELECT COUNT(*)
		FROM assets_fts
		INNER JOIN assets a ON a.id = assets_fts.asset_id
		WHERE ` + whereSQL
	if err := s.db.QueryRow(countSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	listArgs := append(append([]any{}, args...), f.Limit, f.Offset)
	rows, err := s.db.Query(
		`SELECT a.id, a.name, a.hostname, a.purpose, a.environment, a.status, assets_fts.rank
		 FROM assets_fts
		 INNER JOIN assets a ON a.id = assets_fts.asset_id
		 WHERE `+whereSQL+`
		 ORDER BY assets_fts.rank, a.updated_at DESC, a.id DESC
		 LIMIT ? OFFSET ?`,
		listArgs...,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := []Hit{}
	for rows.Next() {
		var h Hit
		if err := rows.Scan(&h.AssetID, &h.Name, &h.Hostname, &h.Purpose, &h.Environment, &h.Status, &h.Rank); err != nil {
			return nil, 0, err
		}
		items = append(items, h)
	}
	return items, total, rows.Err()
}

// MatchingAssetIDs returns asset IDs matching q (unordered). ok=false means empty q / no tokens.
func MatchingAssetIDs(db *sql.DB, q string) (ids []string, ok bool, err error) {
	ftsQ, ok := BuildFTSQuery(q)
	if !ok {
		return nil, false, nil
	}
	rows, err := db.Query(`SELECT asset_id FROM assets_fts WHERE assets_fts MATCH ?`, ftsQ)
	if err != nil {
		return nil, true, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, true, err
		}
		ids = append(ids, id)
	}
	if ids == nil {
		ids = []string{}
	}
	return ids, true, rows.Err()
}

// FTSContainsSecretColumn is a test helper asserting secret_payloads is absent from the FTS source view.
func FTSContainsSecretColumn(db *sql.DB) (bool, error) {
	var sqlText string
	err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE name = 'assets_fts_src'`).Scan(&sqlText)
	if err != nil {
		return false, err
	}
	lower := strings.ToLower(sqlText)
	return strings.Contains(lower, "secret_payloads") || strings.Contains(lower, "ciphertext"), nil
}
