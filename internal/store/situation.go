package store

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// SituationRow represents a tracked situation/event.
type SituationRow struct {
	ID          int64
	Name        string
	Slug        string
	Description string
	Status      string
	CreatedAt   string
	UpdatedAt   string
	ItemCount   int
	ParentID    *int64 // nil if top-level
	Children    []SituationRow // populated by ListSituationsTree
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = slugRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "situation"
	}
	return s
}

// UpsertSituation creates or finds a situation by name, returns its ID.
func UpsertSituation(ctx context.Context, db *sql.DB, name, description string) (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	slug := slugify(name)

	// Try insert first
	res, err := db.ExecContext(ctx, `
INSERT OR IGNORE INTO situations (name, slug, description, status, created_at, updated_at)
VALUES (?, ?, ?, 'active', ?, ?)
`, name, slug, description, now, now)
	if err != nil {
		return 0, err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		id, _ := res.LastInsertId()
		return id, nil
	}

	// Slug collision — try with suffix
	var id int64
	err = db.QueryRowContext(ctx, `SELECT id FROM situations WHERE slug = ?`, slug).Scan(&id)
	if err == nil {
		// Same slug exists — check if same name
		var existingName string
		_ = db.QueryRowContext(ctx, `SELECT name FROM situations WHERE id = ?`, id).Scan(&existingName)
		if existingName == name {
			return id, nil
		}
		// Different name, same slug — add suffix
		for i := 2; i < 100; i++ {
			suffixed := fmt.Sprintf("%s-%d", slug, i)
			res, err = db.ExecContext(ctx, `
INSERT OR IGNORE INTO situations (name, slug, description, status, created_at, updated_at)
VALUES (?, ?, ?, 'active', ?, ?)
`, name, suffixed, description, now, now)
			if err != nil {
				return 0, err
			}
			if n, _ := res.RowsAffected(); n > 0 {
				id, _ = res.LastInsertId()
				return id, nil
			}
		}
	}
	return id, err
}

// LinkSituationItem links an item to a situation (idempotent).
func LinkSituationItem(ctx context.Context, db *sql.DB, situationID, itemID int64) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.ExecContext(ctx, `
INSERT OR IGNORE INTO situation_items (situation_id, item_id, linked_at)
VALUES (?, ?, ?)
`, situationID, itemID, now)
	if err != nil {
		return err
	}
	// Update situation counts and timestamp
	_, err = db.ExecContext(ctx, `
UPDATE situations SET
	item_count = (SELECT COUNT(*) FROM situation_items WHERE situation_id = ?),
	updated_at = ?
WHERE id = ?
`, situationID, now, situationID)
	return err
}

