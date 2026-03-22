package translate

import (
	"context"
	"database/sql"
	"log"
	"strings"
	"time"

	"situationmonitor/internal/ollama"
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

// nonEnglishFeeds lists feed URL substrings for feeds that publish in non-English.
// Everything else is assumed English and skipped by the translator.
var nonEnglishFeeds = []string{
	"www3.nhk.or.jp",      // Japanese
	"www.ansa.it",         // Italian
	"www.repubblica.it",   // Italian
	"feeds.bbci.co.uk/mundo", // Spanish (BBC Mundo)
	"www.lanacion.com.ar", // Spanish (Argentina)
	"www.infobae.com",     // Spanish (Argentina/LatAm)
	"www.eltiempo.com",    // Spanish (Colombia)
	"www.eluniversal.com.mx", // Spanish (Mexico)
	"www.latercera.com",   // Spanish (Chile)
	"efectococuyo.com",    // Spanish (Venezuela)
	"agenciabrasil.ebc.com.br/rss", // Portuguese (Brazil) — not the /en/ version
}

func isEnglishFeed(feedURL string) bool {
	lower := strings.ToLower(feedURL)
	for _, f := range nonEnglishFeeds {
		if strings.Contains(lower, f) {
			return false
		}
	}
	return true
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// RunLoop processes untranslated items until ctx is cancelled.
func RunLoop(ctx context.Context, db *sql.DB, cfg LoopConfig) {
	if cfg.PollInterval <= 0 || strings.TrimSpace(cfg.Model) == "" {
		return
	}
	target := strings.TrimSpace(cfg.TargetLang)
	if target == "" {
		target = "English"
	}
	batch := cfg.BatchSize
	if batch <= 0 {
		batch = 15
	}

	contentBatch := cfg.ContentBatchSize
	if contentBatch <= 0 {
		contentBatch = 3
	}

	run := func() {
		ctxRun, cancel := context.WithTimeout(ctx, 8*time.Minute)
		defer cancel()

		// Pass 1: translate titles and summaries
		rows, err := store.ListUntranslated(ctxRun, db, batch)
		if err != nil {
			log.Printf("translate: list: %v", err)
			return
		}
		for i, r := range rows {
			// Skip known English-language feeds — mark as English directly
			if isEnglishFeed(r.FeedURL) {
				if err := store.SetTranslation(ctxRun, db, r.ID, "en", r.Title, r.Summary, "skip"); err != nil {
					log.Printf("translate: save %d: %v", r.ID, err)
				}
				continue
			}
			lang, tit, sum, err := ollama.TranslateToTarget(ctxRun, cfg.OllamaBaseURL, cfg.Model, target, r.Title, r.Summary)
			if err != nil {
				log.Printf("translate: item %d (%d/%d): %v", r.ID, i+1, len(rows), err)
				continue
			}
			log.Printf("translate: item %d (%d/%d): %s → %s", r.ID, i+1, len(rows), lang, truncate(tit, 60))
			if tit == "" {
				tit = r.Title
			}
			if sum == "" {
				sum = r.Summary
			}
			if lang == "" {
				lang = "und"
			}
			if err := store.SetTranslation(ctxRun, db, r.ID, lang, tit, sum, cfg.Model); err != nil {
				log.Printf("translate: save %d: %v", r.ID, err)
			}
		}
		if len(rows) > 0 {
			log.Printf("translate: processed %d items", len(rows))
		}

		// Pass 2: fetch and translate full article content for non-English items
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
