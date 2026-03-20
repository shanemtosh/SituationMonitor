package rss

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/mmcdole/gofeed"

	"situationmonitor/internal/feeds"
	"situationmonitor/internal/store"
)

// RunLoop periodically fetches all feeds listed in RSS_FEEDS_FILE until ctx is cancelled.
func RunLoop(ctx context.Context, cfg Config, db *sql.DB) {
	if cfg.PollInterval <= 0 {
		return
	}

	client := &http.Client{Timeout: cfg.FetchTimeout}
	parser := gofeed.NewParser()
	parser.UserAgent = cfg.UserAgent
	parser.Client = client

	run := func() {
		urls, err := feeds.LoadURLs(cfg.FeedsFile)
		if err != nil {
			log.Printf("rss: could not read feeds file %q: %v", cfg.FeedsFile, err)
			return
		}
		if len(urls) == 0 {
			log.Printf("rss: %q contains no URLs", cfg.FeedsFile)
			return
		}
		n, err := IngestAll(ctx, db, client, parser, urls, cfg.UserAgent)
		if err != nil {
			log.Printf("rss: ingest finished with errors after %d upserts: %v", n, err)
			return
		}
		log.Printf("rss: ingested %d items from %d feeds", n, len(urls))
	}

	if cfg.IngestOnStart {
		run()
	}

	t := time.NewTicker(cfg.PollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			run()
		}
	}
}

// Config is the subset of app config needed for RSS polling (avoids an import cycle on internal/config).
type Config struct {
	FeedsFile      string
	PollInterval   time.Duration
	FetchTimeout   time.Duration
	UserAgent      string
	IngestOnStart  bool
}

// IngestAll fetches each feed URL and upserts items. Returns total successful upserts and the last error, if any.
func IngestAll(ctx context.Context, db *sql.DB, client *http.Client, parser *gofeed.Parser, feedURLs []string, userAgent string) (int, error) {
	var total int
	var lastErr error
	for _, u := range feedURLs {
		n, err := ingestOne(ctx, db, client, parser, strings.TrimSpace(u), userAgent)
		total += n
		if err != nil {
			lastErr = err
			log.Printf("rss: feed %q: %v", u, err)
		}
	}
	return total, lastErr
}

func ingestOne(ctx context.Context, db *sql.DB, client *http.Client, parser *gofeed.Parser, feedURL, userAgent string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feedURL, nil)
	if err != nil {
		return 0, err
	}
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, fmt.Errorf("HTTP %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 20<<20))
	if err != nil {
		return 0, err
	}
	feed, err := parser.ParseString(string(body))
	if err != nil {
		return 0, err
	}

	ctxUpsert, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	var n int
	for _, it := range feed.Items {
		extID := externalID(it)
		if extID == "" {
			continue
		}
		title := strings.TrimSpace(it.Title)
		if title == "" {
			title = "(no title)"
		}
		pub := publishedAt(it)
		a := store.RSSArticle{
			ExternalID: extID,
			Title:      title,
			Summary:    pickSummary(it),
			URL:        canonicalLink(it),
			FeedURL:    feedURL,
			Published:  pub,
		}
		if err := store.UpsertRSS(ctxUpsert, db, a); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func canonicalLink(it *gofeed.Item) string {
	link := strings.TrimSpace(it.Link)
	if link != "" {
		return link
	}
	for _, l := range it.Links {
		h := strings.TrimSpace(l)
		if h != "" {
			return h
		}
	}
	return ""
}

func externalID(it *gofeed.Item) string {
	g := strings.TrimSpace(it.GUID)
	if g != "" {
		return "g:" + g
	}
	link := canonicalLink(it)
	if link != "" {
		sum := sha256.Sum256([]byte(strings.TrimSpace(link)))
		return "u:" + hex.EncodeToString(sum[:])
	}
	raw := strings.TrimSpace(it.Title) + "\n" + strings.TrimSpace(it.Published)
	if raw == "\n" {
		return ""
	}
	sum := sha256.Sum256([]byte(raw))
	return "h:" + hex.EncodeToString(sum[:])
}

func publishedAt(it *gofeed.Item) time.Time {
	if it.PublishedParsed != nil && !it.PublishedParsed.IsZero() {
		return it.PublishedParsed.UTC()
	}
	if it.UpdatedParsed != nil && !it.UpdatedParsed.IsZero() {
		return it.UpdatedParsed.UTC()
	}
	return time.Time{}
}

func pickSummary(it *gofeed.Item) string {
	s := strings.TrimSpace(it.Content)
	if s == "" {
		s = strings.TrimSpace(it.Description)
	}
	const max = 12000
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}
