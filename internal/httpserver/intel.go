package httpserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"situationmonitor/internal/ollama"
	"situationmonitor/internal/store"
)

type entityJSON struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	ItemCount int    `json:"item_count"`
}

type situationJSON struct {
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	Status    string `json:"status"`
	ItemCount int    `json:"item_count"`
}

type sitOutJSON struct {
	ID                 int64        `json:"id"`
	Name               string       `json:"name"`
	Slug               string       `json:"slug"`
	Description        string       `json:"description,omitempty"`
	Status             string       `json:"status"`
	CreatedAt          string       `json:"created_at"`
	UpdatedAt          string       `json:"updated_at"`
	ItemCount          int          `json:"item_count"`
	ParentID           *int64       `json:"parent_id,omitempty"`
	Snippet            string       `json:"snippet,omitempty"`
	SnippetGeneratedAt string       `json:"snippet_generated_at,omitempty"`
	LastItemAt         string       `json:"last_item_at,omitempty"`
	Children           []sitOutJSON `json:"children,omitempty"`
}

func situationToJSON(s store.SituationRow) sitOutJSON {
	out := sitOutJSON{
		ID: s.ID, Name: s.Name, Slug: s.Slug, Description: s.Description,
		Status: s.Status, CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt,
		ItemCount: s.ItemCount, ParentID: s.ParentID,
		Snippet: s.Snippet, SnippetGeneratedAt: s.SnippetGeneratedAt,
		LastItemAt: s.LastItemAt,
	}
	for _, c := range s.Children {
		out.Children = append(out.Children, situationToJSON(c))
	}
	return out
}

func handleBriefItem(db *sql.DB, rc ReaderConfig) http.HandlerFunc {
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

		// Return cached brief if available
		if item.BriefText != "" {
			entities, _ := store.GetItemEntities(ctx, db, id)
			situations, _ := store.GetItemSituations(ctx, db, id)
			entJSON := make([]entityJSON, 0, len(entities))
			for _, e := range entities {
				entJSON = append(entJSON, entityJSON{Name: e.Name, Kind: e.Kind, ItemCount: e.ItemCount})
			}
			sitJSON := make([]situationJSON, 0, len(situations))
			for _, s := range situations {
				sitJSON = append(sitJSON, situationJSON{Name: s.Name, Slug: s.Slug, Status: s.Status, ItemCount: s.ItemCount})
			}
			pivotTitle := item.TitleTranslated
			if pivotTitle == "" {
				pivotTitle = item.Title
			}
			out := map[string]any{
				"item_id":    id,
				"title":      pivotTitle,
				"summary":    item.BriefText,
				"cached":     true,
				"cached_at":  item.BriefAt,
				"entities":   entJSON,
				"situations": sitJSON,
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_ = json.NewEncoder(w).Encode(out)
			return
		}

		// Find related items via entity overlap
		related, err := store.FindRelatedItems(ctx, db, id, 2, 20)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Load related item details
		relIDs := make([]int64, len(related))
		for i, m := range related {
			relIDs[i] = m.ItemID
		}
		relItems, err := store.BatchGetItems(ctx, db, relIDs)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Get entities for this item
		entities, _ := store.GetItemEntities(ctx, db, id)
		// Get situations for this item
		situations, _ := store.GetItemSituations(ctx, db, id)

		// Build Ollama input — prefer full content over truncated summary
		pivotTitle := item.TitleTranslated
		if pivotTitle == "" {
			pivotTitle = item.Title
		}
		pivotSummary := item.ContentTranslated
		if pivotSummary == "" {
			pivotSummary = item.ContentText
		}
		if pivotSummary == "" {
			pivotSummary = item.SummaryTranslated
		}
		if pivotSummary == "" {
			pivotSummary = item.Summary
		}
		pivotSource := feedName(item.FeedURL)

		// Build entity and situation context from the knowledge graph
		var entityNames []string
		for _, e := range entities {
			entityNames = append(entityNames, fmt.Sprintf("%s (%s)", e.Name, e.Kind))
		}
		var situationNames []string
		for _, s := range situations {
			situationNames = append(situationNames, fmt.Sprintf("%s [%s]", s.Name, s.Status))
		}

		ollamaRelated := make([]ollama.RelatedItem, 0, len(relItems))
		for _, ri := range relItems {
			title := ri.TitleTrans
			if title == "" {
				title = ri.Title
			}
			summary := ri.SummaryTrans
			if summary == "" {
				summary = ri.Summary
			}
			ollamaRelated = append(ollamaRelated, ollama.RelatedItem{
				Title:   title,
				Summary: summary,
				Source:  feedName(ri.FeedURL),
				Age:     timeAgo(ri.CreatedAt),
			})
		}

		// Call LLM for synthesis (90s timeout)
		var synthesis string
		ctxBrief, cancelBrief := context.WithTimeout(ctx, 90*time.Second)
		defer cancelBrief()

		briefCtx := ollama.BriefContext{
			Title:      pivotTitle,
			Content:    pivotSummary,
			Source:     pivotSource,
			Entities:   entityNames,
			Situations: situationNames,
			Related:    ollamaRelated,
		}

		if rc.OpenRouterAPIKey != "" {
			// Prefer OpenRouter for higher quality briefs
			synthesis, err = ollama.BriefViaOpenRouter(ctxBrief, rc.OpenRouterAPIKey, rc.OpenRouterBaseURL, rc.BriefModel, briefCtx)
			if err != nil {
				synthesis = fmt.Sprintf("(synthesis unavailable: %v)", err)
			}
		} else if rc.OllamaModel != "" {
			// Fall back to local Ollama
			synthesis, err = ollama.BriefOnItem(ctxBrief, rc.OllamaBaseURL, rc.OllamaModel, briefCtx)
			if err != nil {
				synthesis = fmt.Sprintf("(synthesis unavailable: %v)", err)
			}
		} else {
			synthesis = "No LLM configured for synthesis."
		}

		// Cache the result
		if synthesis != "" && !strings.HasPrefix(synthesis, "(synthesis unavailable") {
			_ = store.SetBrief(ctx, db, id, synthesis)
		}

		entJSON := make([]entityJSON, 0, len(entities))
		for _, e := range entities {
			entJSON = append(entJSON, entityJSON{Name: e.Name, Kind: e.Kind, ItemCount: e.ItemCount})
		}
		sitJSON := make([]situationJSON, 0, len(situations))
		for _, s := range situations {
			sitJSON = append(sitJSON, situationJSON{Name: s.Name, Slug: s.Slug, Status: s.Status, ItemCount: s.ItemCount})
		}

		out := map[string]any{
			"item_id":       id,
			"title":         pivotTitle,
			"summary":       synthesis,
			"related_count": len(relItems),
			"entities":      entJSON,
			"situations":    sitJSON,
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(out)
	}
}

func handleSituationsJSON(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		status := r.URL.Query().Get("status")
		limit := 20
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
				limit = n
			}
		}

		tree := r.URL.Query().Get("tree") == "true"
		order := r.URL.Query().Get("order")
		var sits []store.SituationRow
		var err error
		switch {
		case tree:
			sits, err = store.ListSituationsTree(ctx, db, status, limit)
		case order == "activity":
			sits, err = store.ListSituationsByActivity(ctx, db, status, limit)
		default:
			sits, err = store.ListSituations(ctx, db, status, limit)
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		out := make([]sitOutJSON, 0, len(sits))
		for _, s := range sits {
			out = append(out, situationToJSON(s))
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(out)
	}
}