// ListSituations returns situations filtered by status, newest first.
func ListSituations(ctx context.Context, db *sql.DB, status string, limit int) ([]SituationRow, error) {
	if limit <= 0 {
		limit = 20
	}
	where := "1=1"
	args := make([]any, 0, 2)
	if status != "" {
		where += " AND status = ?"
		args = append(args, status)
	}
	args = append(args, limit)

	rows, err := db.QueryContext(ctx, fmt.Sprintf(`
SELECT id, name, slug, description, status, created_at, updated_at, item_count, parent_id
FROM situations
WHERE %s
ORDER BY datetime(updated_at) DESC
LIMIT ?
`, where), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SituationRow
	for rows.Next() {
		var s SituationRow
		var parentID sql.NullInt64
		if err := rows.Scan(&s.ID, &s.Name, &s.Slug, &s.Description, &s.Status,
			&s.CreatedAt, &s.UpdatedAt, &s.ItemCount, &parentID); err != nil {
			return nil, err
		}
		if parentID.Valid {
			pid := parentID.Int64
			s.ParentID = &pid
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// GetSituation returns a single situation by slug.
func GetSituation(ctx context.Context, db *sql.DB, slug string) (SituationRow, error) {
	var s SituationRow
	var parentID sql.NullInt64
	err := db.QueryRowContext(ctx, `
SELECT id, name, slug, description, status, created_at, updated_at, item_count, parent_id
FROM situations WHERE slug = ?
`, slug).Scan(&s.ID, &s.Name, &s.Slug, &s.Description, &s.Status,
		&s.CreatedAt, &s.UpdatedAt, &s.ItemCount, &parentID)
	if parentID.Valid {
		pid := parentID.Int64
		s.ParentID = &pid
	}
	return s, err
}

// SetSituationParent sets the parent of a situation.
func SetSituationParent(ctx context.Context, db *sql.DB, childID, parentID int64) error {
	_, err := db.ExecContext(ctx, `UPDATE situations SET parent_id = ? WHERE id = ?`, parentID, childID)
	return err
}

// ListChildSituations returns sub-situations of a parent.
func ListChildSituations(ctx context.Context, db *sql.DB, parentID int64) ([]SituationRow, error) {
	rows, err := db.QueryContext(ctx, `
SELECT id, name, slug, description, status, created_at, updated_at, item_count, parent_id
FROM situations WHERE parent_id = ?
ORDER BY item_count DESC
`, parentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SituationRow
	for rows.Next() {
		var s SituationRow
		var parentID sql.NullInt64
		if err := rows.Scan(&s.ID, &s.Name, &s.Slug, &s.Description, &s.Status,
			&s.CreatedAt, &s.UpdatedAt, &s.ItemCount, &parentID); err != nil {
			return nil, err
		}
		if parentID.Valid {
			pid := parentID.Int64
			s.ParentID = &pid
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ListSituationsTree returns top-level situations with children populated.
func ListSituationsTree(ctx context.Context, db *sql.DB, status string, limit int) ([]SituationRow, error) {
	all, err := ListSituations(ctx, db, status, 100)
	if err != nil {
		return nil, err
	}
	byID := make(map[int64]*SituationRow)
	var roots []SituationRow
	for i := range all {
		byID[all[i].ID] = &all[i]
	}
	for i := range all {
		if all[i].ParentID != nil {
			if parent, ok := byID[*all[i].ParentID]; ok {
				parent.Children = append(parent.Children, all[i])
				continue
			}
		}
		roots = append(roots, all[i])
	}
	if limit > 0 && len(roots) > limit {
		roots = roots[:limit]
	}
	return roots, nil
}

// AutoLinkSituationHierarchy detects sub-situations and links them to parents
// based on entity overlap between their item sets.
func AutoLinkSituationHierarchy(ctx context.Context, db *sql.DB, minOverlap float64) int {
	if minOverlap <= 0 {
		minOverlap = 0.6 // 60% of child's entities must appear in parent
	}
	sits, err := ListSituations(ctx, db, "active", 100)
	if err != nil || len(sits) < 2 {
		return 0
	}

	// Build entity sets for each situation
	type sitEnts struct {
		ID       int64
		Count    int
		Entities map[int64]bool
	}
	var all []sitEnts
	for _, s := range sits {
		if s.ParentID != nil {
			continue // already has a parent
		}
		ents := make(map[int64]bool)
		rows, err := db.QueryContext(ctx, `
SELECT DISTINCT ie.entity_id
FROM situation_items si
JOIN item_entities ie ON si.item_id = ie.item_id
WHERE si.situation_id = ?
`, s.ID)
		if err != nil {
			continue
		}
		for rows.Next() {
			var eid int64
			rows.Scan(&eid)
			ents[eid] = true
		}
		rows.Close()
		all = append(all, sitEnts{ID: s.ID, Count: s.ItemCount, Entities: ents})
	}

	linked := 0
	for i := range all {
		for j := range all {
			if i == j || all[i].Count >= all[j].Count {
				continue // child must be smaller than parent
			}
			if len(all[i].Entities) == 0 {
				continue
			}
			// Count how many of child's entities appear in parent
			overlap := 0
			for eid := range all[i].Entities {
				if all[j].Entities[eid] {
					overlap++
				}
			}
			ratio := float64(overlap) / float64(len(all[i].Entities))
			if ratio >= minOverlap {
				if err := SetSituationParent(ctx, db, all[i].ID, all[j].ID); err == nil {
					linked++
				}
				break // only one parent
			}
		}
	}
	return linked
}

// ListSituationItems returns items linked to a situation, newest first.
func ListSituationItems(ctx context.Context, db *sql.DB, situationID int64, limit int) ([]ListedItem, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := db.QueryContext(ctx, `
SELECT i.id, i.created_at, i.source_kind, i.title, COALESCE(i.summary,''),
       COALESCE(i.url,''), COALESCE(i.feed_url,''), i.urgency,
       COALESCE(i.lang,''), COALESCE(i.title_translated,''),
       COALESCE(i.summary_translated,''), COALESCE(i.tags_json,'[]'),
       COALESCE(i.cluster_key,'')
FROM items i
JOIN situation_items si ON i.id = si.item_id
WHERE si.situation_id = ?
ORDER BY datetime(i.created_at) DESC
LIMIT ?
`, situationID, limit)
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

// GetItemSituations returns situations an item belongs to.
func GetItemSituations(ctx context.Context, db *sql.DB, itemID int64) ([]SituationRow, error) {
	rows, err := db.QueryContext(ctx, `
SELECT s.id, s.name, s.slug, s.description, s.status, s.created_at, s.updated_at, s.item_count, s.parent_id
FROM situations s
JOIN situation_items si ON s.id = si.situation_id
WHERE si.item_id = ?
ORDER BY s.item_count DESC
`, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SituationRow
	for rows.Next() {
		var s SituationRow
		var parentID sql.NullInt64
		if err := rows.Scan(&s.ID, &s.Name, &s.Slug, &s.Description, &s.Status,
			&s.CreatedAt, &s.UpdatedAt, &s.ItemCount, &parentID); err != nil {
			return nil, err
		}
		if parentID.Valid {
			pid := parentID.Int64
			s.ParentID = &pid
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
