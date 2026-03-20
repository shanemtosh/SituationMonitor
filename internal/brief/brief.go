package brief

import (
	"os"
	"strings"
)

const defaultText = `Focus areas:
- United States domestic policy, elections, and regulatory moves affecting tech/finance.
- Major geopolitical flashpoints (Middle East, Europe, Asia-Pacific) and security incidents.
- Global markets: rates, FX, commodities, and large equity moves with a clear catalyst.
- Technology: AI policy, cyber incidents, major platform outages.

Ignore celebrity gossip and sports unless they create market or security impact.
Prefer primary reporting and official sources; include URLs in your JSON sources.`

// Load reads the situation brief file, or returns a built-in default if missing.
func Load(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return strings.TrimSpace(defaultText), nil
		}
		return "", err
	}
	s := strings.TrimSpace(string(b))
	if s == "" {
		return strings.TrimSpace(defaultText), nil
	}
	return s, nil
}
