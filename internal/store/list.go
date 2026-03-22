package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// ListedItem is a row for the dashboard / API.
type ListedItem struct {
	ID           int64
	CreatedAt    string
	SourceKind   string
	Title        string
	Summary      string
	URL          string
	FeedURL      string
	Urgency      int
	Lang         string
	TitleTrans   string
	SummaryTrans string
	TagsJSON     string
	ClusterKey   string
}

// ItemFilter limits listed items.
type ItemFilter struct {
	SourceKind string // empty = all
	MinUrgency int    // 0 = no minimum
	Hours      int    // 0 = no time cutoff (max 60 days enforced)
	Limit      int
	Region     string // empty = all; filters by feed_url using FeedRegionMap
}

// FeedRegionMap maps feed URL → region tag. Populated at startup from feeds.txt.
var FeedRegionMap map[string]string

// ListItems returns items newest-first.
func ListItems(ctx context.Context, db *sql.DB, f ItemFilter) ([]ListedItem, error) {
	if f.Limit <= 0 {
		f.Limit = 50
	}
	if f.Limit > 500 {
		f.Limit = 500
	}

	where := "1=1"
	args := make([]any, 0, 4)
	if f.SourceKind != "" {
		where += " AND source_kind = ?"
		args = append(args, f.SourceKind)
	}
	if f.MinUrgency > 0 {
		where += " AND urgency >= ?"
		args = append(args, f.MinUrgency)
	}
	if f.Hours > 0 {
		h := f.Hours
		if h > 24*60 {
			h = 24 * 60
		}
		where += " AND datetime(created_at) >= datetime('now', ?)"
		args = append(args, fmt.Sprintf("-%d hours", h))
	}
	if f.Region != "" && FeedRegionMap != nil {
		var urls []string
		for u, r := range FeedRegionMap {
			if r == f.Region {
				urls = append(urls, u)
			}
		}
		if len(urls) > 0 {
			placeholders := make([]string, len(urls))
			for i, u := range urls {
				placeholders[i] = "?"
				args = append(args, u)
			}
			where += " AND feed_url IN (" + strings.Join(placeholders, ",") + ")"
		} else {
			// Region specified but no feeds match — return nothing
			where += " AND 0"
		}
	}

	q := fmt.Sprintf(`
SELECT id, created_at, source_kind, title, COALESCE(summary,''), COALESCE(url,''), COALESCE(feed_url,''),
	urgency, COALESCE(lang,''), COALESCE(title_translated,''), COALESCE(summary_translated,''),
	COALESCE(tags_json,'[]'), COALESCE(cluster_key,'')
FROM items
WHERE %s
ORDER BY datetime(created_at) DESC
LIMIT ?
`, where)

	args = append(args, f.Limit)
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ListedItem
	for rows.Next() {
		var it ListedItem
		if err := rows.Scan(&it.ID, &it.CreatedAt, &it.SourceKind, &it.Title, &it.Summary, &it.URL, &it.FeedURL,
			&it.Urgency, &it.Lang, &it.TitleTrans, &it.SummaryTrans, &it.TagsJSON, &it.ClusterKey); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}
