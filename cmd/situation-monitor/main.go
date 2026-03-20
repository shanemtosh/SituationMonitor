package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"situationmonitor/internal/config"
	"situationmonitor/internal/db"
	"situationmonitor/internal/httpserver"
	"situationmonitor/internal/ingest/rss"
	"situationmonitor/internal/ingest/sweep"
	"situationmonitor/internal/market"
	"situationmonitor/internal/translate"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	if err := os.MkdirAll(filepath.Dir(cfg.DatabasePath), 0o755); err != nil && filepath.Dir(cfg.DatabasePath) != "." {
		log.Fatalf("data dir: %v", err)
	}

	sqlDB, err := db.Open(cfg.DatabasePath)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer sqlDB.Close()

	ctx, stopWorkers := context.WithCancel(context.Background())
	defer stopWorkers()

	if cfg.RSSPollInterval > 0 {
		go rss.RunLoop(ctx, rss.Config{
			FeedsFile:     cfg.RSSFeedsFile,
			PollInterval:  cfg.RSSPollInterval,
			FetchTimeout:  cfg.RSSFetchTimeout,
			UserAgent:     cfg.RSSUserAgent,
			IngestOnStart: cfg.RSSIngestOnStart,
		}, sqlDB)
	}

	if cfg.SweepPoll > 0 && cfg.OpenRouterAPIKey != "" {
		go sweep.RunLoop(ctx, sqlDB, sweep.LoopConfig{
			APIKey:                cfg.OpenRouterAPIKey,
			BaseURL:               cfg.OpenRouterBaseURL,
			Model:                 cfg.OpenRouterModel,
			BriefPath:             cfg.SweepBriefPath,
			PollInterval:          cfg.SweepPoll,
			HTTPTimeout:           cfg.OpenRouterHTTPTimeout,
			IngestOnStart:         cfg.SweepOnStart,
			UseJSONResponseFormat: cfg.OpenRouterJSON,
			NtfyServer:            cfg.NtfyServer,
			NtfyTopic:             cfg.NtfyTopic,
			NtfyToken:             cfg.NtfyToken,
			AlertMinUrgency:       cfg.AlertMinUrgency,
			AlertMaxPerHour:       cfg.AlertMaxPerHour,
		})
	} else if cfg.SweepPoll > 0 && cfg.OpenRouterAPIKey == "" {
		log.Printf("sweep: disabled (set OPENROUTER_API_KEY)")
	}

	if cfg.TranslatePoll > 0 && cfg.OllamaTranslate != "" {
		go translate.RunLoop(ctx, sqlDB, translate.LoopConfig{
			OllamaBaseURL: cfg.OllamaBaseURL,
			Model:         cfg.OllamaTranslate,
			TargetLang:    cfg.TranslateTarget,
			PollInterval:  cfg.TranslatePoll,
			BatchSize:     cfg.TranslateBatch,
			OnStart:       cfg.TranslateOnStart,
		})
	}

	if cfg.MarketPoll > 0 && len(cfg.MarketSymbols) > 0 {
		go market.RunLoop(ctx, sqlDB, market.LoopConfig{
			Symbols:      cfg.MarketSymbols,
			PollInterval: cfg.MarketPoll,
			FetchTimeout: cfg.MarketFetchTO,
			OnStart:      cfg.MarketOnStart,
		})
	}

	pagesDir := cfg.PagesDir
	if err := os.MkdirAll(pagesDir, 0o755); err != nil {
		log.Fatalf("pages dir: %v", err)
	}

	mux := http.NewServeMux()
	httpserver.Mount(mux, sqlDB, pagesDir)

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("listening on http://%s", cfg.ListenAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	stopWorkers()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown: %v", err)
	}
}
