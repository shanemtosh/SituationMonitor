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

// Entity is a named entity extracted from a news item.
type Entity struct {
	Name string `json:"name"`
	Kind string `json:"kind"` // PERSON, ORG, PLACE, TOPIC
}

// ExtractEntities calls Ollama to extract named entities from a news item's title and summary.
func ExtractEntities(ctx context.Context, baseURL, model, title, summary string) ([]Entity, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" || strings.TrimSpace(model) == "" {
		return nil, fmt.Errorf("ollama: missing base URL or model")
	}

	sum := strings.TrimSpace(summary)
	if len(sum) > 4000 {
		sum = sum[:4000] + "…"
	}

	user := fmt.Sprintf(`Extract named entities from this news item.

Title: %s
Summary: %s

Reply with ONLY a JSON array (max 12 entities):
[{"name":"...","kind":"PERSON"},{"name":"...","kind":"ORG"},...]

Rules:
- kind must be one of: PERSON ORG PLACE TOPIC
- PLACE: countries, cities, regions (e.g. Syria, Damascus, Middle East, Gaza, Strait of Hormuz)
- PERSON: named individuals (e.g. Donald Trump, Xi Jinping)
- ORG: organizations, companies, government bodies (e.g. NATO, Federal Reserve, TSMC)
- TOPIC: major events, policies, issues (e.g. Iran war, climate change, AI chips)
- Always extract country names as PLACE
- Always extract city names as PLACE
- Normalize to canonical English form
- Do NOT include generic terms like "authorities", "residents", "officials"`,
		strings.TrimSpace(title), sum)

	body, err := json.Marshal(map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": "You are a named entity extractor for news articles. Output valid JSON only, no markdown."},
			{"role": "user", "content": user},
		},
		"stream": false,
		"options": map[string]any{
			"temperature": 0.1,
			"num_ctx":     2048,
		},
	})
	if err != nil {
		return nil, err
	}

	client := &http.Client{Timeout: 3 * time.Minute}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("ollama HTTP %s: %s", resp.Status, string(raw))
	}

	var parsed struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("ollama decode: %w", err)
	}
	content := strings.TrimSpace(parsed.Message.Content)

	// Strip markdown fences
	content = stripFences(content)

	// Parse JSON array
	var entities []Entity
	if err := json.Unmarshal([]byte(content), &entities); err != nil {
		// Try extracting array from surrounding text
		if start := strings.IndexByte(content, '['); start >= 0 {
			if end := strings.LastIndexByte(content, ']'); end > start {
				if err2 := json.Unmarshal([]byte(content[start:end+1]), &entities); err2 != nil {
					return nil, nil // skip silently, will retry
				}
			}
		}
		if entities == nil {
			return nil, nil
		}
	}

	// Validate kinds
	valid := make([]Entity, 0, len(entities))
	for _, e := range entities {
		e.Name = strings.TrimSpace(e.Name)
		e.Kind = strings.ToUpper(strings.TrimSpace(e.Kind))
		if e.Name == "" {
			continue
		}
		switch e.Kind {
		case "PERSON", "ORG", "PLACE", "TOPIC":
			valid = append(valid, e)
		default:
			e.Kind = "TOPIC"
			valid = append(valid, e)
		}
	}
	return valid, nil
}

func stripFences(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```")
		s = strings.TrimPrefix(s, "json")
		s = strings.TrimSpace(s)
		if i := strings.LastIndex(s, "```"); i >= 0 {
			s = strings.TrimSpace(s[:i])
		}
	}
	return s
}
