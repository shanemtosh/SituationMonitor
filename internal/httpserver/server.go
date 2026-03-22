package httpserver

import (
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"situationmonitor/internal/reader"
	"situationmonitor/internal/store"
	"gopkg.in/yaml.v3"
)

//go:embed dashboard.html
var dashboardHTML string

var dashboardTmpl = template.Must(template.New("dash").Parse(dashboardHTML))

// ReaderConfig holds settings for on-demand article fetching and translation.
type ReaderConfig struct {
	OllamaBaseURL     string
	OllamaModel       string
	TargetLang        string
	PaywallFetcherURL string // e.g. "http://127.0.0.1:3100" — empty disables
}

// Mount registers HTTP routes on mux. pagesDir is the directory where
// generated daily briefing YAML files are stored (e.g. "data/pages").
func Mount(mux *http.ServeMux, db *sql.DB, pagesDir string, rc ReaderConfig) {
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("GET /", handleDashboard(db))
	mux.HandleFunc("GET /api/items", handleItemsJSON(db))
	mux.HandleFunc("GET /api/markets", handleMarketsJSON(db))
	mux.HandleFunc("GET /api/sweeps", handleSweepsJSON(db))
	mux.HandleFunc("GET /daily/", handleDailyIndex(pagesDir))
	mux.HandleFunc("GET /daily/{date}", handleDailyPage(pagesDir))
	mux.HandleFunc("GET /read/{id}", handleReader(db, rc))
	mux.HandleFunc("GET /api/brief/{id}", handleBriefItem(db, rc))
	mux.HandleFunc("GET /api/situations", handleSituationsJSON(db))
	mux.HandleFunc("GET /api/situations/{slug}", handleSituationDetail(db))
	mux.HandleFunc("GET /api/entities/{name}", handleEntityDetail(db))
	mountManageRoutes(mux, db)
}

type itemJSON struct {
	ID           int64           `json:"id"`
	CreatedAt    string          `json:"created_at"`
	SourceKind   string          `json:"source_kind"`
	Title        string          `json:"title"`
	Summary      string          `json:"summary,omitempty"`
	URL          string          `json:"url,omitempty"`
	FeedURL      string          `json:"feed_url,omitempty"`
	Urgency      int             `json:"urgency"`
	Lang         string          `json:"lang,omitempty"`
	TitleTrans   string          `json:"title_translated,omitempty"`
	SummaryTrans string          `json:"summary_translated,omitempty"`
	Tags         json.RawMessage `json:"tags,omitempty"`
	ClusterKey   string          `json:"cluster_key,omitempty"`
}

func handleItemsJSON(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		f := parseItemFilter(r)
		rows, err := store.ListItems(ctx, db, f)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		out := make([]itemJSON, 0, len(rows))
		for _, it := range rows {
			j := itemJSON{
				ID: it.ID, CreatedAt: it.CreatedAt, SourceKind: it.SourceKind,
				Title: it.Title, Summary: it.Summary, URL: it.URL, FeedURL: it.FeedURL,
				Urgency: it.Urgency, Lang: it.Lang, TitleTrans: it.TitleTrans, SummaryTrans: it.SummaryTrans,
				ClusterKey: it.ClusterKey,
			}
			if strings.TrimSpace(it.TagsJSON) != "" {
				j.Tags = json.RawMessage(it.TagsJSON)
			}
			out = append(out, j)
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		enc := json.NewEncoder(w)
		enc.SetEscapeHTML(true)
		_ = enc.Encode(out)
	}
}

func handleMarketsJSON(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		q, err := store.ListQuotes(ctx, db)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		type row struct {
			Symbol    string  `json:"symbol"`
			Name      string  `json:"name"`
			Price     float64 `json:"price"`
			ChangePct float64 `json:"change_pct"`
			Currency  string  `json:"currency"`
			FetchedAt string  `json:"fetched_at"`
		}
		var out []row
		for _, x := range q {
			out = append(out, row{
				Symbol: x.Symbol, Name: x.Name, Price: x.Price, ChangePct: x.ChangePct, Currency: x.Currency,
				FetchedAt: x.FetchedAt.UTC().Format(time.RFC3339),
			})
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(out)
	}
}

