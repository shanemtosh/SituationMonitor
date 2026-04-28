package httpserver

import (
	"database/sql"
	_ "embed"
	"html/template"
	"net/http"
	"sort"
	"strings"

	"situationmonitor/internal/store"
)

//go:embed situations_list.html
var situationsListHTML string

var situationsListTmpl = template.Must(template.New("situations-list").Parse(situationsListHTML))

//go:embed situation_detail.html
var situationDetailHTML string

var situationDetailTmpl = template.Must(template.New("situation-detail").Parse(situationDetailHTML))

// situationListRow is the per-row shape rendered into the situations list page.
type situationListRow struct {
	Name        string
	Slug        string
	Status      string
	ItemCount   int
	Snippet     string
	ActivityAgo string
	ChildCount  int
}

type situationsListData struct {
	User       *store.User
	Situations []situationListRow
	Sort       string
	Status     string
}

func handleSituationsListPage(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		sortKey := strings.TrimSpace(r.URL.Query().Get("sort"))
		if sortKey == "" {
			sortKey = "activity"
		}
		status := r.URL.Query().Get("status")
		if _, ok := r.URL.Query()["status"]; !ok {
			status = "active"
		}

		var sits []store.SituationRow
		var err error
		switch sortKey {
		case "items":
			sits, err = store.ListSituations(ctx, db, status, 100)
			if err == nil {
				sort.SliceStable(sits, func(i, j int) bool { return sits[i].ItemCount > sits[j].ItemCount })
			}
		case "created":
			sits, err = store.ListSituations(ctx, db, status, 100)
			if err == nil {
				sort.SliceStable(sits, func(i, j int) bool { return sits[i].CreatedAt > sits[j].CreatedAt })
			}
		default: // "activity"
			sortKey = "activity"
			sits, err = store.ListSituationsByActivity(ctx, db, status, 100)
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Count children per parent in a single pass.
		childCounts := make(map[int64]int)
		for _, s := range sits {
			if s.ParentID != nil {
				childCounts[*s.ParentID]++
			}
		}

		rows := make([]situationListRow, 0, len(sits))
		for _, s := range sits {
			activity := ""
			if s.LastItemAt != "" {
				activity = timeAgo(s.LastItemAt)
			} else if s.UpdatedAt != "" {
				activity = timeAgo(s.UpdatedAt)
			}
			rows = append(rows, situationListRow{
				Name:        s.Name,
				Slug:        s.Slug,
				Status:      s.Status,
				ItemCount:   s.ItemCount,
				Snippet:     s.Snippet,
				ActivityAgo: activity,
				ChildCount:  childCounts[s.ID],
			})
		}

		data := situationsListData{
			User:       UserFromContext(ctx),
			Situations: rows,
			Sort:       sortKey,
			Status:     status,
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = situationsListTmpl.Execute(w, data)
	}
}

// situationDetailItem is the per-item shape rendered into the detail page.
type situationDetailItem struct {
	Title          string
	DisplayTitle   string // translated if available
	URL            string
	Source         string
	AgeAgo         string
	Urgency        int
	DisplaySummary string
}

type situationDetailChild struct {
	Name      string
	Slug      string
	ItemCount int
}

type situationDetailParent struct {
	Name string
	Slug string
}

type situationDetailData struct {
	User        *store.User
	Situation   store.SituationRow
	Items       []situationDetailItem
	Children    []situationDetailChild
	Parent      *situationDetailParent
	ActivityAgo string
	CreatedAgo  string
	SnippetAgo  string
}

func handleSituationDetailPage(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		slug := r.PathValue("slug")
		sit, err := store.GetSituation(ctx, db, slug)
		if err != nil {
			http.NotFound(w, r)
			return
		}

		items, _ := store.ListSituationItems(ctx, db, sit.ID, 100)
		children, _ := store.ListChildSituations(ctx, db, sit.ID)

		var parent *situationDetailParent
		if sit.ParentID != nil {
			parents, _ := store.ListSituations(ctx, db, "", 100)
			for _, p := range parents {
				if p.ID == *sit.ParentID {
					parent = &situationDetailParent{Name: p.Name, Slug: p.Slug}
					break
				}
			}
		}

		dItems := make([]situationDetailItem, 0, len(items))
		for _, it := range items {
			dt := it.TitleTrans
			if dt == "" {
				dt = it.Title
			}
			ds := it.SummaryTrans
			if ds == "" {
				ds = it.Summary
			}
			dItems = append(dItems, situationDetailItem{
				Title:          it.Title,
				DisplayTitle:   dt,
				URL:            it.URL,
				Source:         feedName(it.FeedURL),
				AgeAgo:         timeAgo(it.CreatedAt),
				Urgency:        it.Urgency,
				DisplaySummary: ds,
			})
		}

		dChildren := make([]situationDetailChild, 0, len(children))
		for _, c := range children {
			dChildren = append(dChildren, situationDetailChild{
				Name: c.Name, Slug: c.Slug, ItemCount: c.ItemCount,
			})
		}

		// Activity is derived from most recent linked item if available.
		activity := ""
		if len(items) > 0 {
			activity = timeAgo(items[0].CreatedAt)
		} else if sit.UpdatedAt != "" {
			activity = timeAgo(sit.UpdatedAt)
		}

		data := situationDetailData{
			User:        UserFromContext(ctx),
			Situation:   sit,
			Items:       dItems,
			Children:    dChildren,
			Parent:      parent,
			ActivityAgo: activity,
			CreatedAgo:  timeAgo(sit.CreatedAt),
			SnippetAgo:  optionalTimeAgo(sit.SnippetGeneratedAt),
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = situationDetailTmpl.Execute(w, data)
	}
}

func optionalTimeAgo(rfc3339 string) string {
	if rfc3339 == "" {
		return ""
	}
	return timeAgo(rfc3339)
}
