package openrouter

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"situationmonitor/internal/store"
)

const promptVersion = "situation-sweep-v1"

// SweepArgs configures a single situation sweep run.
type SweepArgs struct {
	Model                 string
	Brief                 string
	HTTPTimeout           time.Duration
	UseJSONResponseFormat bool // if false, omit response_format (some models reject it)
}

// RunSweep calls the model, parses stories, and upserts rows tied to sweep_id.
func RunSweep(ctx context.Context, db *sql.DB, client *Client, args SweepArgs) (sweepID int64, _ error) {
	if client == nil || client.APIKey == "" {
		return 0, fmt.Errorf("openrouter: missing API key")
	}
	if args.Model == "" {
		return 0, fmt.Errorf("openrouter: missing model")
	}
	if client.HTTPClient == nil {
		t := args.HTTPTimeout
		if t <= 0 {
			t = 2 * time.Minute
		}
		client.HTTPClient = &http.Client{Timeout: t}
	}

	system := `You are a situation-awareness analyst. Use current, verifiable public information.
Return ONLY valid JSON (no markdown) matching this shape:
{"stories":[{"title":"string","summary":"string","why_it_matters":"string","urgency":1-5,"region":"string","tags":["string"],"sources":[{"url":"https://...","title":"string","is_x":false}],"x_angle":["optional short bullets about discourse on X/social"]}]}
Rules:
- 5–15 stories max; prioritize developments from roughly the last 24–48h when possible.
- urgency 5 = imminent / market-moving / major security or policy shock; 1 = minor.
- Every story should have at least one http(s) source URL when possible.
- If you are uncertain, lower urgency and say so in summary.`

	user := strings.TrimSpace(args.Brief)
	if user == "" {
		user = "Global + United States focus. Geopolitics, markets, tech/policy, major disasters."
	}
	user += "\n\nRespond with JSON only."

	sid, err := store.BeginSweep(ctx, db, args.Model, promptVersion)
	if err != nil {
		return 0, err
	}

	req := ChatRequest{
		Model:       args.Model,
		Temperature: 0.35,
		Messages: []Message{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
	}
	if args.UseJSONResponseFormat {
		req.ResponseFormat = &ResponseFormat{Type: "json_object"}
	}

	raw, err := client.ChatCompletion(ctx, req)
	if err != nil {
		_ = store.FinishSweep(ctx, db, sid, "error", err.Error(), "")
		return sid, err
	}

	parsed, err := ParseSweepResponse(raw)
	if err != nil {
		_ = store.FinishSweep(ctx, db, sid, "error", "parse: "+err.Error(), raw)
		return sid, fmt.Errorf("parse sweep json: %w", err)
	}

	for _, st := range parsed.Stories {
		ext := storyExternalID(st)
		if ext == "" {
			continue
		}
		url := primaryURL(st)
		tags, _ := json.Marshal(st.Tags)
		if len(tags) == 0 || string(tags) == "null" {
			tags = []byte("[]")
		}
		summary := buildSummary(st)
		if err := store.UpsertSweepItem(ctx, db, sid, ext, strings.TrimSpace(st.Title), summary, url, st.Urgency, string(tags), strings.TrimSpace(st.Region)); err != nil {
			_ = store.FinishSweep(ctx, db, sid, "error", "db: "+err.Error(), raw)
			return sid, err
		}
	}

	if err := store.FinishSweep(ctx, db, sid, "ok", "", raw); err != nil {
		return sid, err
	}
	return sid, nil
}

func buildSummary(st SweepStory) string {
	var b strings.Builder
	s := strings.TrimSpace(st.Summary)
	if s != "" {
		b.WriteString(s)
	}
	if w := strings.TrimSpace(st.Why); w != "" {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString("Why it matters: ")
		b.WriteString(w)
	}
	if len(st.XAngle) > 0 {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString("X / social angle:\n- ")
		b.WriteString(strings.Join(st.XAngle, "\n- "))
	}
	out := b.String()
	const max = 16000
	if len(out) > max {
		return out[:max] + "…"
	}
	return out
}

func primaryURL(st SweepStory) string {
	for _, s := range st.Sources {
		u := strings.TrimSpace(s.URL)
		if strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
			return u
		}
	}
	return ""
}

func storyExternalID(st SweepStory) string {
	title := strings.TrimSpace(st.Title)
	url := primaryURL(st)
	region := strings.TrimSpace(st.Region)
	if title == "" && url == "" {
		return ""
	}
	h := sha256.Sum256([]byte(strings.ToLower(title) + "\n" + url + "\n" + region))
	return "s:" + hex.EncodeToString(h[:])
}
