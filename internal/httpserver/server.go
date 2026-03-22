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

//go:embed about.html
var aboutHTML string

var aboutTmpl = template.Must(template.New("about").Parse(aboutHTML))

//go:embed legal.html
var legalHTML string

var legalTmpl = template.Must(template.New("legal").Parse(legalHTML))

// ReaderConfig holds settings for on-demand article fetching and translation.
type ReaderConfig struct {
	OllamaBaseURL     string
	OllamaModel       string
	TargetLang        string
	PaywallFetcherURL string // e.g. "http://127.0.0.1:3100" — empty disables

	// Brief generation: if OpenRouterAPIKey is set, use OpenRouter for briefs.
	// Otherwise fall back to Ollama.
	OpenRouterAPIKey  string
	OpenRouterBaseURL string
	BriefModel        string // e.g. "deepseek/deepseek-chat-v3-0324"
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
	mux.HandleFunc("GET /about", handleAbout())
	mux.HandleFunc("GET /terms", handleLegalPage("Terms of Use", termsBody))
	mux.HandleFunc("GET /privacy", handleLegalPage("Privacy Policy", privacyBody))
	mux.HandleFunc("GET /content-notice", handleLegalPage("Content Notice", contentNoticeBody))
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
	Region     string
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
				FeedName: feedName(it.FeedURL), Region: store.FeedRegionMap[it.FeedURL], Tags: tags,
			})
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = dashboardTmpl.Execute(w, dashData{Quotes: dq, Items: di, Filter: f})
	}
}

