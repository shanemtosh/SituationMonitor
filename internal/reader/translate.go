package reader

import (
	"context"
	"fmt"
	"strings"

	"situationmonitor/internal/ollama"
)

// TranslateContent translates article text to the target language using Ollama.
// Chunks long text to fit within TranslateGemma's 2K token context window.
func TranslateContent(ctx context.Context, ollamaURL, model, targetLang, text string) (string, error) {
	if strings.TrimSpace(text) == "" || strings.TrimSpace(model) == "" {
		return text, nil
	}

	// Chunk to ~1200 chars to stay within TranslateGemma's 2K token limit
	// (prompt template ~200 tokens + source text + output tokens)
	chunks := chunkText(text, 1200)
	var translated []string

	for _, chunk := range chunks {
		result, err := ollama.TranslateText(ctx, ollamaURL, model, "", targetLang, chunk)
		if err != nil {
			return text, err
		}

		// Hallucination guard: if the result is wildly different in length, reject it
		ratio := float64(len(result)) / float64(len(chunk))
		if ratio < 0.2 || ratio > 3.0 {
			return text, fmt.Errorf("translation length ratio %.1f suggests hallucination (input=%d, output=%d)", ratio, len(chunk), len(result))
		}

		translated = append(translated, result)
	}

	return strings.Join(translated, "\n\n"), nil
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
