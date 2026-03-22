package reader

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	readability "github.com/go-shiori/go-readability"

	"situationmonitor/internal/htmltext"
)

// Article holds extracted article content.
type Article struct {
	Title   string
	Content string
	Excerpt string
}

// FetchConfig holds settings for article fetching.
type FetchConfig struct {
	PaywallFetcherURL string // e.g. "http://127.0.0.1:3100" — empty disables
}

// Fetch downloads a URL and extracts readable article content.
// Fallback chain: direct fetch → archive/cache bypass → Playwright (if configured).
func Fetch(ctx context.Context, rawURL string, cfg FetchConfig) (Article, error) {
	// 1. Try direct fetch
	article, err := fetchDirect(ctx, rawURL)
	if err == nil {
		return article, nil
	}
	if !isPaywallError(err) {
		return article, err
	}

	// 2. Try archive/cache bypass services
	article, bypassErr := fetchViaBypass(ctx, rawURL)
	if bypassErr == nil && article.Content != "" {
		return article, nil
	}

	// 3. Fall back to Playwright with cookies
	if cfg.PaywallFetcherURL != "" {
		return fetchViaPlaywright(ctx, rawURL, cfg.PaywallFetcherURL)
	}

	return Article{}, err
}

func isPaywallError(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "HTTP 403") ||
		strings.Contains(msg, "HTTP 401") ||
		strings.Contains(msg, "HTTP 451")
}

func fetchDirect(ctx context.Context, rawURL string) (Article, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return Article{}, err
	}

	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return Article{}, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; SituationMonitor/1.0)")

	resp, err := client.Do(req)
	if err != nil {
		return Article{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Article{}, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	article, err := readability.FromReader(resp.Body, u)
	if err != nil {
		return Article{}, err
	}

	text := strings.TrimSpace(article.TextContent)
	if text == "" {
		text = strings.TrimSpace(article.Content)
	}
	text = htmltext.Strip(text)
	if len(text) > 30000 {
		text = text[:30000] + "\n\n[Article truncated]"
	}

	return Article{
		Title:   article.Title,
		Content: text,
		Excerpt: strings.TrimSpace(article.Excerpt),
	}, nil
}

func fetchViaPlaywright(ctx context.Context, rawURL, fetcherURL string) (Article, error) {
	body, err := json.Marshal(map[string]string{"url": rawURL})
	if err != nil {
		return Article{}, err
	}

	client := &http.Client{Timeout: 60 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fetcherURL+"/fetch", bytes.NewReader(body))
	if err != nil {
		return Article{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return Article{}, fmt.Errorf("paywall fetcher: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return Article{}, err
	}
	if resp.StatusCode != http.StatusOK {
		var errResp struct{ Error string }
		_ = json.Unmarshal(raw, &errResp)
		return Article{}, fmt.Errorf("paywall fetcher: %s", errResp.Error)
	}

	var result struct {
		Title   string `json:"title"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return Article{}, err
	}

	text := htmltext.Strip(result.Content)
	if len(text) > 30000 {
		text = text[:30000] + "\n\n[Article truncated]"
	}

	return Article{
		Title:   result.Title,
		Content: text,
	}, nil
}

// fetchViaBypass tries free archive/cache services to get paywalled content.
func fetchViaBypass(ctx context.Context, rawURL string) (Article, error) {
	// Try archive.org Wayback Machine (often has cached versions)
	archiveURL := "https://web.archive.org/web/2/" + rawURL
	article, err := fetchDirect(ctx, archiveURL)
	if err == nil && len(article.Content) > 200 {
		return article, nil
	}

	// Try Google's AMP cache
	u, _ := url.Parse(rawURL)
	if u != nil {
		ampURL := fmt.Sprintf("https://%s.cdn.ampproject.org/c/s/%s%s", strings.ReplaceAll(u.Host, ".", "-"), u.Host, u.Path)
		article, err = fetchDirect(ctx, ampURL)
		if err == nil && len(article.Content) > 200 {
			return article, nil
		}
	}

	// Try 12ft.io
	twelveURL := "https://12ft.io/" + rawURL
	article, err = fetchDirect(ctx, twelveURL)
	if err == nil && len(article.Content) > 200 {
		return article, nil
	}

	return Article{}, fmt.Errorf("all bypass methods failed")
}

