package alerts

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"situationmonitor/internal/store"
)

// DispatchSweepAlerts sends ntfy notifications for high-urgency sweep items (rate-limited).
func DispatchSweepAlerts(ctx context.Context, db *sql.DB, p NtfyParams) error {
	if p.Topic == "" || p.Server == "" {
		return nil
	}
	candidates, err := store.ListSweepItemsNeedingAlert(ctx, db, p.SweepID, p.MinUrgency)
	if err != nil {
		return err
	}
	if len(candidates) == 0 {
		return nil
	}

	client := p.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}

	since := time.Now().Add(-1 * time.Hour)
	sentLastHour, err := store.AlertCountSince(ctx, db, "ntfy", since)
	if err != nil {
		return err
	}
	remaining := p.MaxPerHour - sentLastHour
	if remaining <= 0 {
		return fmt.Errorf("alerts: rate limit reached (%d/hour)", p.MaxPerHour)
	}

	endpoint := strings.TrimRight(p.Server, "/") + "/" + url.PathEscape(p.Topic)
	for _, it := range candidates {
		if remaining <= 0 {
			break
		}
		title := fmt.Sprintf("Urgency %d — %s", it.Urgency, truncate(it.Title, 120))
		body := truncate(it.Summary, 1500)
		if it.URL != "" {
			body += "\n\n" + it.URL
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Title", title)
		req.Header.Set("Content-Type", "text/plain; charset=utf-8")
		if p.Token != "" {
			req.Header.Set("Authorization", "Bearer "+p.Token)
		}
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		func() {
			defer resp.Body.Close()
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		}()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("ntfy HTTP %s", resp.Status)
		}
		digest := truncate(it.Title+"\n"+it.URL, 200)
		if err := store.InsertAlertLog(ctx, db, "ntfy", it.ID, digest); err != nil {
			return err
		}
		if err := store.MarkItemAlerted(ctx, db, it.ID); err != nil {
			return err
		}
		remaining--
	}
	return nil
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// NtfyParams configures sweep alerting.
type NtfyParams struct {
	HTTPClient *http.Client
	Server     string
	Topic      string
	Token      string
	SweepID    int64
	MinUrgency int
	MaxPerHour int
}
