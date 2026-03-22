package store

import (
	"context"
	"database/sql"
	"time"
)

// RenameSituation changes a situation's name and slug.
func RenameSituation(ctx context.Context, db *sql.DB, id int64, name string) error {
	slug := slugify(name)
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.ExecContext(ctx, `UPDATE situations SET name = ?, slug = ?, updated_at = ? WHERE id = ?`,
		name, slug, now, id)
	return err
}

// SetSituationStatus changes a situation's status (active/resolved/watching).
func SetSituationStatus(ctx context.Context, db *sql.DB, id int64, status string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.ExecContext(ctx, `UPDATE situations SET status = ?, updated_at = ? WHERE id = ?`,
		status, now, id)
	return err
}

// MergeSituations merges situation fromID into toID.
// All item links are moved, then fromID is deleted.
func MergeSituations(ctx context.Context, db *sql.DB, fromID, toID int64) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := time.Now().UTC().Format(time.RFC3339)

	// Move item links (skip duplicates)
	_, err = tx.ExecContext(ctx, `
INSERT OR IGNORE INTO situation_items (situation_id, item_id, linked_at)
SELECT ?, item_id, ? FROM situation_items WHERE situation_id = ?
`, toID, now, fromID)
	if err != nil {
		return err
	}

	// Re-parent any children of fromID
	_, err = tx.ExecContext(ctx, `UPDATE situations SET parent_id = ? WHERE parent_id = ?`, toID, fromID)
	if err != nil {
		return err
	}

	// Delete old links and situation
	_, _ = tx.ExecContext(ctx, `DELETE FROM situation_items WHERE situation_id = ?`, fromID)
	_, _ = tx.ExecContext(ctx, `DELETE FROM situations WHERE id = ?`, fromID)

	// Update count on target
	_, err = tx.ExecContext(ctx, `
UPDATE situations SET
	item_count = (SELECT COUNT(*) FROM situation_items WHERE situation_id = ?),
	updated_at = ?
WHERE id = ?
`, toID, now, toID)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// DeleteSituation removes a situation and its item links.
func DeleteSituation(ctx context.Context, db *sql.DB, id int64) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Re-parent children to this situation's parent (or null)
	var parentID sql.NullInt64
	_ = db.QueryRowContext(ctx, `SELECT parent_id FROM situations WHERE id = ?`, id).Scan(&parentID)
	if parentID.Valid {
		_, _ = tx.ExecContext(ctx, `UPDATE situations SET parent_id = ? WHERE parent_id = ?`, parentID.Int64, id)
	} else {
		_, _ = tx.ExecContext(ctx, `UPDATE situations SET parent_id = NULL WHERE parent_id = ?`, id)
	}

	_, _ = tx.ExecContext(ctx, `DELETE FROM situation_items WHERE situation_id = ?`, id)
	_, _ = tx.ExecContext(ctx, `DELETE FROM situations WHERE id = ?`, id)
	return tx.Commit()
}

// RenameEntity changes an entity's name.
func RenameEntity(ctx context.Context, db *sql.DB, id int64, name string) error {
	_, err := db.ExecContext(ctx, `UPDATE entities SET name = ? WHERE id = ?`, name, id)
	return err
}

// DeleteEntity removes an entity and its item links.
func DeleteEntity(ctx context.Context, db *sql.DB, id int64) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, _ = tx.ExecContext(ctx, `DELETE FROM item_entities WHERE entity_id = ?`, id)
	_, _ = tx.ExecContext(ctx, `DELETE FROM entities WHERE id = ?`, id)
	return tx.Commit()
}

// ListTopEntities returns the most-referenced entities.
func ListTopEntities(ctx context.Context, db *sql.DB, kind string, limit int) ([]EntityRow, error) {
	if limit <= 0 {
		limit = 50
	}
	where := "canonical_id IS NULL"
	args := make([]any, 0, 2)
	if kind != "" {
		where += " AND kind = ?"
		args = append(args, kind)
	}
	args = append(args, limit)

	q := "SELECT id, name, kind, first_seen, last_seen, item_count FROM entities WHERE " + where + " ORDER BY item_count DESC LIMIT ?"
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []EntityRow
	for rows.Next() {
		var e EntityRow
		if err := rows.Scan(&e.ID, &e.Name, &e.Kind, &e.FirstSeen, &e.LastSeen, &e.ItemCount); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
