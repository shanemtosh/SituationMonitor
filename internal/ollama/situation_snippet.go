package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// SnippetItem is a recent headline used to seed a situation snippet.
type SnippetItem struct {
	Title   string
	Summary string
	Source  string
	Age     string // e.g. "3h ago"
}

// GenerateSituationSnippet asks Ollama to summarize the latest state of a
// tracked situation in 1-2 sentences, given recent items.
func GenerateSituationSnippet(ctx context.Context, baseURL, model, situationName string, items []SnippetItem) (string, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" || strings.TrimSpace(model) == "" {
		return "", fmt.Errorf("ollama: missing base URL or model")
	}
	if len(items) == 0 {
		return "", fmt.Errorf("ollama: no items provided")
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Situation: %s\n\nRecent headlines (newest first):\n", situationName))
	for i, it := range items {
		if i >= 10 {
			break
		}
		line := fmt.Sprintf("- [%s, %s] %s", it.Source, it.Age, it.Title)
		if it.Summary != "" {
			line += " — " + it.Summary
		}
		sb.WriteString(line + "\n")
	}
	sb.WriteString("\nWrite 1-2 sentences (max 280 characters) describing the current state of this situation based only on these headlines. Plain prose, no markdown, no editorializing, no preamble. Do not invent facts not present above.")

	body, err := json.Marshal(map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": "You write terse, factual situation summaries from news headlines. No speculation."},
			{"role": "user", "content": sb.String()},
		},
		"stream": false,
		"options": map[string]any{
			"temperature": 0.2,
			"num_ctx":     2048,
		},
	})
	if err != nil {
		return "", err
	}

	client := &http.Client{Timeout: 90 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("ollama HTTP %s: %s", resp.Status, string(raw))
	}

	var parsed struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", fmt.Errorf("ollama decode: %w", err)
	}

	out := strings.TrimSpace(parsed.Message.Content)
	out = strings.Trim(out, `"'`)
	if len(out) > 400 {
		out = out[:400]
	}
	return out, nil
}
