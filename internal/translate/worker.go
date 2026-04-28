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

// RunLoop pre-fetches full article content for all items and translates non-English ones.
// Title/summary translation is handled inline during RSS ingestion.
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
		contentBatch = 5
	}

	run := func() {
		ctxRun, cancel := context.WithTimeout(ctx, 8*time.Minute)
		defer cancel()
		prefetchContent(ctxRun, db, cfg, target, contentBatch)
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

// prefetchContent fetches full article content for ALL items that don't have it yet.
// Non-English items also get their content translated via Ollama.
func prefetchContent(ctx context.Context, db *sql.DB, cfg LoopConfig, target string, batch int) {
	rows, err := store.ListContentUnfetched(ctx, db, batch)
	if err != nil {
		log.Printf("prefetch: list: %v", err)
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

		article, err := reader.Fetch(ctx, r.URL, fetchCfg)
		if err != nil {
			log.Printf("prefetch: fetch %d (%d/%d) %s: %v", r.ID, i+1, len(rows), truncate(r.URL, 60), err)
			// Mark as attempted so we don't retry endlessly
			_ = store.SetContent(ctx, db, r.ID, "", "")
			continue
		}

		if strings.TrimSpace(article.Content) == "" {
			log.Printf("prefetch: empty content %d (%d/%d)", r.ID, i+1, len(rows))
			_ = store.SetContent(ctx, db, r.ID, "", "")
			continue
		}

		// Translate non-English content
		var translated string
		needsTranslation := r.Lang != "" && r.Lang != "en" && r.Lang != "und" && r.Lang != "skip"
		if needsTranslation && cfg.Model != "" {
			translated, err = reader.TranslateContent(ctx, cfg.OllamaBaseURL, cfg.Model, target, article.Content)
			if err != nil {
				log.Printf("prefetch: translate %d (%d/%d): %v", r.ID, i+1, len(rows), err)
				// Cache raw content even if translation fails
				_ = store.SetContent(ctx, db, r.ID, article.Content, "")
				continue
			}
		}

		if err := store.SetContent(ctx, db, r.ID, article.Content, translated); err != nil {
			log.Printf("prefetch: save %d: %v", r.ID, err)
			continue
		}
		if translated != "" {
			log.Printf("prefetch: %d (%d/%d): fetched+translated %s", r.ID, i+1, len(rows), truncate(r.URL, 60))
		} else {
			log.Printf("prefetch: %d (%d/%d): fetched %s", r.ID, i+1, len(rows), truncate(r.URL, 60))
		}
	}
	log.Printf("prefetch: processed %d items", len(rows))
}