func handleSweepsJSON(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		limit := 30
		if q := r.URL.Query().Get("limit"); q != "" {
			if n, err := strconv.Atoi(q); err == nil && n > 0 && n <= 200 {
				limit = n
			}
		}
		s, err := store.ListSweeps(ctx, db, limit)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(s)
	}
}

type dashQuote struct {
	Symbol    string
	Name      string
	Price     float64
	ChangePct float64
	Currency  string
	FetchedAt string
}

type dashItem struct {
	ID         int64
	CreatedAt  string
	Urgency    int
	SourceKind string
	Title      string
	TitleTrans string
	Summary    string
	URL        string
	FeedName   string
	Tags       []string
}

type dashData struct {
	Quotes []dashQuote
	Items  []dashItem
	Filter store.ItemFilter
}

func handleDashboard(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		f := parseItemFilter(r)
		if r.URL.Query().Get("hours") == "" {
			f.Hours = 72
		}
		if r.URL.Query().Get("limit") == "" {
			f.Limit = 80
		}

		items, err := store.ListItems(ctx, db, f)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		quotes, _ := store.ListQuotes(ctx, db)

		dq := make([]dashQuote, 0, len(quotes))
		for _, q := range quotes {
			dq = append(dq, dashQuote{
				Symbol: q.Symbol, Name: q.Name, Price: q.Price, ChangePct: q.ChangePct, Currency: q.Currency,
				FetchedAt: q.FetchedAt.UTC().Format(time.RFC3339),
			})
		}
		di := make([]dashItem, 0, len(items))
		for _, it := range items {
			var tags []string
			_ = json.Unmarshal([]byte(it.TagsJSON), &tags)
			summary := it.SummaryTrans
			if summary == "" {
				summary = it.Summary
			}
			// Truncate summary for feed view
			if len(summary) > 200 {
				summary = summary[:200] + "…"
			}
			di = append(di, dashItem{
				ID: it.ID, CreatedAt: it.CreatedAt, Urgency: it.Urgency, SourceKind: it.SourceKind,
				Title: it.Title, TitleTrans: it.TitleTrans, Summary: summary, URL: it.URL,
				FeedName: feedName(it.FeedURL), Tags: tags,
			})
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = dashboardTmpl.Execute(w, dashData{Quotes: dq, Items: di, Filter: f})
	}
}

// feedName extracts a short publication name from a feed URL.
// e.g. "https://feeds.bbci.co.uk/news/world/rss.xml" → "BBC"
var knownFeeds = map[string]string{
	// General news
	"bbci.co.uk":         "BBC",
	"nytimes.com":        "NYT",
	"theguardian.com":    "Guardian",
	"reuters.com":        "Reuters",
	"aljazeera.com":      "Al Jazeera",
	"washingtonpost.com": "WaPo",
	"cnbc.com":           "CNBC",
	"cnn.com":            "CNN",
	"npr.org":            "NPR",
	"apnews.com":         "AP",
	// Asia
	"nhk.or.jp":          "NHK",
	"nikkei.com":         "Nikkei",
	"yna.co.kr":          "Yonhap",
	"koreaherald.com":    "Korea Herald",
	"cgtn.com":           "CGTN",
	"hindustantimes.com": "Hindustan Times",
	"scmp.com":           "SCMP",
	"chinadaily.com.cn":  "China Daily",
	"taipeitimes.com":    "Taipei Times",
	// Semiconductors & industry
	"digitimes.com":      "DigiTimes",
	"trendforce.com":     "TrendForce",
	"eetimes.com":        "EE Times",
	"semiengineering.com": "SemiEngineering",
	"semianalysis":       "SemiAnalysis",
	"kedglobal.com":      "KED Global",
	// Supply chain
	"supplychaindive.com": "Supply Chain Dive",
}

