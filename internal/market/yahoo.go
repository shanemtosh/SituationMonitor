package market

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"situationmonitor/internal/store"
)

// FetchYahooQuotes returns snapshot quotes for the given symbols (comma-free slice).
func FetchYahooQuotes(ctx context.Context, client *http.Client, symbols []string) ([]store.Quote, error) {
	if len(symbols) == 0 {
		return nil, nil
	}
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	q := url.Values{}
	q.Set("symbols", strings.Join(symbols, ","))
	u := "https://query1.finance.yahoo.com/v7/finance/quote?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; SituationMonitor/1.0)")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("yahoo HTTP %s: %s", resp.Status, string(body))
	}

	var parsed struct {
		QuoteResponse struct {
			Result []struct {
				Symbol                    string  `json:"symbol"`
				ShortName                 string  `json:"shortName"`
				LongName                  string  `json:"longName"`
				RegularMarketPrice        float64 `json:"regularMarketPrice"`
				RegularMarketChangePercent float64 `json:"regularMarketChangePercent"`
				Currency                  string  `json:"currency"`
			} `json:"result"`
		} `json:"quoteResponse"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	var out []store.Quote
	for _, r := range parsed.QuoteResponse.Result {
		name := strings.TrimSpace(r.ShortName)
		if name == "" {
			name = strings.TrimSpace(r.LongName)
		}
		out = append(out, store.Quote{
			Symbol:    strings.TrimSpace(r.Symbol),
			Name:      name,
			Price:     r.RegularMarketPrice,
			ChangePct: r.RegularMarketChangePercent,
			Currency:  strings.TrimSpace(r.Currency),
			FetchedAt: now,
		})
	}
	return out, nil
}
