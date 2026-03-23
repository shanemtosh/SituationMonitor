package reader

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
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
	text = stripNavJunk(text)
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

// navJunkPatterns are substrings that indicate navigation/menu content
// was picked up by the readability parser.
var navJunkPatterns = []string{
	"WorldChinaJapan",
	"SemiconductorsAutomobiles",
	"EquitiesCurrenciesBonds",
	"PoliticsPolitics",
	"EconomyEconomy",
	"BusinessBusiness",
	"TechTech",
	"Life & ArtsLife & Arts",
	"Watch & Listen",
}

// stripNavJunk removes navigation menu text that readability sometimes picks up.
// Works on both multi-line nav lists and single-line concatenated nav text.
func stripNavJunk(text string) string {
	// Check for concatenated nav junk (single-line nav menus like "WorldChinaJapanIndia...")
	for _, p := range navJunkPatterns {
		if idx := strings.Index(text, p); idx >= 0 && idx < 200 {
			// Nav junk at the start — find where real content begins.
			// Look for common article dateline patterns or long sentences with periods.
			cleaned := stripToArticleBody(text)
			if cleaned != "" {
				return cleaned
			}
		}
	}

	// Multi-line nav stripping: detect long runs of short lines
	lines := strings.Split(text, "\n")
	if len(lines) < 5 {
		return text
	}

	articleStart := 0
	consecutiveShort := 0
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if len(line) < 40 && !strings.Contains(line, ".") && line != "" {
			consecutiveShort++
		} else if len(line) >= 60 {
			if consecutiveShort >= 8 {
				articleStart = i
			}
			consecutiveShort = 0
		} else {
			consecutiveShort = 0
		}
	}

	// Strip trailing nav/promo junk
	articleEnd := len(lines)
	trailingShort := 0
	for i := len(lines) - 1; i > articleStart; i-- {
		line := strings.TrimSpace(lines[i])
		if len(line) < 40 && line != "" {
			trailingShort++
		} else if len(line) >= 60 {
			if trailingShort >= 5 {
				articleEnd = i + 1
			}
			break
		}
	}

	if articleStart > 0 || articleEnd < len(lines) {
		return strings.TrimSpace(strings.Join(lines[articleStart:articleEnd], "\n"))
	}
	return text
}

// trailingJunkMarkers indicate where article content ends and promo/nav begins.
var trailingJunkMarkers = []string{
	"Read Next",
	"Latest on ",
	"Sponsored Content",
	"About Sponsored Content",
	"This content was commissioned",
	"Sign up to our newsletters",
	"Subscribe to our newsletter",
	"Related Articles",
	"More from ",
	"Share this article",
}

// stripToArticleBody finds the actual article content after nav junk.
// Looks for dateline patterns (CITY --) or the first long sentence with a period.
func stripToArticleBody(text string) string {
	// Try dateline pattern: "TOKYO -- ", "WASHINGTON -- ", etc.
	datelineRe := regexp.MustCompile(`[A-Z]{3,}[\s]*--[\s]`)
	if loc := datelineRe.FindStringIndex(text); loc != nil {
		text = strings.TrimSpace(text[loc[0]:])
	} else {
		// Find first sentence-like content
		sentenceRe := regexp.MustCompile(`[A-Z][a-z]{2,}[^.]{20,}\.\s`)
		if loc := sentenceRe.FindStringIndex(text); loc != nil && loc[0] > 100 {
			text = strings.TrimSpace(text[loc[0]:])
		} else {
			return ""
		}
	}

	// Strip trailing promo/nav junk
	for _, marker := range trailingJunkMarkers {
		if idx := strings.Index(text, marker); idx > 0 {
			text = strings.TrimSpace(text[:idx])
		}
	}

	return text
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