func feedName(feedURL string) string {
	if feedURL == "" {
		return ""
	}
	lower := strings.ToLower(feedURL)
	for domain, name := range knownFeeds {
		if strings.Contains(lower, domain) {
			return name
		}
	}
	// Fallback: extract domain
	if i := strings.Index(feedURL, "://"); i >= 0 {
		host := feedURL[i+3:]
		if j := strings.Index(host, "/"); j >= 0 {
			host = host[:j]
		}
		host = strings.TrimPrefix(host, "www.")
		host = strings.TrimPrefix(host, "feeds.")
		if k := strings.LastIndex(host, "."); k > 0 {
			host = host[:k]
		}
		return host
	}
	return ""
}

func parseItemFilter(r *http.Request) store.ItemFilter {
	q := r.URL.Query()
	f := store.ItemFilter{
		SourceKind: strings.TrimSpace(q.Get("source")),
	}
	if v := q.Get("min_u"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 && n <= 5 {
			f.MinUrgency = n
		}
	}
	if v := q.Get("hours"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			f.Hours = n
		}
	}
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			f.Limit = n
		}
	}
	return f
}

// dateRe matches YYYY-MM-DD to prevent path traversal.
var dateRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// Briefing YAML types

type briefingSource struct {
	Name  string `yaml:"name"`
	Title string `yaml:"title"`
	URL   string `yaml:"url"`
}

type briefingInstrument struct {
	Name      string `yaml:"name"`
	Price     string `yaml:"price"`
	Move      string `yaml:"move"`
	Direction string `yaml:"direction"`
}

type briefingMarkets struct {
	Narrative   string               `yaml:"narrative"`
	Instruments []briefingInstrument `yaml:"instruments"`
}

type briefingStory struct {
	Title   string           `yaml:"title"`
	Urgency int              `yaml:"urgency"`
	Body    string           `yaml:"body"`
	Body2   string           `yaml:"body2"`
	Why     string           `yaml:"why"`
	Sources []briefingSource `yaml:"sources"`
}

func (s briefingStory) UrgencyLabel() string {
	switch s.Urgency {
	case 5:
		return "CRIT"
	case 4:
		return "HIGH"
	case 3:
		return "MOD"
	case 2:
		return "LOW"
	default:
		return "INFO"
	}
}

type briefingTheme struct {
	Title string `yaml:"title"`
	Body  string `yaml:"body"`
}

type briefingWatch struct {
	Title string `yaml:"title"`
	Body  string `yaml:"body"`
}

type briefingData struct {
	Date       string           `yaml:"date"`
	Weekday    string           `yaml:"weekday"`
	Summary    string           `yaml:"summary"`
	Markets    briefingMarkets  `yaml:"markets"`
	Stories    []briefingStory  `yaml:"stories"`
	Themes     []briefingTheme  `yaml:"themes"`
	Social     string           `yaml:"social"`
	Watchlist  []briefingWatch  `yaml:"watchlist"`
	AllSources []briefingSource `yaml:"all_sources"`
}

//go:embed briefing.html
var briefingHTML string

var briefingTmpl = template.Must(template.New("briefing").Parse(briefingHTML))

