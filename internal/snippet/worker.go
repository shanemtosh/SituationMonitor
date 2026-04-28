// Package snippet runs a background worker that maintains a 1-2 sentence
// rolling Ollama-generated summary on each active situation, surfaced on the
// /situations list page.
package snippet

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"

	"situationmonitor/internal/ollama"
	"situationmonitor/internal/store"
)

// LoopConfig drives the snippet worker.
type LoopConfig struct {
	OllamaBaseURL string
	Model         string
	Interval      time.Duration // tick between full passes
	TopN          int           // max situations refreshed per pass
	PerCallSleep  time.Duration // gap between Ollama calls inside a pass
}

// RunLoop ticks every Interval and refreshes snippets for the most active
// situations whose snippet is older than their newest item.
func RunLoop(ctx context.Context, db *sql.DB, cfg LoopConfig) {
	if cfg.Interval <= 0 || strings.TrimSpace(cfg.Model) == "" {
		return
	}
	if cfg.TopN <= 0 {
		cfg.TopN = 30
	}
	if cfg.PerCallSleep <= 0 {
		cfg.PerCallSleep = 60 * time.Second
	}

	tick := func() {
		runOnce(ctx, db, cfg)
	}

	tick()
	t := time.NewTicker(cfg.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			tick()
		}
	}
}

func runOnce(ctx context.Context, db *sql.DB, cfg LoopConfig) {
	start := time.Now()
	sits, err := store.ListSituationsByActivity(ctx, db, "active", cfg.TopN)
	if err != nil {
		log.Printf("snippet: list: %v", err)
		return
	}

	refreshed := 0
	skipped := 0
	for _, s := range sits {
		if ctx.Err() != nil {
			return
		}
		if isFresh(s) {
			skipped++
			continue
		}
		items, err := store.ListSituationItems(ctx, db, s.ID, 10)
		if err != nil {
			log.Printf("snippet: items %d: %v", s.ID, err)
			continue
		}
		if len(items) == 0 {
			skipped++
			continue
		}

		snippet, err := ollama.GenerateSituationSnippet(ctx, cfg.OllamaBaseURL, cfg.Model, s.Name, toSnippetItems(items))
		if err != nil {
			log.Printf("snippet: generate %d %q: %v", s.ID, s.Name, err)
			// Pace even on failure so we don't hammer a sick Ollama.
			sleep(ctx, cfg.PerCallSleep)
			continue
		}
		if err := store.SetSituationSnippet(ctx, db, s.ID, snippet); err != nil {
			log.Printf("snippet: write %d: %v", s.ID, err)
			continue
		}
		refreshed++
		sleep(ctx, cfg.PerCallSleep)
	}

	log.Printf("snippet: refreshed %d/%d situations in %s (skipped %d as fresh)",
		refreshed, len(sits), time.Since(start).Round(time.Second), skipped)
}

// isFresh returns true when the existing snippet is at least as new as the
// most recent item linked to the situation. Empty snippets are never fresh.
func isFresh(s store.SituationRow) bool {
	if s.Snippet == "" || s.SnippetGeneratedAt == "" {
		return false
	}
	gen, err := time.Parse(time.RFC3339, s.SnippetGeneratedAt)
	if err != nil {
		return false
	}
	last, err := time.Parse(time.RFC3339, s.LastItemAt)
	if err != nil {
		// No reliable last-item timestamp — treat as fresh to avoid pointless
		// regeneration of a situation with no recorded activity.
		return true
	}
	return !gen.Before(last)
}

func toSnippetItems(items []store.ListedItem) []ollama.SnippetItem {
	out := make([]ollama.SnippetItem, 0, len(items))
	for _, it := range items {
		title := it.TitleTrans
		if title == "" {
			title = it.Title
		}
		summary := it.SummaryTrans
		if summary == "" {
			summary = it.Summary
		}
		out = append(out, ollama.SnippetItem{
			Title:   title,
			Summary: summary,
			Source:  feedHost(it.FeedURL),
			Age:     ageOf(it.CreatedAt),
		})
	}
	return out
}

func feedHost(feedURL string) string {
	if feedURL == "" {
		return ""
	}
	u, err := url.Parse(feedURL)
	if err != nil || u.Host == "" {
		return ""
	}
	host := strings.TrimPrefix(u.Host, "www.")
	host = strings.TrimPrefix(host, "feeds.")
	if k := strings.LastIndex(host, "."); k > 0 {
		host = host[:k]
	}
	return host
}

func ageOf(rfc3339 string) string {
	t, err := time.Parse(time.RFC3339, rfc3339)
	if err != nil {
		return "unknown"
	}
	d := time.Since(t)
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func sleep(ctx context.Context, d time.Duration) {
	if d <= 0 {
		return
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}
