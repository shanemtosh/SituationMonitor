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

// TranslateToTarget asks the local model for JSON: {lang, title, summary} in the target language (usually English).
func TranslateToTarget(ctx context.Context, baseURL, model, targetLang, title, summary string) (lang, titleOut, summaryOut string, err error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" || strings.TrimSpace(model) == "" {
		return "", "", "", fmt.Errorf("ollama: missing base URL or model")
	}
	sum := strings.TrimSpace(summary)
	if len(sum) > 6000 {
		sum = sum[:6000] + "…"
	}
	user := fmt.Sprintf(`Title: %s

Summary:
%s

Return JSON only with this exact shape:
{"lang":"<detected ISO 639-1 code>","title":"<title in %s>","summary":"<summary in %s>"}

If the text is already in %s, keep wording the same but still fill all fields.`,
		strings.TrimSpace(title), sum, targetLang, targetLang, targetLang)

	body, err := json.Marshal(map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": "You are a careful news translator. Output valid JSON only, no markdown."},
			{"role": "user", "content": user},
		},
		"stream": false,
		"options": map[string]any{
			"temperature": 0.1,
		},
	})
	if err != nil {
		return "", "", "", err
	}

	client := &http.Client{Timeout: 3 * time.Minute}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return "", "", "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", "", "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", "", "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", "", fmt.Errorf("ollama HTTP %s: %s", resp.Status, string(raw))
	}

	var parsed struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", "", "", fmt.Errorf("ollama decode: %w", err)
	}
	content := strings.TrimSpace(parsed.Message.Content)
	type out struct {
		Lang    string `json:"lang"`
		Title   string `json:"title"`
		Summary string `json:"summary"`
	}
	obj := extractJSONObject(content)
	var o out
	if err := json.Unmarshal([]byte(obj), &o); err != nil {
		return "", "", "", fmt.Errorf("ollama json: %w", err)
	}
	return strings.TrimSpace(o.Lang), strings.TrimSpace(o.Title), strings.TrimSpace(o.Summary), nil
}

func extractJSONObject(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```")
		s = strings.TrimPrefix(s, "json")
		s = strings.TrimSpace(s)
		if i := strings.LastIndex(s, "```"); i >= 0 {
			s = strings.TrimSpace(s[:i])
		}
	}
	start := strings.IndexByte(s, '{')
	if start < 0 {
		return s
	}
	depth := 0
	for i := start; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return strings.TrimSpace(s[start:])
}