func handleDailyPage(pagesDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		date := r.PathValue("date")
		if !dateRe.MatchString(date) {
			http.NotFound(w, r)
			return
		}
		path := filepath.Join(pagesDir, date+".yaml")
		raw, err := os.ReadFile(path)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		var b briefingData
		if err := yaml.Unmarshal(raw, &b); err != nil {
			http.Error(w, "bad briefing data", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = briefingTmpl.Execute(w, b)
	}
}

//go:embed daily_index.html
var dailyIndexHTML string

var dailyIndexTmpl = template.Must(template.New("daily-index").Parse(dailyIndexHTML))

func handleDailyIndex(pagesDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/daily/" {
			http.NotFound(w, r)
			return
		}
		entries, _ := os.ReadDir(pagesDir)
		type briefing struct {
			Date string
			URL  string
		}
		var list []briefing
		for _, e := range entries {
			name := e.Name()
			if strings.HasSuffix(name, ".yaml") {
				date := strings.TrimSuffix(name, ".yaml")
				if dateRe.MatchString(date) {
					list = append(list, briefing{Date: date, URL: fmt.Sprintf("/daily/%s", date)})
				}
			}
		}
		sort.Slice(list, func(i, j int) bool { return list[i].Date > list[j].Date })
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = dailyIndexTmpl.Execute(w, list)
	}
}

//go:embed reader.html
var readerHTML string

var readerTmpl = template.Must(template.New("reader").Parse(readerHTML))

type readerData struct {
	DisplayTitle   string
	DisplaySummary string
	URL            string
	SourceKind     string
	FeedName       string
	Lang           string
	CreatedAt      string
	Content        string
	Error          string
	WasTranslated  bool
	TranslatorModel string
}

func handleReader(db *sql.DB, rc ReaderConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := r.PathValue("id")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			http.NotFound(w, r)
			return
		}

		ctx := r.Context()
		item, err := store.GetReaderItem(ctx, db, id)
		if err != nil {
			http.NotFound(w, r)
			return
		}

		data := readerData{
			DisplayTitle: item.Title,
			URL:          item.URL,
			SourceKind:   item.SourceKind,
			FeedName:     feedName(item.FeedURL),
			Lang:         item.Lang,
			CreatedAt:    item.CreatedAt,
		}
		if item.TitleTranslated != "" {
			data.DisplayTitle = item.TitleTranslated
		}
		if item.SummaryTranslated != "" {
			data.DisplaySummary = item.SummaryTranslated
		} else if item.Summary != "" {
			data.DisplaySummary = item.Summary
		}

		// Serve cached content if available
		if item.ContentFetchedAt != "" {
			if item.ContentTranslated != "" {
				data.Content = item.ContentTranslated
				data.WasTranslated = true
				data.TranslatorModel = rc.OllamaModel
			} else {
				data.Content = item.ContentText
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_ = readerTmpl.Execute(w, data)
			return
		}

		// Fetch article content on demand
		if item.URL == "" {
			data.Error = "No URL available for this item."
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_ = readerTmpl.Execute(w, data)
			return
		}

		article, err := reader.Fetch(ctx, item.URL, reader.FetchConfig{
			PaywallFetcherURL: rc.PaywallFetcherURL,
		})
		if err != nil {
			log.Printf("reader: fetch %d: %v", id, err)
			data.Error = fmt.Sprintf("Could not fetch article: %v", err)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_ = readerTmpl.Execute(w, data)
			return
		}

		contentText := article.Content
		var contentTranslated string

		// Translate if non-English and Ollama is configured
		needsTranslation := item.Lang != "" && item.Lang != "en" && item.Lang != "und"
		if needsTranslation && rc.OllamaModel != "" {
			translated, err := reader.TranslateContent(ctx, rc.OllamaBaseURL, rc.OllamaModel, rc.TargetLang, contentText)
			if err != nil {
				log.Printf("reader: translate %d: %v", id, err)
			} else {
				contentTranslated = translated
			}
		}

		// Cache the result
		if err := store.SetContent(ctx, db, id, contentText, contentTranslated); err != nil {
			log.Printf("reader: cache %d: %v", id, err)
		}

		if contentTranslated != "" {
			data.Content = contentTranslated
			data.WasTranslated = true
			data.TranslatorModel = rc.OllamaModel
		} else {
			data.Content = contentText
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = readerTmpl.Execute(w, data)
	}
}

