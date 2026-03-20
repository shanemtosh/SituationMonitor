package store

import (
	"context"
	"database/sql"
	"time"
)

// SourceRSS is the source_kind value for syndicated feed articles.
const SourceRSS = "rss"

// RSSArticle is a normalized row for upsert into items.
type RSSArticle struct {
	ExternalID string
	Title      string
	Summary    string
	URL        string
	FeedURL    string
	Published  time.Time
}

// UpsertRSS inserts or updates an RSS-derived item. Empty ExternalID is a no-op.
func UpsertRSS(ctx context.Context, db *sql.DB, a RSSArticle) error {
	if a.ExternalID == "" {
		return nil
	}
	created := a.Published
	if created.IsZero() {
		created = time.Now().UTC()
	}
	createdStr := created.UTC().Format(time.RFC3339)

	const q = `
INSERT INTO items (created_at, source_kind, external_id, title, summary, url, feed_url, urgency, tags_json)
VALUES (?, ?, ?, ?, ?, ?, ?, 3, '[]')
ON CONFLICT(source_kind, external_id) DO UPDATE SET
	title = excluded.title,
	summary = excluded.summary,
	url = excluded.url,
	feed_url = excluded.feed_url
`
	var summary any
	if a.Summary != "" {
		summary = a.Summary
	}
	var url any
	if a.URL != "" {
		url = a.URL
	}
	var feedURL any
	if a.FeedURL != "" {
		feedURL = a.FeedURL
	}

	_, err := db.ExecContext(ctx, q,
		createdStr,
		SourceRSS,
		a.ExternalID,
		a.Title,
		summary,
		url,
		feedURL,
	)
	return err
}
