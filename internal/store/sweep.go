package store

import (
	"context"
	"database/sql"
	"time"
)

// SourceSweep is source_kind for OpenRouter / situation sweep rows.
const SourceSweep = "sweep"

// BeginSweep inserts a running sweep row and returns its id.
func BeginSweep(ctx context.Context, db *sql.DB, model, promptVersion string) (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := db.ExecContext(ctx, `
INSERT INTO sweeps (started_at, model, prompt_version, status)
VALUES (?, ?, ?, 'running')
`, now, model, promptVersion)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// FinishSweep marks a sweep finished and stores optional error + truncated JSON for debugging.
func FinishSweep(ctx context.Context, db *sql.DB, id int64, status, errMsg, responseJSON string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	const maxJSON = 512 * 1024
	if len(responseJSON) > maxJSON {
		responseJSON = responseJSON[:maxJSON] + "…"
	}
	_, err := db.ExecContext(ctx, `
UPDATE sweeps SET finished_at = ?, status = ?, error_message = ?, response_json = ?
WHERE id = ?
`, now, status, nullString(errMsg), nullString(responseJSON), id)
	return err
}

// UpsertSweepItem inserts or updates a story produced by a sweep.
func UpsertSweepItem(ctx context.Context, db *sql.DB, sweepID int64, externalID, title, summary, url string, urgency int, tagsJSON, clusterKey string) error {
	if externalID == "" {
		return nil
	}
	if urgency < 1 {
		urgency = 1
	}
	if urgency > 5 {
		urgency = 5
	}
	if tagsJSON == "" {
		tagsJSON = "[]"
	}
	now := time.Now().UTC().Format(time.RFC3339)

	const q = `
INSERT INTO items (created_at, source_kind, external_id, title, summary, url, urgency, tags_json, cluster_key, sweep_id)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(source_kind, external_id) DO UPDATE SET
	title = excluded.title,
	summary = excluded.summary,
	url = excluded.url,
	urgency = excluded.urgency,
	tags_json = excluded.tags_json,
	cluster_key = excluded.cluster_key,
	sweep_id = excluded.sweep_id
`
	var u any
	if url != "" {
		u = url
	}
	_, err := db.ExecContext(ctx, q,
		now,
		SourceSweep,
		externalID,
		title,
		nullString(summary),
		u,
		urgency,
		tagsJSON,
		nullString(clusterKey),
		sweepID,
	)
	return err
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