// feedName extracts a short publication name from a feed URL.
// e.g. "https://feeds.bbci.co.uk/news/world/rss.xml" → "BBC"
var knownFeeds = map[string]string{
	// Western / English
	"bbci.co.uk/news/world": "BBC",
	"bbci.co.uk/mundo":      "BBC Mundo",
	"nytimes.com":           "NYT",
	"theguardian.com":       "Guardian",
	"reuters.com":           "Reuters",
	"aljazeera.com":         "Al Jazeera",
	"washingtonpost.com":    "WaPo",
	"feeds.npr.org":         "NPR",
	"thehill.com":           "The Hill",
	"dowjones.io":           "WSJ",
	"bloomberg.com":         "Bloomberg",
	"foreignaffairs.com":    "Foreign Affairs",
	"foreignpolicy.com":     "Foreign Policy",
	"propublica.org":        "ProPublica",
	"theintercept.com":      "The Intercept",
	"cnbc.com":              "CNBC",
	"cnn.com":               "CNN",
	"npr.org":               "NPR",
	"apnews.com":            "AP",
	// Europe
	"france24.com":          "France 24",
	"rfi.fr":                "RFI",
	"lemonde.fr":            "Le Monde",
	"dw.com":                "DW",
	"spiegel.de":            "Der Spiegel",
	"elpais.com":            "EL PAIS",
	"ansa.it":               "ANSA",
	"repubblica.it":         "La Repubblica",
	"politico.eu":           "Politico EU",
	"euobserver.com":        "EUobserver",
	"euronews.com":          "Euronews",
	// Eastern Europe & Russia
	"themoscowtimes.com":    "Moscow Times",
	"tass.com":              "TASS",
	"ukrinform.net":         "Ukrinform",
	"notesfrompoland.com":   "Notes from Poland",
	"balkaninsight.com":     "Balkan Insight",
	"feeds.yle.fi":          "YLE",
	// Asia
	"nhk.or.jp":             "NHK",
	"nikkei.com":            "Nikkei",
	"yna.co.kr":             "Yonhap",
	"koreaherald.com":       "Korea Herald",
	"cgtn.com":              "CGTN",
	"hindustantimes.com":    "Hindustan Times",
	"scmp.com":              "SCMP",
	"chinadaily.com.cn":     "China Daily",
	"taipeitimes.com":       "Taipei Times",
	"money.udn.com":         "UDN Economic Daily",
	// Latin America
	"batimes.com.ar":        "BA Times",
	"caracaschronicles.com": "Caracas Chronicles",
	"agenciabrasil.ebc.com.br/en": "Agencia Brasil",
	"agenciabrasil.ebc.com.br/rss": "Agencia Brasil",
	"latinamericareports.com": "LatAm Reports",
	"lanacion.com.ar":       "La Nacion",
	"infobae.com":           "Infobae",
	"eltiempo.com":          "El Tiempo",
	"eluniversal.com.mx":    "El Universal",
	"latercera.com":         "La Tercera",
	"efectococuyo.com":      "Efecto Cocuyo",
	// US Government
	"federalreserve.gov":    "Federal Reserve",
	"whitehouse.gov":        "White House",
	"census.gov":            "Census Bureau",
	"bea.gov":               "BEA",
	"treasury://":           "Treasury",
	// Semiconductors & industry
	"digitimes.com":         "DigiTimes",
	"trendforce.com":        "TrendForce",
	"eetimes.com":           "EE Times",
	"semiengineering.com":   "SemiEngineering",
	"semianalysis":          "SemiAnalysis",
	"kedglobal.com":         "KED Global",
	// Supply chain
	"supplychaindive.com":   "Supply Chain Dive",
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
		Region:     strings.TrimSpace(q.Get("region")),
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

type readerEntity struct {
	Name      string
	Kind      string
	ItemCount int
}

type readerSituation struct {
	Name      string
	Slug      string
	Status    string
	ItemCount int
}

type readerData struct {
	DisplayTitle    string
	DisplaySummary  string
	URL             string
	SourceKind      string
	FeedName        string
	Lang            string
	CreatedAt       string
	Content         string
	Error           string
	FetchFailed     bool
	WasTranslated   bool
	TranslatorModel string
	ItemID          int64
	BriefText       string
	IsInternal      bool // true when accessed via Tailscale/localhost
	Entities        []readerEntity
	Situations      []readerSituation
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

		internal := isInternalRequest(r)

		data := readerData{
			DisplayTitle: item.Title,
			URL:          item.URL,
			SourceKind:   item.SourceKind,
			FeedName:     feedName(item.FeedURL),
			Lang:         item.Lang,
			CreatedAt:    item.CreatedAt,
			ItemID:       id,
			BriefText:    item.BriefText,
			IsInternal:   internal,
		}
		if item.TitleTranslated != "" {
			data.DisplayTitle = item.TitleTranslated
		}
		if item.SummaryTranslated != "" {
			data.DisplaySummary = item.SummaryTranslated
		} else if item.Summary != "" {
			data.DisplaySummary = item.Summary
		}

		// Load knowledge graph entities and situations for this item
		if ents, err := store.GetItemEntities(ctx, db, id); err == nil {
			for _, e := range ents {
				data.Entities = append(data.Entities, readerEntity{Name: e.Name, Kind: e.Kind, ItemCount: e.ItemCount})
			}
		}
		if sits, err := store.GetItemSituations(ctx, db, id); err == nil {
			for _, s := range sits {
				data.Situations = append(data.Situations, readerSituation{Name: s.Name, Slug: s.Slug, Status: s.Status, ItemCount: s.ItemCount})
			}
		}

		// Full content: only serve to internal requests
		if !internal {
			// Public access: show summary only, no full article
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_ = readerTmpl.Execute(w, data)
			return
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
			data.FetchFailed = true
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

// isInternalRequest returns true if the request comes from localhost, Tailscale,
// or has the X-Internal header set by nginx for trusted origins.
func isInternalRequest(r *http.Request) bool {
	// Check X-Internal header (set by nginx for Tailscale-origin requests)
	if r.Header.Get("X-Internal") == "true" {
		return true
	}
	// Check if direct localhost access
	host := r.Host
	if strings.HasPrefix(host, "127.0.0.1") || strings.HasPrefix(host, "localhost") || strings.HasPrefix(host, "[::1]") {
		return true
	}
	// Check RemoteAddr for local connections
	remote := r.RemoteAddr
	if strings.HasPrefix(remote, "127.0.0.1") || strings.HasPrefix(remote, "[::1]") || strings.HasPrefix(remote, "100.") {
		return true // 100.x.y.z = Tailscale
	}
	return false
}

func handleAbout() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = aboutTmpl.Execute(w, nil)
	}
}

type legalPage struct {
	Title    string
	BodyHTML template.HTML
}

func handleLegalPage(title string, body template.HTML) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = legalTmpl.Execute(w, legalPage{Title: title, BodyHTML: body})
	}
}

