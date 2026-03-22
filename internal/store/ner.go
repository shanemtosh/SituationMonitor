package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// ListUnextracted returns items that have been translated but not yet entity-extracted.
func ListUnextracted(ctx context.Context, db *sql.DB, limit int) ([]ItemRow, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := db.QueryContext(ctx, `
SELECT id, title, COALESCE(summary,''), COALESCE(feed_url,'')
FROM items
WHERE entities_extracted_at IS NULL
  AND (title_translated IS NOT NULL AND title_translated != '')
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
		if err := rows.Scan(&r.ID, &r.Title, &r.Summary, &r.FeedURL); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// MarkExtracted sets entities_extracted_at for an item.
func MarkExtracted(ctx context.Context, db *sql.DB, id int64) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.ExecContext(ctx, `UPDATE items SET entities_extracted_at = ? WHERE id = ?`, now, id)
	return err
}

// SetClusterFromEntities updates an item's cluster_key based on entity overlap.
func SetClusterFromEntities(ctx context.Context, db *sql.DB, itemID int64, related []RelatedItemMatch) error {
	if len(related) == 0 {
		key := fmt.Sprintf("e:%d", itemID)
		_, err := db.ExecContext(ctx, `UPDATE items SET cluster_key = ? WHERE id = ?`, key, itemID)
		return err
	}
	// Use the cluster_key of the most-overlapping related item
	bestID := related[0].ItemID
	var clusterKey sql.NullString
	_ = db.QueryRowContext(ctx, `SELECT cluster_key FROM items WHERE id = ?`, bestID).Scan(&clusterKey)

	key := clusterKey.String
	if key == "" {
		key = fmt.Sprintf("e:%d", bestID)
	}
	_, err := db.ExecContext(ctx, `UPDATE items SET cluster_key = ? WHERE id = ?`, key, itemID)
	return err
}

// UnlinkedCluster is a cluster_key with enough items to form a situation.
type UnlinkedCluster struct {
	ClusterKey string
	ItemCount  int
}

// FindUnlinkedClusters returns entity-based clusters (e:*) with enough items
// that have no situation linkage yet.
func FindUnlinkedClusters(ctx context.Context, db *sql.DB, minItems int) ([]UnlinkedCluster, error) {
	if minItems <= 0 {
		minItems = 4
	}
	rows, err := db.QueryContext(ctx, `
SELECT i.cluster_key, COUNT(*) as n
FROM items i
LEFT JOIN situation_items si ON i.id = si.item_id
WHERE si.item_id IS NULL
  AND i.cluster_key IS NOT NULL
  AND i.cluster_key LIKE 'e:%'
GROUP BY i.cluster_key
HAVING n >= ?
ORDER BY n DESC
LIMIT 20
`, minItems)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []UnlinkedCluster
	for rows.Next() {
		var c UnlinkedCluster
		if err := rows.Scan(&c.ClusterKey, &c.ItemCount); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ListClusterItems returns items with a given cluster_key.
func ListClusterItems(ctx context.Context, db *sql.DB, clusterKey string, limit int) ([]ItemRow, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := db.QueryContext(ctx, `
SELECT id, title, COALESCE(summary,''), COALESCE(feed_url,'')
FROM items
WHERE cluster_key = ?
ORDER BY datetime(created_at) DESC
LIMIT ?
`, clusterKey, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ItemRow
	for rows.Next() {
		var r ItemRow
		if err := rows.Scan(&r.ID, &r.Title, &r.Summary, &r.FeedURL); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// BatchGetItems loads multiple items by ID.
func BatchGetItems(ctx context.Context, db *sql.DB, ids []int64) ([]ListedItem, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	q := fmt.Sprintf(`
SELECT id, created_at, source_kind, title, COALESCE(summary,''),
       COALESCE(url,''), COALESCE(feed_url,''), urgency,
       COALESCE(lang,''), COALESCE(title_translated,''),
       COALESCE(summary_translated,''), COALESCE(tags_json,'[]'),
       COALESCE(cluster_key,'')
FROM items
WHERE id IN (%s)
ORDER BY datetime(created_at) DESC
`, joinStrings(placeholders, ","))

	rows, err := db.QueryContext(ctx, q, args...)
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

func joinStrings(ss []string, sep string) string {
	result := ""
	for i, s := range ss {
		if i > 0 {
			result += sep
		}
		result += s
	}
	return result
}
