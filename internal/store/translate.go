package store

import (
	"context"
	"database/sql"
	"time"
)

// ItemRow is a minimal item projection for the translation worker.
type ItemRow struct {
	ID      int64
	Title   string
	Summary string
}

// ListUntranslated returns items missing title_translated (bounded).
func ListUntranslated(ctx context.Context, db *sql.DB, limit int) ([]ItemRow, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := db.QueryContext(ctx, `
SELECT id, title, COALESCE(summary,'')
FROM items
WHERE (title_translated IS NULL OR title_translated = '')
ORDER BY datetime(created_at) DESC
LIMIT ?
`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ItemRow
	for rows.Next() {
		var r ItemRow
		if err := rows.Scan(&r.ID, &r.Title, &r.Summary); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// SetTranslation updates language + translated fields for an item.
func SetTranslation(ctx context.Context, db *sql.DB, id int64, lang, titleTr, summaryTr, model string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.ExecContext(ctx, `
UPDATE items SET
	lang = ?,
	title_translated = ?,
	summary_translated = ?,
	translator_model = ?,
	translated_at = ?
WHERE id = ?
`, lang, titleTr, summaryTr, model, now, id)
	return err
}
