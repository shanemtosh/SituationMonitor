package translate

import (
	"context"
	"database/sql"
	"log"
	"strings"
	"time"

	"situationmonitor/internal/ollama"
	"situationmonitor/internal/store"
)

// LoopConfig drives the translation worker.
type LoopConfig struct {
	OllamaBaseURL string
	Model         string
	TargetLang    string
	PollInterval  time.Duration
	BatchSize     int
	OnStart       bool
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

	run := func() {
		ctxRun, cancel := context.WithTimeout(ctx, 8*time.Minute)
		defer cancel()
		rows, err := store.ListUntranslated(ctxRun, db, batch)
		if err != nil {
			log.Printf("translate: list: %v", err)
			return
		}
		for _, r := range rows {
			lang, tit, sum, err := ollama.TranslateToTarget(ctxRun, cfg.OllamaBaseURL, cfg.Model, target, r.Title, r.Summary)
			if err != nil {
				log.Printf("translate: item %d: %v", r.ID, err)
				continue
			}
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
