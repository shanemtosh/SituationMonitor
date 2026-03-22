package main

import (
	"bufio"
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"situationmonitor/internal/config"
	"situationmonitor/internal/db"
	"situationmonitor/internal/httpserver"
	"situationmonitor/internal/ingest/rss"
	"situationmonitor/internal/ingest/sweep"
	"situationmonitor/internal/market"
	"situationmonitor/internal/ollama"
	"situationmonitor/internal/extract"
	"situationmonitor/internal/translate"
)

func loadDotenv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		// Don't override env vars already set externally
		if os.Getenv(k) == "" {
			os.Setenv(k, v)
		}
	}
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	loadDotenv(".env")

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

	var ollamaMgr *ollama.Manager
	if cfg.TranslatePoll > 0 && cfg.OllamaTranslate != "" {
		ollamaMgr = &ollama.Manager{
			BaseURL: cfg.OllamaBaseURL,
			Model:   cfg.OllamaTranslate,
		}
		if err := ollamaMgr.Start(ctx); err != nil {
			log.Printf("ollama: %v — translation disabled", err)
			ollamaMgr = nil
		} else {
			go translate.RunLoop(ctx, sqlDB, translate.LoopConfig{
				OllamaBaseURL:     cfg.OllamaBaseURL,
				Model:             cfg.OllamaTranslate,
				TargetLang:        cfg.TranslateTarget,
				PollInterval:      cfg.TranslatePoll,
				BatchSize:         cfg.TranslateBatch,
				OnStart:           cfg.TranslateOnStart,
				PaywallFetcherURL: cfg.PaywallFetcherURL,
			})
		}
	}

	// NER / entity extraction / situation tracking worker
	nerModel := cfg.NERModel
	if nerModel == "" {
		nerModel = cfg.OllamaTranslate // fall back to translate model
	}
	if cfg.NERPoll > 0 && nerModel != "" && ollamaMgr != nil {
		go extract.RunLoop(ctx, sqlDB, extract.LoopConfig{
			OllamaBaseURL:     cfg.OllamaBaseURL,
			Model:             nerModel,
			PollInterval:      cfg.NERPoll,
			BatchSize:         cfg.NERBatch,
			OnStart:           cfg.NEROnStart,
			MinClusterOverlap: 2,
			SituationMinItems: cfg.SituationMinItems,
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
	httpserver.Mount(mux, sqlDB, pagesDir, httpserver.ReaderConfig{
		OllamaBaseURL:     cfg.OllamaBaseURL,
		OllamaModel:       cfg.OllamaTranslate,
		TargetLang:        cfg.TranslateTarget,
		PaywallFetcherURL: cfg.PaywallFetcherURL,
		OpenRouterAPIKey:  cfg.OpenRouterAPIKey,
		OpenRouterBaseURL: cfg.OpenRouterBaseURL,
		BriefModel:        cfg.BriefModel,
	})

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

	if ollamaMgr != nil {
		ollamaMgr.Stop()
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown: %v", err)
	}
}
