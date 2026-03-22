package htmltext

import (
	"html"
	"regexp"
	"strings"
)

var (
	reBlockTags  = regexp.MustCompile(`(?i)</(p|div|br|li|tr|h[1-6]|blockquote|section|article|header|footer|figcaption)\s*>`)
	reBR         = regexp.MustCompile(`(?i)<br\s*/?>`)
	reAllTags    = regexp.MustCompile(`<[^>]*>`)
	reInlineWS   = regexp.MustCompile(`[^\S\n]+`)
	reBlankLines = regexp.MustCompile(`\n{3,}`)
	reNoPeriodSpace = regexp.MustCompile(`\.([A-Z])`)
)

// Strip converts HTML to plain text: strips tags, decodes entities,
// normalizes whitespace, and ensures paragraph breaks are preserved.
func Strip(s string) string {
	if s == "" {
		return ""
	}

	// Insert newlines before block-level closing tags so paragraphs separate
	s = reBlockTags.ReplaceAllString(s, "\n$0")
	// <br> → newline
	s = reBR.ReplaceAllString(s, "\n")
	// Remove all remaining HTML tags
	s = reAllTags.ReplaceAllString(s, "")
	// Decode HTML entities (&amp; &#39; &quot; etc.)
	s = html.UnescapeString(s)
	// Normalize inline whitespace per line (preserve newlines)
	lines := strings.Split(s, "\n")
	var out []string
	for _, line := range lines {
		line = reInlineWS.ReplaceAllString(line, " ")
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	result := strings.Join(out, "\n")
	// Collapse runs of blank lines
	result = reBlankLines.ReplaceAllString(result, "\n\n")
	// Fix missing space after periods (e.g. "sentence.Next" → "sentence. Next")
	result = reNoPeriodSpace.ReplaceAllString(result, ". $1")
	return strings.TrimSpace(result)
}
