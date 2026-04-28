package httpserver

import (
	"database/sql"
	_ "embed"
	"encoding/json"
	"html/template"
	"net/http"

	"situationmonitor/internal/store"
)

//go:embed saved.html
var savedHTML string

var savedTmpl = template.Must(template.New("saved").Parse(savedHTML))

//go:embed settings.html
var settingsHTML string

var settingsTmpl = template.Must(template.New("settings").Parse(settingsHTML))

// Saved page data types

type savedHighlight struct {
	ActionID int64
	ItemID   int64
	Text     string
	Note     string
}

type savedHighlightGroup struct {
	ItemID     int64
	ItemTitle  string
	Highlights []savedHighlight
}

type savedNote struct {
	ActionID  int64
	Text      string
	CreatedAt string
}

type savedNoteGroup struct {
	ItemID    int64
	ItemTitle string
	Notes     []savedNote
}

type savedData struct {
	User            *store.User
	SavedItems      []store.SavedItem
	HighlightGroups []savedHighlightGroup
	NoteGroups      []savedNoteGroup
}

func handleSaved(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := UserFromContext(r.Context())
		if u == nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		ctx := r.Context()

		saved, _ := store.ListSavedItems(ctx, db, u.ID)

		// Build highlight groups
		hlRaw, _ := store.ListHighlights(ctx, db, u.ID)
		hlGroupMap := make(map[int64]*savedHighlightGroup)
		var hlGroups []savedHighlightGroup
		for _, h := range hlRaw {
			var d struct {
				Text string `json:"text"`
				Note string `json:"note"`
			}
			_ = json.Unmarshal(h.Data, &d)

			g, ok := hlGroupMap[h.ItemID]
			if !ok {
				hlGroups = append(hlGroups, savedHighlightGroup{ItemID: h.ItemID, ItemTitle: h.ItemTitle})
				g = &hlGroups[len(hlGroups)-1]
				hlGroupMap[h.ItemID] = g
			}
			g.Highlights = append(g.Highlights, savedHighlight{
				ActionID: h.ActionID,
				ItemID:   h.ItemID,
				Text:     d.Text,
				Note:     d.Note,
			})
		}

		// Build note groups
		notesRaw, _ := store.ListNotes(ctx, db, u.ID)
		noteGroupMap := make(map[int64]*savedNoteGroup)
		var noteGroups []savedNoteGroup
		for _, n := range notesRaw {
			var d struct {
				Text string `json:"text"`
			}
			_ = json.Unmarshal(n.Data, &d)

			g, ok := noteGroupMap[n.ItemID]
			if !ok {
				noteGroups = append(noteGroups, savedNoteGroup{ItemID: n.ItemID, ItemTitle: n.ItemTitle})
				g = &noteGroups[len(noteGroups)-1]
				noteGroupMap[n.ItemID] = g
			}
			g.Notes = append(g.Notes, savedNote{
				ActionID:  n.ActionID,
				Text:      d.Text,
				CreatedAt: n.CreatedAt,
			})
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = savedTmpl.Execute(w, savedData{
			User:            u,
			SavedItems:      saved,
			HighlightGroups: hlGroups,
			NoteGroups:      noteGroups,
		})
	}
}

// Settings page

type hiddenFeedEntry struct {
	ActionID int64
	FeedURL  string
	FeedName string
}

type settingsData struct {
	User        *store.User
	HiddenFeeds []hiddenFeedEntry
}

func handleSettings(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := UserFromContext(r.Context())
		if u == nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		ctx := r.Context()

		hidden, _ := store.ListHiddenFeeds(ctx, db, u.ID)
		var feeds []hiddenFeedEntry
		for _, h := range hidden {
			var d struct {
				FeedURL string `json:"feed_url"`
			}
			_ = json.Unmarshal(h.Data, &d)
			feeds = append(feeds, hiddenFeedEntry{
				ActionID: h.ID,
				FeedURL:  d.FeedURL,
				FeedName: feedName(d.FeedURL),
			})
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = settingsTmpl.Execute(w, settingsData{User: u, HiddenFeeds: feeds})
	}
}
