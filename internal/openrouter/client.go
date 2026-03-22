package openrouter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const defaultBaseURL = "https://openrouter.ai/api/v1"

// Client calls OpenRouter's OpenAI-compatible API.
type Client struct {
	HTTPClient *http.Client
	APIKey     string
	BaseURL    string
}

// ChatRequest is a minimal chat completions payload.
type ChatRequest struct {
	Model          string           `json:"model"`
	Messages       []Message        `json:"messages"`
	Temperature    float64          `json:"temperature,omitempty"`
	ResponseFormat *ResponseFormat  `json:"response_format,omitempty"`
}

// Message is a chat message.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ResponseFormat requests JSON output when the model supports it.
type ResponseFormat struct {
	Type string `json:"type"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// ChatCompletion runs one non-streaming completion and returns assistant text.
func (c *Client) ChatCompletion(ctx context.Context, req ChatRequest) (string, error) {
	if c.BaseURL == "" {
		c.BaseURL = defaultBaseURL
	}
	if c.HTTPClient == nil {
		c.HTTPClient = &http.Client{Timeout: 2 * time.Minute}
	}
	body, err := json.Marshal(req)
	if err != nil {
		return "", err
	}
	url := c.BaseURL + "/chat/completions"
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	r.Header.Set("Authorization", "Bearer "+c.APIKey)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("HTTP-Referer", "https://situation.mto.sh")
	r.Header.Set("X-Title", "Situation Monitor")

	resp, err := c.HTTPClient.Do(r)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("openrouter HTTP %s: %s", resp.Status, string(b))
	}
	var parsed chatResponse
	if err := json.Unmarshal(b, &parsed); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	if parsed.Error != nil && parsed.Error.Message != "" {
		return "", fmt.Errorf("openrouter error: %s", parsed.Error.Message)
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("openrouter: empty choices")
	}
	return parsed.Choices[0].Message.Content, nil
}
