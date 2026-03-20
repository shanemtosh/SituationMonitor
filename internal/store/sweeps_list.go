package store

import (
	"context"
	"database/sql"
)

// SweepRow is a sweep history row for the API / UI.
type SweepRow struct {
	ID         int64
	StartedAt  string
	FinishedAt string
	Model      string
	Version    string
	Status     string
	Error      string
}

// ListSweeps returns recent sweeps newest-first.
func ListSweeps(ctx context.Context, db *sql.DB, limit int) ([]SweepRow, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 200 {
		limit = 200
	}
	rows, err := db.QueryContext(ctx, `
SELECT id, started_at, COALESCE(finished_at,''), model, prompt_version, status, COALESCE(error_message,'')
FROM sweeps
ORDER BY id DESC
LIMIT ?
`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SweepRow
	for rows.Next() {
		var s SweepRow
		if err := rows.Scan(&s.ID, &s.StartedAt, &s.FinishedAt, &s.Model, &s.Version, &s.Status, &s.Error); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
