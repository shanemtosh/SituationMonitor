package feeds

import (
	"bufio"
	"os"
	"strings"
)

// Feed represents a single RSS feed with its region tag.
type Feed struct {
	URL    string
	Region string
}

// LoadFeeds reads feeds from path. Each non-comment, non-empty line is
// either "region|url" or just "url" (region defaults to "").
func LoadFeeds(path string) ([]Feed, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []Feed
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		var feed Feed
		if region, url, ok := strings.Cut(line, "|"); ok {
			feed.Region = region
			feed.URL = url
		} else {
			feed.URL = line
		}
		out = append(out, feed)
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// LoadURLs reads one feed URL per line (backward-compatible).
// It parses the region|url format but returns only URLs.
func LoadURLs(path string) ([]string, error) {
	feeds, err := LoadFeeds(path)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(feeds))
	for _, f := range feeds {
		out = append(out, f.URL)
	}
	return out, nil
}