func handleSituationDetail(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		slug := r.PathValue("slug")
		ctx := r.Context()

		sit, err := store.GetSituation(ctx, db, slug)
		if err != nil {
			http.NotFound(w, r)
			return
		}

		limit := 50
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
				limit = n
			}
		}

		items, err := store.ListSituationItems(ctx, db, sit.ID, limit)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		itemsJSON := make([]itemJSON, 0, len(items))
		for _, it := range items {
			j := itemJSON{
				ID: it.ID, CreatedAt: it.CreatedAt, SourceKind: it.SourceKind,
				Title: it.Title, Summary: it.Summary, URL: it.URL, FeedURL: it.FeedURL,
				Urgency: it.Urgency, Lang: it.Lang, TitleTrans: it.TitleTrans,
				SummaryTrans: it.SummaryTrans, ClusterKey: it.ClusterKey,
			}
			if strings.TrimSpace(it.TagsJSON) != "" {
				j.Tags = json.RawMessage(it.TagsJSON)
			}
			itemsJSON = append(itemsJSON, j)
		}

		// Load children
		children, _ := store.ListChildSituations(ctx, db, sit.ID)
		var childJSON []sitOutJSON
		for _, c := range children {
			childJSON = append(childJSON, situationToJSON(c))
		}

		out := map[string]any{
			"situation": map[string]any{
				"id":          sit.ID,
				"name":        sit.Name,
				"slug":        sit.Slug,
				"description": sit.Description,
				"status":      sit.Status,
				"created_at":  sit.CreatedAt,
				"updated_at":  sit.UpdatedAt,
				"item_count":  sit.ItemCount,
				"parent_id":   sit.ParentID,
				"children":    childJSON,
			},
			"items": itemsJSON,
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(out)
	}
}

func handleEntityDetail(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		ctx := r.Context()

		entity, err := store.GetEntityByName(ctx, db, name)
		if err == sql.ErrNoRows {
			// Try searching
			results, err := store.SearchEntities(ctx, db, name, 5)
			if err != nil || len(results) == 0 {
				http.NotFound(w, r)
				return
			}
			entity = results[0]
		} else if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		limit := 30
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
				limit = n
			}
		}

		items, err := store.ListEntityItems(ctx, db, entity.ID, limit)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		itemsJSON := make([]itemJSON, 0, len(items))
		for _, it := range items {
			j := itemJSON{
				ID: it.ID, CreatedAt: it.CreatedAt, SourceKind: it.SourceKind,
				Title: it.Title, Summary: it.Summary, URL: it.URL, FeedURL: it.FeedURL,
				Urgency: it.Urgency, Lang: it.Lang, TitleTrans: it.TitleTrans,
				SummaryTrans: it.SummaryTrans, ClusterKey: it.ClusterKey,
			}
			if strings.TrimSpace(it.TagsJSON) != "" {
				j.Tags = json.RawMessage(it.TagsJSON)
			}
			itemsJSON = append(itemsJSON, j)
		}

		out := map[string]any{
			"entity": map[string]any{
				"id":         entity.ID,
				"name":       entity.Name,
				"kind":       entity.Kind,
				"first_seen": entity.FirstSeen,
				"last_seen":  entity.LastSeen,
				"item_count": entity.ItemCount,
			},
			"items": itemsJSON,
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(out)
	}
}

func timeAgo(createdAt string) string {
	t, err := time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return "unknown"
	}
	d := time.Since(t)
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
