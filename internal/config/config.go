package config

import (
	"os"
	"strconv"
	"strings"
	"time"

	"situationmonitor/internal/market"
)

// Config holds runtime settings from the environment.
type Config struct {
	ListenAddr       string
	DatabasePath     string
	PagesDir         string
	OpenRouterAPIKey string
	OpenRouterModel  string
	OpenRouterBaseURL string
	OpenRouterJSON   bool
	OpenRouterHTTPTimeout time.Duration

	OllamaBaseURL   string
	OllamaTranslate string
	TranslateTarget string
	TranslatePoll   time.Duration
	TranslateBatch  int
	TranslateOnStart bool

	NERModel         string
	NERPoll          time.Duration
	NERBatch         int
	NEROnStart       bool
	SituationMinItems int

	RSSFeedsFile     string
	RSSPollInterval  time.Duration
	RSSFetchTimeout  time.Duration
	RSSUserAgent     string
	RSSIngestOnStart bool

	SweepBriefPath   string
	SweepPoll        time.Duration
	SweepOnStart     bool

	NtfyServer          string
	NtfyTopic           string
	NtfyToken           string
	AlertMinUrgency     int
	AlertMaxPerHour     int

	MarketSymbols   []string
	MarketPoll      time.Duration
	MarketFetchTO   time.Duration
	MarketOnStart   bool

	PaywallFetcherURL string
}

// Load reads configuration from the environment.
func Load() (Config, error) {
	rssPoll := durSec("RSS_POLL_SEC", 900)
	sweepPoll := durSec("SWEEP_POLL_SEC", 3600)
	trPoll := durSec("TRANSLATE_POLL_SEC", 600)
	mktPoll := durSec("MARKET_POLL_SEC", 120)

	orTO := durSec("OPENROUTER_HTTP_TIMEOUT_SEC", 120)
	if orTO <= 0 {
		orTO = 2 * time.Minute
	}

	c := Config{
		ListenAddr:            getEnv("LISTEN_ADDR", "127.0.0.1:8080"),
		DatabasePath:          getEnv("DATABASE_PATH", "./data/situation.db"),
		PagesDir:              getEnv("PAGES_DIR", "./data/pages"),
		OpenRouterAPIKey:      strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY")),
		OpenRouterModel:       getEnv("OPENROUTER_MODEL", "x-ai/grok-4-fast"),
		OpenRouterBaseURL:     strings.TrimRight(strings.TrimSpace(getEnv("OPENROUTER_BASE_URL", "https://openrouter.ai/api/v1")), "/"),
		OpenRouterJSON:        ParseBool("OPENROUTER_JSON_OBJECT", true),
		OpenRouterHTTPTimeout: orTO,

		OllamaBaseURL:    strings.TrimRight(getEnv("OLLAMA_BASE_URL", "http://127.0.0.1:11434"), "/"),
		OllamaTranslate:  getEnv("OLLAMA_TRANSLATE_MODEL", ""),
		TranslateTarget:  getEnv("TRANSLATE_TARGET_LANG", "English"),
		TranslatePoll:    trPoll,
		TranslateBatch:   ParseInt("TRANSLATE_BATCH", 15),
		TranslateOnStart: ParseBool("TRANSLATE_ON_START", true),

		NERModel:          getEnv("NER_MODEL", ""),
		NERPoll:           durSec("NER_POLL_SEC", 900),
		NERBatch:          ParseInt("NER_BATCH", 10),
		NEROnStart:        ParseBool("NER_ON_START", true),
		SituationMinItems: ParseInt("SITUATION_MIN_ITEMS", 4),

		RSSFeedsFile:     getEnv("RSS_FEEDS_FILE", "config/feeds.txt"),
		RSSPollInterval:  rssPoll,
		RSSFetchTimeout:  durSec("RSS_FETCH_TIMEOUT_SEC", 45),
		RSSUserAgent:     getEnv("RSS_USER_AGENT", "SituationMonitor/1.0 (+local; RSS)"),
		RSSIngestOnStart: ParseBool("RSS_INGEST_ON_START", true),

		SweepBriefPath: getEnv("SWEEP_BRIEF_FILE", "config/brief.txt"),
		SweepPoll:      sweepPoll,
		SweepOnStart:   ParseBool("SWEEP_ON_START", true),

		NtfyServer:      strings.TrimRight(strings.TrimSpace(getEnv("NTFY_SERVER", "https://ntfy.sh")), "/"),
		NtfyTopic:       strings.TrimSpace(os.Getenv("NTFY_TOPIC")),
		NtfyToken:       strings.TrimSpace(os.Getenv("NTFY_TOKEN")),
		AlertMinUrgency: ParseInt("ALERT_MIN_URGENCY", 4),
		AlertMaxPerHour: ParseInt("ALERT_MAX_PER_HOUR", 12),

		MarketSymbols: market.ParseSymbols(os.Getenv("MARKET_SYMBOLS")),
		MarketPoll:    mktPoll,
		MarketFetchTO: durSec("MARKET_FETCH_TIMEOUT_SEC", 30),
		MarketOnStart: ParseBool("MARKET_ON_START", true),

		PaywallFetcherURL: strings.TrimRight(strings.TrimSpace(os.Getenv("PAYWALL_FETCHER_URL")), "/"),
	}

	if c.RSSFetchTimeout <= 0 {
		c.RSSFetchTimeout = 45 * time.Second
	}
	if c.MarketFetchTO <= 0 {
		c.MarketFetchTO = 30 * time.Second
	}
	return c, nil
}

func durSec(key string, defSec int) time.Duration {
	sec := ParseInt(key, defSec)
	if sec <= 0 {
		return 0
	}
	return time.Duration(sec) * time.Second
}

func getEnv(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

// ParseBool parses a boolean env var; missing or invalid returns default.
func ParseBool(key string, def bool) bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	switch v {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return def
	}
}

// ParseInt parses an int env var; invalid or missing returns default.
func ParseInt(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}
