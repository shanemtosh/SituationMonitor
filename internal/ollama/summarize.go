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

	"situationmonitor/internal/openrouter"
)

// RelatedItem is a related news item used for contextual briefing.
type RelatedItem struct {
	Title   string
	Summary string
	Source  string
	Age     string // e.g. "3h ago", "2d ago"
}

// BriefContext holds all inputs for generating a brief.
type BriefContext struct {
	Title      string
	Content    string // full article content or summary
	Source     string // publication name
	Entities   []string // knowledge graph entities, e.g. "Iran (PLACE)"
	Situations []string // tracked situations, e.g. "US-Iran War [active]"
	Related    []RelatedItem
}

const briefSystemPrompt = `You are an intelligence analyst producing concise situational briefings from multiple news sources. Write in plain text without markdown formatting — no bold, no headers, no bullet points. Use clear paragraphs.

IMPORTANT: Only state facts that are explicitly present in the provided text. Do not infer, assume, or fabricate details that are not in the source material. If the provided text is incomplete or truncated, base your analysis only on what is available. The source publication name is for attribution context — do not conflate the publication's country or name with the topic of the article.`

// buildBriefPrompt constructs the user prompt for brief generation.
func buildBriefPrompt(bc BriefContext) string {
	var sb strings.Builder

	if len(bc.Related) > 0 {
		sb.WriteString("Synthesize a brief intelligence summary about this topic based on multiple sources.\n\n")
	} else {
		sb.WriteString("Write a brief intelligence-style analysis of this news item.\n\n")
	}

	// Primary item
	sb.WriteString("PRIMARY ITEM:\n")
	sb.WriteString(fmt.Sprintf("Title: %s\n", bc.Title))
	if bc.Source != "" {
		sb.WriteString(fmt.Sprintf("Publication: %s\n", bc.Source))
	}
	sb.WriteString(fmt.Sprintf("Content:\n%s\n", bc.Content))

	// Knowledge graph context
	if len(bc.Entities) > 0 {
		sb.WriteString(fmt.Sprintf("\nKNOWN ENTITIES: %s\n", strings.Join(bc.Entities, ", ")))
	}
	if len(bc.Situations) > 0 {
		sb.WriteString(fmt.Sprintf("TRACKED SITUATIONS: %s\n", strings.Join(bc.Situations, ", ")))
	}

	// Related coverage
	if len(bc.Related) > 0 {
		sb.WriteString("\nRELATED COVERAGE:\n")
		for i, r := range bc.Related {
			if i >= 20 {
				break
			}
			sb.WriteString(fmt.Sprintf("%d. [%s, %s] %s", i+1, r.Source, r.Age, r.Title))
			if r.Summary != "" {
				sb.WriteString(fmt.Sprintf(" — %s", r.Summary))
			}
			sb.WriteString("\n")
		}

		sb.WriteString("\nWrite a 3-5 paragraph synthesis that:\n")
		sb.WriteString("- Explains the core situation based on what the sources actually say\n")
		sb.WriteString("- Uses the knowledge graph entities and situations to frame the analysis\n")
		sb.WriteString("- Notes where sources agree or differ\n")
		sb.WriteString("- Highlights what changed most recently\n")
		sb.WriteString("- Assesses why this matters and potential implications\n")
	} else {
		sb.WriteString("\nWrite 2-3 paragraphs that:\n")
		sb.WriteString("- Explain what happened and the context based on what the article actually says\n")
		sb.WriteString("- Use the knowledge graph entities and situations for framing if available\n")
		sb.WriteString("- Assess why this matters and potential implications\n")
	}

	sb.WriteString("\nBe direct and analytical. No preamble. No markdown formatting. Do not assume or fabricate details not present in the source text.")
	return sb.String()
}

// BriefViaOpenRouter generates a brief using OpenRouter API.
func BriefViaOpenRouter(ctx context.Context, apiKey, baseURL, model string, bc BriefContext) (string, error) {
	if strings.TrimSpace(apiKey) == "" {
		return "", fmt.Errorf("openrouter: missing API key")
	}
	if model == "" {
		model = "deepseek/deepseek-v3.2"
	}
	client := &openrouter.Client{
		APIKey:  apiKey,
		BaseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
	}
	text, err := client.ChatCompletion(ctx, openrouter.ChatRequest{
		Model: model,
		Messages: []openrouter.Message{
			{Role: "system", Content: briefSystemPrompt},
			{Role: "user", Content: buildBriefPrompt(bc)},
		},
		Temperature: 0.3,
	})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(text), nil
}

// BriefOnItem generates a contextual synthesis given a pivot item and related coverage via Ollama.
func BriefOnItem(ctx context.Context, baseURL, model string, bc BriefContext) (string, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" || strings.TrimSpace(model) == "" {
		return "", fmt.Errorf("ollama: missing base URL or model")
	}

	prompt := buildBriefPrompt(bc)

	body, err := json.Marshal(map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": briefSystemPrompt},
			{"role": "user", "content": prompt},
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
