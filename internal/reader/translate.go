package reader

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

// TranslateContent translates article text to the target language using Ollama.
// Returns the translated text, or the original if already in the target language.
func TranslateContent(ctx context.Context, ollamaURL, model, targetLang, text string) (string, error) {
	if strings.TrimSpace(text) == "" || strings.TrimSpace(model) == "" {
		return text, nil
	}

	// Chunk long text to fit in context window
	chunks := chunkText(text, 4000)
	var translated []string

	for _, chunk := range chunks {
		result, err := translateChunk(ctx, ollamaURL, model, targetLang, chunk)
		if err != nil {
			return text, err
		}
		translated = append(translated, result)
	}

	return strings.Join(translated, "\n\n"), nil
}

func translateChunk(ctx context.Context, ollamaURL, model, targetLang, text string) (string, error) {
	prompt := fmt.Sprintf(`Translate the following text to %s. Output ONLY the translated text, no explanations or markup.

%s`, targetLang, text)

	body, err := json.Marshal(map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": "You are a professional translator. Output only the translated text, preserving paragraph structure."},
			{"role": "user", "content": prompt},
		},
		"stream": false,
		"options": map[string]any{
			"temperature": 0.1,
			"num_ctx":     4096,
		},
	})
	if err != nil {
		return text, err
	}

	client := &http.Client{Timeout: 5 * time.Minute}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ollamaURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return text, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return text, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return text, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return text, fmt.Errorf("ollama HTTP %s", resp.Status)
	}

	var parsed struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return text, err
	}

	result := strings.TrimSpace(parsed.Message.Content)
	if result == "" {
		return text, nil
	}
	return result, nil
}

func chunkText(text string, maxChars int) []string {
	if len(text) <= maxChars {
		return []string{text}
	}
	var chunks []string
	paragraphs := strings.Split(text, "\n\n")
	var current strings.Builder
	for _, p := range paragraphs {
		if current.Len()+len(p)+2 > maxChars && current.Len() > 0 {
			chunks = append(chunks, current.String())
			current.Reset()
		}
		if current.Len() > 0 {
			current.WriteString("\n\n")
		}
		current.WriteString(p)
	}
	if current.Len() > 0 {
		chunks = append(chunks, current.String())
	}
	return chunks
}
