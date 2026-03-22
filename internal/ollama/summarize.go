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

// RelatedItem is a related news item used for contextual briefing.
type RelatedItem struct {
	Title   string
	Summary string
	Source  string
	Age     string // e.g. "3h ago", "2d ago"
}

// BriefOnItem generates a contextual synthesis given a pivot item and related coverage.
func BriefOnItem(ctx context.Context, baseURL, model, pivotTitle, pivotSummary string, related []RelatedItem) (string, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" || strings.TrimSpace(model) == "" {
		return "", fmt.Errorf("ollama: missing base URL or model")
	}

	var sb strings.Builder
	sb.WriteString("Synthesize a brief intelligence summary about this topic based on multiple sources.\n\n")
	sb.WriteString(fmt.Sprintf("PRIMARY ITEM:\nTitle: %s\nSummary: %s\n\n", pivotTitle, pivotSummary))
	sb.WriteString("RELATED COVERAGE:\n")
	for i, r := range related {
		if i >= 20 {
			break
		}
		sb.WriteString(fmt.Sprintf("%d. [%s, %s] %s — %s\n", i+1, r.Source, r.Age, r.Title, r.Summary))
	}
	sb.WriteString("\nWrite a 3-5 paragraph synthesis that:\n")
	sb.WriteString("- Explains the core situation\n")
	sb.WriteString("- Notes where sources agree or differ\n")
	sb.WriteString("- Highlights what changed most recently\n")
	sb.WriteString("- Assesses why this matters\n")
	sb.WriteString("\nBe direct and analytical. No preamble.")

	body, err := json.Marshal(map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": "You are an intelligence analyst producing concise situational briefings from multiple news sources."},
			{"role": "user", "content": sb.String()},
		},
		"stream": false,
		"options": map[string]any{
			"temperature": 0.3,
			"num_ctx":     4096,
		},
	})
	if err != nil {
		return "", err
	}

	client := &http.Client{Timeout: 5 * time.Minute}
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
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
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
	return strings.TrimSpace(parsed.Message.Content), nil
}

// NameSituation generates a short situation name from a set of related headlines.
func NameSituation(ctx context.Context, baseURL, model string, titles []string) (string, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" || strings.TrimSpace(model) == "" {
		return "", fmt.Errorf("ollama: missing base URL or model")
	}

	var sb strings.Builder
	sb.WriteString("Given these related news headlines, provide a 3-6 word situation name that captures the core event or trend.\n\n")
	sb.WriteString("Headlines:\n")
	for _, t := range titles {
		sb.WriteString(fmt.Sprintf("- %s\n", t))
	}
	sb.WriteString("\nOutput ONLY the situation name. No punctuation, no explanation, no quotes.")

	body, err := json.Marshal(map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": "You name evolving news stories with concise, descriptive titles."},
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

	client := &http.Client{Timeout: 2 * time.Minute}
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

	name := strings.TrimSpace(parsed.Message.Content)
	// Clean up: remove quotes, trailing periods
	name = strings.Trim(name, `"'`)
	name = strings.TrimRight(name, ".")
	if name == "" {
		return "Unnamed Situation", nil
	}
	return name, nil
}
