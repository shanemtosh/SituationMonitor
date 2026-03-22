package store

import (
	"context"
	"database/sql"
	"time"
)

// ReaderItem holds everything needed for the reader view.
type ReaderItem struct {
	ID                int64
	Title             string
	TitleTranslated   string
	Summary           string
	SummaryTranslated string
	URL               string
	FeedURL           string
	Lang              string
	SourceKind        string
	CreatedAt         string
	ContentText       string
	ContentTranslated string
	ContentFetchedAt  string
	BriefText         string
	BriefAt           string
}

// GetReaderItem fetches a single item by ID for the reader view.
func GetReaderItem(ctx context.Context, db *sql.DB, id int64) (ReaderItem, error) {
	var r ReaderItem
	err := db.QueryRowContext(ctx, `
SELECT id, title, COALESCE(title_translated,''), COALESCE(summary,''),
       COALESCE(summary_translated,''), COALESCE(url,''), COALESCE(feed_url,''),
       COALESCE(lang,''), source_kind, created_at,
       COALESCE(content_text,''), COALESCE(content_translated,''),
       COALESCE(content_fetched_at,''),
       COALESCE(brief_text,''), COALESCE(brief_at,'')
FROM items WHERE id = ?
`, id).Scan(&r.ID, &r.Title, &r.TitleTranslated, &r.Summary,
		&r.SummaryTranslated, &r.URL, &r.FeedURL,
		&r.Lang, &r.SourceKind, &r.CreatedAt,
		&r.ContentText, &r.ContentTranslated, &r.ContentFetchedAt,
		&r.BriefText, &r.BriefAt)
	return r, err
}

// SetBrief caches an AI-generated brief for an item.
func SetBrief(ctx context.Context, db *sql.DB, id int64, text string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.ExecContext(ctx, `
UPDATE items SET brief_text = ?, brief_at = ? WHERE id = ?
`, text, now, id)
	return err
}

// SetContent caches extracted article content for an item.
func SetContent(ctx context.Context, db *sql.DB, id int64, text, translated string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.ExecContext(ctx, `
UPDATE items SET content_text = ?, content_translated = ?, content_fetched_at = ?
WHERE id = ?
`, text, translated, now, id)
	return err
}
