package extract

import (
	"context"
	"database/sql"
	"log"
	"strings"
	"time"

	"situationmonitor/internal/ollama"
	"situationmonitor/internal/store"
)

// LoopConfig drives the entity extraction and situation tracking worker.
type LoopConfig struct {
	OllamaBaseURL     string
	Model             string
	PollInterval      time.Duration
	BatchSize         int
	OnStart           bool
	MinClusterOverlap int // minimum entity overlap to cluster (default 2)
	SituationMinItems int // items needed to auto-create a situation (default 4)
}

// RunLoop processes items for entity extraction, clustering, and situation tracking.
func RunLoop(ctx context.Context, db *sql.DB, cfg LoopConfig) {
	if cfg.PollInterval <= 0 || strings.TrimSpace(cfg.Model) == "" {
		return
	}
	batch := cfg.BatchSize
	if batch <= 0 {
		batch = 10
	}
	minOverlap := cfg.MinClusterOverlap
	if minOverlap <= 0 {
		minOverlap = 2
	}
	sitMin := cfg.SituationMinItems
	if sitMin <= 0 {
		sitMin = 4
	}

	run := func() {
		ctxRun, cancel := context.WithTimeout(ctx, 10*time.Minute)
		defer cancel()

		// Pass 1: Entity extraction
		extracted := pass1NER(ctxRun, db, cfg.OllamaBaseURL, cfg.Model, batch)

		// Pass 2: Cluster rewrite for newly extracted items
		if len(extracted) > 0 {
			pass2Cluster(ctxRun, db, extracted, minOverlap)
		}

		// Pass 3: Auto-create situations from unlinked clusters
		pass3Situations(ctxRun, db, cfg.OllamaBaseURL, cfg.Model, sitMin)

		// Pass 4: Entity normalization and situation hierarchy
		if merges := store.RunEntityNormalization(ctxRun, db); merges > 0 {
			log.Printf("extract: normalized %d entities", merges)
		}
		if linked := store.AutoLinkSituationHierarchy(ctxRun, db, 0.6); linked > 0 {
			log.Printf("extract: linked %d sub-situations", linked)
		}
	}

	if cfg.OnStart {
		run()
	}
	t := time.NewTicker(cfg.PollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			run()
		}
	}
}

// pass1NER extracts entities from untranslated items. Returns IDs of processed items.
func pass1NER(ctx context.Context, db *sql.DB, baseURL, model string, batch int) []int64 {
	rows, err := store.ListUnextracted(ctx, db, batch)
	if err != nil {
		log.Printf("extract: list: %v", err)
		return nil
	}
	if len(rows) == 0 {
		return nil
	}

	var processed []int64
	for i, r := range rows {
		if ctx.Err() != nil {
			break
		}
		// Use translated title/summary if available, fall back to original
		title := r.Title
		summary := r.Summary

		entities, relevance, err := ollama.ExtractEntities(ctx, baseURL, model, title, summary)
		if err != nil {
			log.Printf("extract: item %d (%d/%d): %v", r.ID, i+1, len(rows), err)
			// Mark extracted anyway to avoid infinite retry
			_ = store.MarkExtracted(ctx, db, r.ID)
			continue
		}

		// Low-relevance items: set urgency to 0 so they're hidden from default feed
		if relevance == "low" {
			log.Printf("extract: item %d (%d/%d): low relevance, hiding", r.ID, i+1, len(rows))
			_ = store.SetUrgency(ctx, db, r.ID, 0)
			_ = store.MarkExtracted(ctx, db, r.ID)
			processed = append(processed, r.ID)
			continue
		}

		if len(entities) == 0 {
			log.Printf("extract: item %d (%d/%d): no entities found", r.ID, i+1, len(rows))
			_ = store.MarkExtracted(ctx, db, r.ID)
			processed = append(processed, r.ID)
			continue
		}

		entityIDs := make([]int64, 0, len(entities))
		for _, e := range entities {
			eid, err := store.UpsertEntity(ctx, db, e.Name, e.Kind)
			if err != nil {
				log.Printf("extract: upsert entity %q: %v", e.Name, err)
				continue
			}
			entityIDs = append(entityIDs, eid)
		}

		if err := store.SetItemEntities(ctx, db, r.ID, entityIDs); err != nil {
			log.Printf("extract: link entities item %d: %v", r.ID, err)
		}
		if err := store.MarkExtracted(ctx, db, r.ID); err != nil {
			log.Printf("extract: mark %d: %v", r.ID, err)
		}

		log.Printf("extract: item %d (%d/%d): %d entities", r.ID, i+1, len(rows), len(entities))
		processed = append(processed, r.ID)
	}
	if len(processed) > 0 {
		log.Printf("extract: NER processed %d items", len(processed))
	}
	return processed
}

// pass2Cluster updates cluster_key for recently extracted items based on entity overlap.
func pass2Cluster(ctx context.Context, db *sql.DB, itemIDs []int64, minOverlap int) {
	for _, id := range itemIDs {
		if ctx.Err() != nil {
			return
		}
		related, err := store.FindRelatedItems(ctx, db, id, minOverlap, 50)
		if err != nil {
			log.Printf("extract: cluster %d: %v", id, err)
			continue
		}
		if err := store.SetClusterFromEntities(ctx, db, id, related); err != nil {
			log.Printf("extract: set cluster %d: %v", id, err)
		}
	}
	log.Printf("extract: clustered %d items", len(itemIDs))
}

// pass3Situations auto-creates situations from clusters with enough items.
func pass3Situations(ctx context.Context, db *sql.DB, baseURL, model string, minItems int) {
	clusters, err := store.FindUnlinkedClusters(ctx, db, minItems)
	if err != nil {
		log.Printf("extract: find clusters: %v", err)
		return
	}
	if len(clusters) == 0 {
		return
	}

	for _, c := range clusters {
		if ctx.Err() != nil {
			return
		}
		items, err := store.ListClusterItems(ctx, db, c.ClusterKey, 10)
		if err != nil {
			log.Printf("extract: list cluster %s: %v", c.ClusterKey, err)
			continue
		}
		if len(items) == 0 {
			continue
		}

		// Collect titles for naming
		titles := make([]string, 0, len(items))
		for _, it := range items {
			titles = append(titles, it.Title)
		}

		name, err := ollama.NameSituation(ctx, baseURL, model, titles)
		if err != nil {
			log.Printf("extract: name situation: %v", err)
			continue
		}

		sitID, err := store.UpsertSituation(ctx, db, name, "")
		if err != nil {
			log.Printf("extract: create situation %q: %v", name, err)
			continue
		}

		// Link all items in the cluster
		allItems, _ := store.ListClusterItems(ctx, db, c.ClusterKey, 200)
		for _, it := range allItems {
			if err := store.LinkSituationItem(ctx, db, sitID, it.ID); err != nil {
				log.Printf("extract: link item %d to situation %d: %v", it.ID, sitID, err)
			}
		}
		log.Printf("extract: created situation %q (%d items)", name, len(allItems))
	}
}
