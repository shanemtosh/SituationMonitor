package market

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"strings"
	"time"

	"situationmonitor/internal/store"
)

// LoopConfig drives periodic Yahoo quote refresh.
type LoopConfig struct {
	Symbols      []string
	PollInterval time.Duration
	FetchTimeout time.Duration
	OnStart      bool
}

// RunLoop refreshes market_quotes until ctx is cancelled.
func RunLoop(ctx context.Context, db *sql.DB, cfg LoopConfig) {
	if cfg.PollInterval <= 0 || len(cfg.Symbols) == 0 {
		return
	}
	client := &http.Client{Timeout: cfg.FetchTimeout}
	if client.Timeout <= 0 {
		client.Timeout = 30 * time.Second
	}

	run := func() {
		quotes, err := FetchYahooQuotes(ctx, client, cfg.Symbols)
		if err != nil {
			log.Printf("market: fetch: %v", err)
			return
		}
		ctxUpsert, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		for _, q := range quotes {
			if err := store.UpsertQuote(ctxUpsert, db, q); err != nil {
				log.Printf("market: upsert %s: %v", q.Symbol, err)
				return
			}
		}
		log.Printf("market: updated %d symbols", len(quotes))
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

// ParseSymbols splits a comma-separated list like "AAPL, MSFT, SPY".
func ParseSymbols(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(strings.ToUpper(p))
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
