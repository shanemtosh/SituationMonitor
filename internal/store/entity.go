package store

import (
	"context"
	"database/sql"
	"time"
)

// EntityRow represents a row from the entities table.
type EntityRow struct {
	ID        int64
	Name      string
	Kind      string
	FirstSeen string
	LastSeen  string
	ItemCount int
}

// RelatedItemMatch is an item that shares entities with a pivot item.
type RelatedItemMatch struct {
	ItemID  int64
	Overlap int
}

// UpsertEntity inserts or updates an entity, returning its ID.
func UpsertEntity(ctx context.Context, db *sql.DB, name, kind string) (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.ExecContext(ctx, `
INSERT INTO entities (name, kind, first_seen, last_seen, item_count)
VALUES (?, ?, ?, ?, 1)
ON CONFLICT(name, kind) DO UPDATE SET
	last_seen = ?,
	item_count = item_count + 1
`, name, kind, now, now, now)
	if err != nil {
		return 0, err
	}
	var id int64
	err = db.QueryRowContext(ctx, `SELECT id FROM entities WHERE name = ? AND kind = ?`, name, kind).Scan(&id)
	return id, err
}

// SetItemEntities replaces all entity links for an item.
func SetItemEntities(ctx context.Context, db *sql.DB, itemID int64, entityIDs []int64) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM item_entities WHERE item_id = ?`, itemID); err != nil {
		return err
	}
	for _, eid := range entityIDs {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO item_entities (item_id, entity_id) VALUES (?, ?)`, itemID, eid); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// FindRelatedItems returns items sharing entity overlap with a given item.
func FindRelatedItems(ctx context.Context, db *sql.DB, itemID int64, minOverlap, limit int) ([]RelatedItemMatch, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := db.QueryContext(ctx, `
SELECT ie2.item_id, COUNT(*) as overlap
FROM item_entities ie1
JOIN item_entities ie2 ON ie1.entity_id = ie2.entity_id
WHERE ie1.item_id = ?
  AND ie2.item_id != ?
GROUP BY ie2.item_id
HAVING overlap >= ?
ORDER BY overlap DESC, ie2.item_id DESC
LIMIT ?
`, itemID, itemID, minOverlap, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []RelatedItemMatch
	for rows.Next() {
		var m RelatedItemMatch
		if err := rows.Scan(&m.ItemID, &m.Overlap); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// GetEntityByName returns an entity by its canonical name.
func GetEntityByName(ctx context.Context, db *sql.DB, name string) (EntityRow, error) {
	var e EntityRow
	err := db.QueryRowContext(ctx, `
SELECT id, name, kind, first_seen, last_seen, item_count
FROM entities WHERE name = ?
`, name).Scan(&e.ID, &e.Name, &e.Kind, &e.FirstSeen, &e.LastSeen, &e.ItemCount)
	return e, err
}

// ListEntityItems returns recent items mentioning an entity.
func ListEntityItems(ctx context.Context, db *sql.DB, entityID int64, limit int) ([]ListedItem, error) {
	if limit <= 0 {
		limit = 30
	}
	rows, err := db.QueryContext(ctx, `
SELECT i.id, i.created_at, i.source_kind, i.title, COALESCE(i.summary,''),
       COALESCE(i.url,''), COALESCE(i.feed_url,''), i.urgency,
       COALESCE(i.lang,''), COALESCE(i.title_translated,''),
       COALESCE(i.summary_translated,''), COALESCE(i.tags_json,'[]'),
       COALESCE(i.cluster_key,'')
FROM items i
JOIN item_entities ie ON i.id = ie.item_id
WHERE ie.entity_id = ?
ORDER BY datetime(i.created_at) DESC
LIMIT ?
`, entityID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ListedItem
	for rows.Next() {
		var it ListedItem
		if err := rows.Scan(&it.ID, &it.CreatedAt, &it.SourceKind, &it.Title, &it.Summary,
			&it.URL, &it.FeedURL, &it.Urgency, &it.Lang, &it.TitleTrans,
			&it.SummaryTrans, &it.TagsJSON, &it.ClusterKey); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// GetItemEntities returns entity rows linked to an item.
func GetItemEntities(ctx context.Context, db *sql.DB, itemID int64) ([]EntityRow, error) {
	rows, err := db.QueryContext(ctx, `
SELECT e.id, e.name, e.kind, e.first_seen, e.last_seen, e.item_count
FROM entities e
JOIN item_entities ie ON e.id = ie.entity_id
WHERE ie.item_id = ?
ORDER BY e.item_count DESC
`, itemID)
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

// SearchEntities returns entities matching a name prefix.
func SearchEntities(ctx context.Context, db *sql.DB, query string, limit int) ([]EntityRow, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := db.QueryContext(ctx, `
SELECT id, name, kind, first_seen, last_seen, item_count
FROM entities
WHERE name LIKE ? || '%'
ORDER BY item_count DESC
LIMIT ?
`, query, limit)
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
