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
	"situationmonitor/internal/htmltext"
	"situationmonitor/internal/ollama"
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
		n, err := IngestAll(ctx, db, client, parser, urls, cfg)
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

// Config is the subset of app config needed for RSS polling.
type Config struct {
	FeedsFile     string
	PollInterval  time.Duration
	FetchTimeout  time.Duration
	UserAgent     string
	IngestOnStart bool
	// Inline translation
	OllamaBaseURL  string
	OllamaModel    string
	TranslateTarget string
}

// nonEnglishFeeds lists feed URL substrings for feeds that publish in non-English.
var nonEnglishFeeds = []string{
	"www3.nhk.or.jp",              // Japanese
	"www.ansa.it",                 // Italian
	"www.repubblica.it",           // Italian
	"feeds.bbci.co.uk/mundo",      // Spanish (BBC Mundo)
	"www.lanacion.com.ar",         // Spanish (Argentina)
	"www.infobae.com",             // Spanish (Argentina/LatAm)
	"www.eltiempo.com",            // Spanish (Colombia)
	"www.eluniversal.com.mx",      // Spanish (Mexico)
	"www.latercera.com",           // Spanish (Chile)
	"efectococuyo.com",            // Spanish (Venezuela)
	"agenciabrasil.ebc.com.br/rss", // Portuguese (Brazil)
	"money.udn.com",              // Traditional Chinese (Taiwan)
}

func isNonEnglish(feedURL string) bool {
	lower := strings.ToLower(feedURL)
	for _, f := range nonEnglishFeeds {
		if strings.Contains(lower, f) {
			return true
		}
	}
	return false
}

// IngestAll fetches each feed URL and upserts items. Returns total successful upserts and the last error, if any.
func IngestAll(ctx context.Context, db *sql.DB, client *http.Client, parser *gofeed.Parser, feedURLs []string, cfg Config) (int, error) {
	var total int
	var lastErr error
	for _, u := range feedURLs {
		u = strings.TrimSpace(u)
		// Skip non-HTTP feeds (e.g. treasury:// handled by dedicated scrapers)
		if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
			continue
		}
		n, err := ingestOne(ctx, db, client, parser, u, cfg)
		total += n
		if err != nil {
			lastErr = err
			log.Printf("rss: feed %q: %v", u, err)
		}
	}
	return total, lastErr
}

func ingestOne(ctx context.Context, db *sql.DB, client *http.Client, parser *gofeed.Parser, feedURL string, cfg Config) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feedURL, nil)
	if err != nil {
		return 0, err
	}
	if cfg.UserAgent != "" {
		req.Header.Set("User-Agent", cfg.UserAgent)
	}
	req.Header.Set("Accept", "application/rss+xml, application/xml, text/xml, */*")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
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

	ctxUpsert, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	needsTranslation := isNonEnglish(feedURL)
	target := cfg.TranslateTarget
	if target == "" {
		target = "English"
	}

	var n int
	for _, it := range feed.Items {
		extID := externalID(it)
		if extID == "" {
			continue
		}
		title := htmltext.Strip(it.Title)
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

		// Inline translation for non-English feeds — only for new items
		if needsTranslation && cfg.OllamaModel != "" && !store.ItemExists(ctxUpsert, db, a.ExternalID) {
			lang, tit, sum, err := ollama.TranslateToTarget(ctxUpsert, cfg.OllamaBaseURL, cfg.OllamaModel, target, a.Title, a.Summary)
			if err != nil {
				log.Printf("rss: translate %q: %v", truncate(a.Title, 40), err)
				// Still ingest without translation — will appear untranslated
			} else if strings.EqualFold(strings.TrimSpace(tit), strings.TrimSpace(a.Title)) {
				log.Printf("rss: translate pass-through: %q", truncate(a.Title, 40))
				// Pass-through detected — don't store bad translation
			} else {
				if lang == "" {
					lang = "und"
				}
				if tit == "" {
					tit = a.Title
				}
				if sum == "" {
					sum = a.Summary
				}
				a.Lang = lang
				a.TitleTranslated = tit
				a.SummaryTranslated = sum
				a.TranslatorModel = cfg.OllamaModel
			}
		} else if !needsTranslation && !store.ItemExists(ctxUpsert, db, a.ExternalID) {
			// Mark English feeds as English so they're not picked up by any translate backfill
			a.Lang = "en"
			a.TitleTranslated = a.Title
			a.SummaryTranslated = a.Summary
			a.TranslatorModel = "skip"
		}

		if err := store.UpsertRSS(ctxUpsert, db, a); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
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
	var t time.Time
	if it.PublishedParsed != nil && !it.PublishedParsed.IsZero() {
		t = it.PublishedParsed.UTC()
	} else if it.UpdatedParsed != nil && !it.UpdatedParsed.IsZero() {
		t = it.UpdatedParsed.UTC()
	}
	// Clamp future dates to now (some feeds like Taipei Times publish with tomorrow's date)
	if !t.IsZero() && t.After(time.Now().UTC()) {
		t = time.Now().UTC()
	}
	return t
}

func pickSummary(it *gofeed.Item) string {
	s := strings.TrimSpace(it.Content)
	if s == "" {
		s = strings.TrimSpace(it.Description)
	}
	s = htmltext.Strip(s)
	const max = 12000
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}
