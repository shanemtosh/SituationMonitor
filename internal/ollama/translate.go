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

// langNames maps ISO 639-1 codes to full language names for TranslateGemma prompts.
var langNames = map[string]string{
	"ja": "Japanese",
	"zh": "Chinese",
	"ko": "Korean",
	"es": "Spanish",
	"pt": "Portuguese",
	"it": "Italian",
	"fr": "French",
	"de": "German",
	"ar": "Arabic",
	"ru": "Russian",
}

// TranslateText translates a single text string from sourceLang to targetLang
// using TranslateGemma's recommended prompt format.
func TranslateText(ctx context.Context, baseURL, model, sourceLang, targetLang, text string) (string, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" || strings.TrimSpace(model) == "" {
		return text, fmt.Errorf("ollama: missing base URL or model")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", nil
	}

	srcName := langNames[sourceLang]
	if srcName == "" && sourceLang != "" {
		srcName = sourceLang
	}
	tgtName := targetLang
	tgtCode := "en"
	if strings.EqualFold(targetLang, "English") {
		tgtCode = "en"
	}

	// TranslateGemma recommended prompt format
	var prompt string
	if srcName != "" && sourceLang != "" {
		prompt = fmt.Sprintf(
			"You are a professional %s (%s) to %s (%s) translator. "+
				"Your goal is to accurately convey the meaning and nuances of the original %s text "+
				"while adhering to %s grammar, vocabulary, and cultural sensitivities. "+
				"Produce only the %s translation, without any additional explanations or commentary. "+
				"Please translate the following %s text into %s:\n\n\n%s",
			srcName, sourceLang, tgtName, tgtCode,
			srcName, tgtName, tgtName, srcName, tgtName,
			text,
		)
	} else {
		// Source language unknown — simpler prompt
		prompt = fmt.Sprintf(
			"Translate the following text into %s (%s). "+
				"Produce only the %s translation, without any additional explanations or commentary.\n\n\n%s",
			tgtName, tgtCode, tgtName, text,
		)
	}

	body, err := json.Marshal(map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
		"stream": false,
		"options": map[string]any{
			"temperature": 0.1,
			"num_ctx":     2048,
		},
	})
	if err != nil {
		return text, err
	}

	client := &http.Client{Timeout: 5 * time.Minute}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/chat", bytes.NewReader(body))
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
		return text, fmt.Errorf("ollama HTTP %s: %s", resp.Status, string(raw))
	}

	var parsed struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return text, fmt.Errorf("ollama decode: %w", err)
	}
	result := strings.TrimSpace(parsed.Message.Content)
	if result == "" {
		return text, nil
	}
	return result, nil
}
