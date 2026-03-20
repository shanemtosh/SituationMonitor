package sweep

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"strings"
	"time"

	"situationmonitor/internal/alerts"
	"situationmonitor/internal/brief"
	"situationmonitor/internal/openrouter"
)

// LoopConfig configures scheduled situation sweeps.
type LoopConfig struct {
	APIKey                string
	BaseURL               string
	Model                 string
	BriefPath             string
	PollInterval          time.Duration
	HTTPTimeout           time.Duration
	IngestOnStart         bool
	UseJSONResponseFormat bool
	NtfyServer            string
	NtfyTopic             string
	NtfyToken             string
	AlertMinUrgency       int
	AlertMaxPerHour       int
}

// RunLoop executes OpenRouter sweeps until ctx is cancelled.
func RunLoop(ctx context.Context, db *sql.DB, cfg LoopConfig) {
	if cfg.PollInterval <= 0 || cfg.APIKey == "" {
		return
	}

	client := &openrouter.Client{
		APIKey:  cfg.APIKey,
		BaseURL: strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"),
		HTTPClient: &http.Client{
			Timeout: cfg.HTTPTimeout,
		},
	}
	if client.HTTPClient.Timeout <= 0 {
		client.HTTPClient.Timeout = 2 * time.Minute
	}

	run := func() {
		text, err := brief.Load(cfg.BriefPath)
		if err != nil {
			log.Printf("sweep: brief %q: %v", cfg.BriefPath, err)
			return
		}
		ctxRun, cancel := context.WithTimeout(ctx, 8*time.Minute)
		defer cancel()

		sid, err := openrouter.RunSweep(ctxRun, db, client, openrouter.SweepArgs{
			Model:                 cfg.Model,
			Brief:                 text,
			HTTPTimeout:           cfg.HTTPTimeout,
			UseJSONResponseFormat: cfg.UseJSONResponseFormat,
		})
		if err != nil {
			log.Printf("sweep: failed (sweep_id=%d): %v", sid, err)
			return
		}
		log.Printf("sweep: completed sweep_id=%d", sid)

		minU := cfg.AlertMinUrgency
		if minU <= 0 {
			minU = 4
		}
		maxH := cfg.AlertMaxPerHour
		if maxH <= 0 {
			maxH = 12
		}
		if err := alerts.DispatchSweepAlerts(ctxRun, db, alerts.NtfyParams{
			HTTPClient: client.HTTPClient,
			Server:     cfg.NtfyServer,
			Topic:      cfg.NtfyTopic,
			Token:      cfg.NtfyToken,
			SweepID:    sid,
			MinUrgency: minU,
			MaxPerHour: maxH,
		}); err != nil {
			log.Printf("sweep: alerts: %v", err)
		}
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