const termsBody template.HTML = `
<p>Last updated: March 2026</p>

<h2>Acceptance</h2>
<p>By accessing Situation Monitor, you agree to these terms. If you do not agree, please do not use the service.</p>

<h2>Service Description</h2>
<p>Situation Monitor is a news aggregation and monitoring tool operated by Mimir. It collects and displays publicly available information from RSS feeds, social media platforms, and market data providers for informational purposes only.</p>

<h2>No Warranty</h2>
<p>The service is provided "as is" without warranty of any kind. We make no guarantees about the accuracy, completeness, or timeliness of the information displayed. Content is sourced from third parties and may contain errors.</p>

<h2>Not Financial or Professional Advice</h2>
<p>Nothing on this service constitutes financial, investment, legal, or other professional advice. Market data is provided for informational purposes only and may be delayed. Always consult qualified professionals before making decisions based on information found here.</p>

<h2>AI-Generated Content</h2>
<p>Some content, including daily briefings and article summaries, is generated or processed by AI systems. AI-generated content may contain inaccuracies or misinterpretations of source material. Always verify important information against original sources.</p>

<h2>Limitation of Liability</h2>
<p>To the fullest extent permitted by law, Mimir shall not be liable for any damages arising from your use of or inability to use this service.</p>

<h2>Changes</h2>
<p>We may update these terms at any time. Continued use of the service constitutes acceptance of the updated terms.</p>

<h2>Contact</h2>
<p>Questions about these terms? Email <a href="mailto:shane@mto.sh">shane@mto.sh</a></p>
`

const privacyBody template.HTML = `
<p>Last updated: March 2026</p>

<h2>Overview</h2>
<p>Situation Monitor is operated by Mimir. We take privacy seriously and collect minimal data.</p>

<h2>What We Collect</h2>
<ul>
	<li><strong>Server logs:</strong> Standard HTTP access logs (IP address, user agent, pages visited) for security and debugging. These are retained for a limited time and not shared with third parties.</li>
	<li><strong>Local storage:</strong> Your theme preference (light/dark mode) is stored in your browser's local storage. This never leaves your device.</li>
</ul>

<h2>What We Don't Collect</h2>
<ul>
	<li>No cookies or tracking pixels</li>
	<li>No analytics or advertising scripts</li>
	<li>No personal accounts or user profiles</li>
	<li>No data shared with or sold to third parties</li>
</ul>

<h2>Third-Party Content</h2>
<p>This service displays content from third-party sources (news publishers, social media platforms, market data providers). When you click through to an original source, that publisher's own privacy policy applies.</p>

<h2>AI Processing</h2>
<p>Content is processed by AI systems (Ollama, OpenRouter) for entity extraction, translation, and summarization. This processing happens server-side; your browsing activity is not sent to AI providers.</p>

<h2>Contact</h2>
<p>Privacy questions? Email <a href="mailto:shane@mto.sh">shane@mto.sh</a></p>
`

const contentNoticeBody template.HTML = `
<p>Last updated: March 2026</p>

<h2>Third-Party Content</h2>
<p>Situation Monitor aggregates and displays content from third-party sources including news publishers, social media platforms, and market data providers. All such content remains the property of its respective owners and publishers.</p>

<h2>No Claim of Ownership</h2>
<p>Mimir does not claim ownership, copyright, or any proprietary rights over content produced by third-party publishers. Headlines, summaries, and article text displayed on this service are sourced from publicly available RSS feeds and are attributed to their original publishers.</p>

<h2>Fair Use and Attribution</h2>
<p>Content is displayed for personal, non-commercial, informational purposes. We provide attribution to original sources wherever possible, including publisher names, article links, and timestamps. We encourage users to visit original sources for complete reporting.</p>

<h2>AI-Generated Summaries</h2>
<p>AI-generated summaries, briefings, and translations are derivative works produced for personal informational use. These are not intended to substitute for or replace the original reporting. Original source links are provided so users can access the full, authoritative content from publishers.</p>

<h2>Market Data</h2>
<p>Market quotes and financial data are sourced from third-party providers and may be delayed. This data is provided for informational purposes only and should not be relied upon for trading decisions.</p>

<h2>Social Media Content</h2>
<p>Social media content displayed in sweeps is collected from public posts and represents the views of the original authors, not Mimir or Situation Monitor.</p>

<h2>Takedown Requests</h2>
<p>If you are a content owner and believe your content is being displayed inappropriately, please contact <a href="mailto:shane@mto.sh">shane@mto.sh</a> and we will address your concern promptly.</p>

<h2>DMCA</h2>
<p>We respect intellectual property rights. If you believe content on this service infringes your copyright, please send a notice to <a href="mailto:shane@mto.sh">shane@mto.sh</a> including identification of the copyrighted work, the infringing material and its location on our service, and your contact information.</p>
`
