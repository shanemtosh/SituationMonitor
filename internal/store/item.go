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
	// Translation fields (optional, set during inline translation)
	Lang            string
	TitleTranslated string
	SummaryTranslated string
	TranslatorModel string
}

// ItemNeedsTranslation checks if an item is new or needs a translation retry.
// Returns false (skip translation) if the item exists with a translation, or if it's
// older than 24h without translation (permanently failed — don't keep retrying).
func ItemNeedsTranslation(ctx context.Context, db *sql.DB, externalID string) bool {
	var lang sql.NullString
	var createdAt string
	err := db.QueryRowContext(ctx,
		"SELECT COALESCE(lang,''), created_at FROM items WHERE source_kind = ? AND external_id = ? LIMIT 1",
		SourceRSS, externalID).Scan(&lang, &createdAt)
	if err != nil {
		return true // item doesn't exist — new item, needs translation
	}
	if lang.String != "" {
		return false // already translated (or marked as failed)
	}
	// Item exists but no translation — retry only if recent (< 24h)
	t, err := time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return true
	}
	return time.Since(t) < 24*time.Hour
}

// ItemExists checks if an item with the given external_id already exists.
func ItemExists(ctx context.Context, db *sql.DB, externalID string) bool {
	var n int
	err := db.QueryRowContext(ctx, "SELECT 1 FROM items WHERE source_kind = ? AND external_id = ? LIMIT 1", SourceRSS, externalID).Scan(&n)
	return err == nil
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
INSERT INTO items (created_at, source_kind, external_id, title, summary, url, feed_url, urgency, tags_json,
	lang, title_translated, summary_translated, translator_model, translated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, 3, '[]', ?, ?, ?, ?, ?)
ON CONFLICT(source_kind, external_id) DO UPDATE SET
	title = excluded.title,
	summary = excluded.summary,
	url = excluded.url,
	feed_url = excluded.feed_url,
	lang = COALESCE(excluded.lang, lang),
	title_translated = COALESCE(excluded.title_translated, title_translated),
	summary_translated = COALESCE(excluded.summary_translated, summary_translated),
	translator_model = COALESCE(excluded.translator_model, translator_model),
	translated_at = COALESCE(excluded.translated_at, translated_at)
`
	var summary, url, feedURL any
	if a.Summary != "" {
		summary = a.Summary
	}
	if a.URL != "" {
		url = a.URL
	}
	if a.FeedURL != "" {
		feedURL = a.FeedURL
	}

	var lang, titleTr, summaryTr, model, translatedAt any
	if a.Lang != "" {
		lang = a.Lang
		titleTr = a.TitleTranslated
		summaryTr = a.SummaryTranslated
		model = a.TranslatorModel
		translatedAt = time.Now().UTC().Format(time.RFC3339)
	}

	_, err := db.ExecContext(ctx, q,
		createdStr,
		SourceRSS,
		a.ExternalID,
		a.Title,
		summary,
		url,
		feedURL,
		lang,
		titleTr,
		summaryTr,
		model,
		translatedAt,
	)
	return err
}
