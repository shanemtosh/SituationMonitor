package httpserver

import (
	"bytes"
	"database/sql"
	_ "embed"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"

	"situationmonitor/internal/store"
	"github.com/yuin/goldmark"
)

// digestsRoot is where the weekly alpha digests are written by the
// .claude/skills/{geopolitics,macro,semiconductors,energy}-alpha.md skills'
// Step D. Path is relative to the server's working directory.
const digestsRoot = "data/alpha/digests"

// week directory name like "2026-W17"
var weekRe = regexp.MustCompile(`^\d{4}-W\d{2}$`)

// digestDomains is the canonical order for rendering sections.
var digestDomains = []struct {
	Key   string
	Label string
}{
	{"geopolitics", "Geopolitics"},
	{"macro", "Macro & Monetary Policy"},
	{"semiconductors", "Semiconductors & Technology"},
	{"energy", "Energy"},
}

//go:embed alpha_digests_index.html
var alphaDigestsIndexHTML string

var alphaDigestsIndexTmpl = template.Must(template.New("digests-index").Parse(alphaDigestsIndexHTML))

//go:embed alpha_digest.html
var alphaDigestHTML string

var alphaDigestTmpl = template.Must(template.New("digest").Parse(alphaDigestHTML))

type digestsIndexEntry struct {
	Week string // 2026-W17
	URL  string // /alpha/digests/2026-W17
	HasGeopolitics, HasMacro, HasSemiconductors, HasEnergy bool
}

type digestsIndexData struct {
	User    *store.User
	Entries []digestsIndexEntry
}

func handleDigestsIndex(_ *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		entries, _ := os.ReadDir(digestsRoot)
		var list []digestsIndexEntry
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			name := e.Name()
			if !weekRe.MatchString(name) {
				continue
			}
			row := digestsIndexEntry{
				Week: name,
				URL:  fmt.Sprintf("/alpha/digests/%s", name),
			}
			for _, d := range digestDomains {
				if _, err := os.Stat(filepath.Join(digestsRoot, name, d.Key+".md")); err == nil {
					switch d.Key {
					case "geopolitics":
						row.HasGeopolitics = true
					case "macro":
						row.HasMacro = true
					case "semiconductors":
						row.HasSemiconductors = true
					case "energy":
						row.HasEnergy = true
					}
				}
			}
			list = append(list, row)
		}
		sort.Slice(list, func(i, j int) bool { return list[i].Week > list[j].Week })

		data := digestsIndexData{
			User:    UserFromContext(r.Context()),
			Entries: list,
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = alphaDigestsIndexTmpl.Execute(w, data)
	}
}

type digestSection struct {
	Domain   string
	Label    string
	BodyHTML template.HTML
}

type digestPageData struct {
	User     *store.User
	Week     string
	Sections []digestSection
}

func handleDigestPage(_ *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		week := r.PathValue("week")
		if !weekRe.MatchString(week) {
			http.NotFound(w, r)
			return
		}
		dir := filepath.Join(digestsRoot, week)
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			http.NotFound(w, r)
			return
		}

		md := goldmark.New()
		sections := make([]digestSection, 0, len(digestDomains))
		anyFound := false
		for _, d := range digestDomains {
			path := filepath.Join(dir, d.Key+".md")
			raw, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			anyFound = true
			var buf bytes.Buffer
			if err := md.Convert(raw, &buf); err != nil {
				http.Error(w, "render: "+err.Error(), http.StatusInternalServerError)
				return
			}
			sections = append(sections, digestSection{
				Domain:   d.Key,
				Label:    d.Label,
				BodyHTML: template.HTML(buf.String()),
			})
		}
		if !anyFound {
			http.NotFound(w, r)
			return
		}

		data := digestPageData{
			User:     UserFromContext(r.Context()),
			Week:     week,
			Sections: sections,
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = alphaDigestTmpl.Execute(w, data)
	}
}
