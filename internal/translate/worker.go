package translate

import (
	"context"
	"database/sql"
	"log"
	"strings"
	"time"

	"situationmonitor/internal/reader"
	"situationmonitor/internal/store"
)

// LoopConfig drives the translation worker.
type LoopConfig struct {
	OllamaBaseURL     string
	Model             string
	TargetLang        string
	PollInterval      time.Duration
	BatchSize         int
	OnStart           bool
	PaywallFetcherURL string
	ContentBatchSize  int // max articles to fetch+translate per tick (default 3)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// RunLoop fetches and translates full article content for non-English items.
// Title/summary translation is now handled inline during RSS ingestion.
func RunLoop(ctx context.Context, db *sql.DB, cfg LoopConfig) {
	if cfg.PollInterval <= 0 || strings.TrimSpace(cfg.Model) == "" {
		return
	}
	target := strings.TrimSpace(cfg.TargetLang)
	if target == "" {
		target = "English"
	}

	contentBatch := cfg.ContentBatchSize
	if contentBatch <= 0 {
		contentBatch = 3
	}

	run := func() {
		ctxRun, cancel := context.WithTimeout(ctx, 8*time.Minute)
		defer cancel()
		translateContent(ctxRun, db, cfg, target, contentBatch)
	}

	if cfg.OnStart {
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

// translateContent fetches and translates full article content for non-English items
// that already have their title/summary translated but no content yet.
func translateContent(ctx context.Context, db *sql.DB, cfg LoopConfig, target string, batch int) {
	rows, err := store.ListContentUntranslated(ctx, db, batch)
	if err != nil {
		log.Printf("translate-content: list: %v", err)
		return
	}
	if len(rows) == 0 {
		return
	}

	fetchCfg := reader.FetchConfig{PaywallFetcherURL: cfg.PaywallFetcherURL}

	for i, r := range rows {
		if ctx.Err() != nil {
			return
		}

		// Fetch article content
		article, err := reader.Fetch(ctx, r.URL, fetchCfg)
		if err != nil {
			log.Printf("translate-content: fetch %d (%d/%d) %s: %v", r.ID, i+1, len(rows), truncate(r.URL, 60), err)
			// Store empty content so we don't retry endlessly
			_ = store.SetContent(ctx, db, r.ID, "", "")
			continue
		}

		if strings.TrimSpace(article.Content) == "" {
			log.Printf("translate-content: empty content %d (%d/%d)", r.ID, i+1, len(rows))
			_ = store.SetContent(ctx, db, r.ID, "", "")
			continue
		}

		// Translate the content
		var translated string
		if cfg.Model != "" {
			translated, err = reader.TranslateContent(ctx, cfg.OllamaBaseURL, cfg.Model, target, article.Content)
			if err != nil {
				log.Printf("translate-content: translate %d (%d/%d): %v", r.ID, i+1, len(rows), err)
				// Still cache the raw content so it's not re-fetched
				_ = store.SetContent(ctx, db, r.ID, article.Content, "")
				continue
			}
		}

		if err := store.SetContent(ctx, db, r.ID, article.Content, translated); err != nil {
			log.Printf("translate-content: save %d: %v", r.ID, err)
			continue
		}
		log.Printf("translate-content: %d (%d/%d): fetched+translated %s", r.ID, i+1, len(rows), truncate(r.URL, 60))
	}
	log.Printf("translate-content: processed %d items", len(rows))
}
