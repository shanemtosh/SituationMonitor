package openrouter

import (
	"encoding/json"
	"strings"
)

// ExtractJSONObject returns the first JSON object found in s (handles ```json fences).
func ExtractJSONObject(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```")
		s = strings.TrimPrefix(s, "json")
		s = strings.TrimSpace(s)
		if i := strings.LastIndex(s, "```"); i >= 0 {
			s = strings.TrimSpace(s[:i])
		}
	}
	// Find outermost { ... } by brace depth
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

// ParseSweepResponse decodes the model JSON into stories.
func ParseSweepResponse(raw string) (SweepResponse, error) {
	obj := ExtractJSONObject(raw)
	var out SweepResponse
	if err := json.Unmarshal([]byte(obj), &out); err != nil {
		return SweepResponse{}, err
	}
	return out, nil
}
