package treasury

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

	"situationmonitor/internal/htmltext"
	"situationmonitor/internal/store"
)

const (
	listingURL = "https://home.treasury.gov/news/press-releases"
	feedURL    = "treasury://home.treasury.gov/news/press-releases"
)

// Config drives the Treasury scraper.
type Config struct {
	PollInterval  time.Duration
	FetchTimeout  time.Duration
	IngestOnStart bool
}

// RunLoop scrapes Treasury press releases until ctx is cancelled.
func RunLoop(ctx context.Context, cfg Config, db *sql.DB) {
	if cfg.PollInterval <= 0 {
		return
	}
	client := &http.Client{Timeout: cfg.FetchTimeout}

	run := func() {
		n, err := ingest(ctx, db, client)
		if err != nil {
			log.Printf("treasury: ingest error after %d items: %v", n, err)
			return
		}
		log.Printf("treasury: ingested %d press releases", n)
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

// ingest fetches the listing page, extracts release links, fetches each
// release page, and upserts them as items.
func ingest(ctx context.Context, db *sql.DB, client *http.Client) (int, error) {
	releases, err := fetchListing(ctx, client)
	if err != nil {
		return 0, fmt.Errorf("listing: %w", err)
	}

	ctxUpsert, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	var n int
	for _, rel := range releases {
		summary, err := fetchReleaseSummary(ctx, client, rel.URL)
		if err != nil {
			log.Printf("treasury: fetch %s: %v", rel.Slug, err)
			continue
		}

		h := sha256.Sum256([]byte(rel.URL))
		extID := "u:" + hex.EncodeToString(h[:])

		a := store.RSSArticle{
			ExternalID: extID,
			Title:      rel.Title,
			Summary:    summary,
			URL:        rel.URL,
			FeedURL:    feedURL,
			Published:  rel.Date,
		}
		if err := store.UpsertRSS(ctxUpsert, db, a); err != nil {
			return n, fmt.Errorf("upsert: %w", err)
		}
		n++
	}
	return n, nil
}

type release struct {
	Title string
	URL   string
	Slug  string
	Date  time.Time
}

// fetchListing scrapes the Treasury press releases listing page.
func fetchListing(ctx context.Context, client *http.Client) ([]release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, listingURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "SituationMonitor/1.0 (+local; Treasury)")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %s", resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}

	return parseListing(string(body))
}

// parseListing extracts release links from the listing HTML.
// Treasury uses <h3><a href="/news/press-releases/SLUG">Title</a></h3>
// with dates in preceding <p> or <time> elements.
func parseListing(html string) ([]release, error) {
	var releases []release

	// Find all <h3> blocks with links to press releases
	remaining := html
	for {
		// Look for links to /news/press-releases/
		idx := strings.Index(remaining, `href="/news/press-releases/`)
		if idx == -1 {
			break
		}
		remaining = remaining[idx:]

		// Extract href value
		hrefEnd := strings.Index(remaining[6:], `"`)
		if hrefEnd == -1 {
			remaining = remaining[6:]
			continue
		}
		path := remaining[6 : 6+hrefEnd]
		remaining = remaining[6+hrefEnd:]

		// Skip pagination, listing links, and category navigation pages.
		slug := strings.TrimPrefix(path, "/news/press-releases/")
		if slug == "" || strings.HasPrefix(slug, "?") || strings.Contains(slug, "/") {
			continue
		}
		// Actual release slugs are like "sb0420", "jl1234". Skip navigation
		// pages like "readouts", "testimonies", "statements-remarks".
		if !strings.ContainsAny(slug[:1], "abcdefghijklmnopqrstuvwxyz") || len(slug) > 30 {
			continue
		}
		// Navigation slugs are plain words; release slugs have digits
		hasDigit := false
		for _, c := range slug {
			if c >= '0' && c <= '9' {
				hasDigit = true
				break
			}
		}
		if !hasDigit {
			continue
		}

		// Extract title text (content between > and </a>)
		titleStart := strings.Index(remaining, ">")
		if titleStart == -1 {
			continue
		}
		titleEnd := strings.Index(remaining[titleStart:], "</a>")
		if titleEnd == -1 {
			continue
		}
		title := htmltext.Strip(remaining[titleStart+1 : titleStart+titleEnd])
		remaining = remaining[titleStart+titleEnd:]

		if title == "" {
			continue
		}

		url := "https://home.treasury.gov" + path

		// Try to find a date near this entry by looking backward in the original HTML
		// We'll parse dates from the release page instead for accuracy
		releases = append(releases, release{
			Title: title,
			URL:   url,
			Slug:  slug,
		})
	}

	return releases, nil
}

// fetchReleaseSummary fetches an individual release page and extracts
// the first few paragraphs as a summary.
func fetchReleaseSummary(ctx context.Context, client *http.Client, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "SituationMonitor/1.0 (+local; Treasury)")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("HTTP %s", resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return "", err
	}

	return extractSummary(string(body)), nil
}

// extractSummary pulls text from the release page body.
// Treasury uses Drupal; the article body lives in a div with
// class "field--name-field-news-body".
func extractSummary(page string) string {
	// Find the news body field — this contains the actual release text
	marker := `field--name-field-news-body`
	idx := strings.Index(page, marker)
	if idx == -1 {
		return ""
	}
	content := page[idx:]

	// Find the opening > of that div
	divStart := strings.Index(content, ">")
	if divStart == -1 {
		return ""
	}
	content = content[divStart+1:]

	// Extract text from <p> and <h3> tags within the body
	var paragraphs []string
	remaining := content
	for len(paragraphs) < 6 {
		pStart := strings.Index(remaining, "<p")
		hStart := strings.Index(remaining, "<h3")

		// Pick whichever comes first
		start := pStart
		endTag := "</p>"
		if hStart >= 0 && (pStart < 0 || hStart < pStart) {
			start = hStart
			endTag = "</h3>"
		}
		if start < 0 {
			break
		}
		remaining = remaining[start:]

		// Skip to end of opening tag
		tagEnd := strings.Index(remaining, ">")
		if tagEnd == -1 {
			break
		}

		closeIdx := strings.Index(remaining[tagEnd+1:], endTag)
		if closeIdx < 0 {
			break
		}

		text := htmltext.Strip(remaining[tagEnd+1 : tagEnd+1+closeIdx])
		remaining = remaining[tagEnd+1+closeIdx+len(endTag):]

		// Skip short fragments (dates, "###", nav links)
		if len(text) < 40 || strings.HasPrefix(text, "###") ||
			strings.HasPrefix(text, "Click here") {
			continue
		}

		paragraphs = append(paragraphs, text)
	}

	summary := strings.Join(paragraphs, "\n\n")
	const max = 12000
	if len(summary) > max {
		return summary[:max] + "…"
	}
	return summary
}
