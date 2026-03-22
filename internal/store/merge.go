package store

import (
	"context"
	"database/sql"
	"log"
	"strings"
)

// MergeEntities merges entity `fromID` into `toID`. All item_entities links
// are redirected, canonical_id is set, and counts are updated.
func MergeEntities(ctx context.Context, db *sql.DB, fromID, toID int64) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Move item_entities links from fromID to toID (skip duplicates)
	_, err = tx.ExecContext(ctx, `
INSERT OR IGNORE INTO item_entities (item_id, entity_id)
SELECT item_id, ? FROM item_entities WHERE entity_id = ?
`, toID, fromID)
	if err != nil {
		return err
	}

	// Remove old links
	_, err = tx.ExecContext(ctx, `DELETE FROM item_entities WHERE entity_id = ?`, fromID)
	if err != nil {
		return err
	}

	// Set canonical_id on the merged entity
	_, err = tx.ExecContext(ctx, `UPDATE entities SET canonical_id = ? WHERE id = ?`, toID, fromID)
	if err != nil {
		return err
	}

	// Update item_count on the canonical entity
	_, err = tx.ExecContext(ctx, `
UPDATE entities SET item_count = (
	SELECT COUNT(*) FROM item_entities WHERE entity_id = ?
) WHERE id = ?
`, toID, toID)
	if err != nil {
		return err
	}

	// Update first_seen/last_seen on canonical
	_, err = tx.ExecContext(ctx, `
UPDATE entities SET
	first_seen = (SELECT MIN(first_seen) FROM entities WHERE id IN (?, ?)),
	last_seen  = (SELECT MAX(last_seen)  FROM entities WHERE id IN (?, ?))
WHERE id = ?
`, fromID, toID, fromID, toID, toID)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// ResolveCanonicalID returns the canonical entity ID, following the alias chain.
func ResolveCanonicalID(ctx context.Context, db *sql.DB, entityID int64) (int64, error) {
	for i := 0; i < 10; i++ { // max depth to prevent loops
		var canonID sql.NullInt64
		err := db.QueryRowContext(ctx, `SELECT canonical_id FROM entities WHERE id = ?`, entityID).Scan(&canonID)
		if err != nil {
			return entityID, err
		}
		if !canonID.Valid {
			return entityID, nil
		}
		entityID = canonID.Int64
	}
	return entityID, nil
}

// countryNames maps country names that should always be PLACE kind.
var countryNames = map[string]bool{
	"iran": true, "israel": true, "china": true, "japan": true,
	"russia": true, "india": true, "taiwan": true, "ukraine": true,
	"cuba": true, "vietnam": true, "qatar": true, "bahrain": true,
	"kuwait": true, "oman": true, "iraq": true, "syria": true,
	"lebanon": true, "jordan": true, "turkey": true, "egypt": true,
	"saudi arabia": true, "south korea": true, "north korea": true,
	"australia": true, "pakistan": true, "afghanistan": true,
	"philippines": true, "indonesia": true, "thailand": true,
	"singapore": true, "malaysia": true, "myanmar": true,
	"bangladesh": true, "sri lanka": true, "nepal": true,
	"south africa": true, "nigeria": true, "kenya": true,
	"sudan": true, "ethiopia": true, "somalia": true,
	"france": true, "germany": true, "italy": true, "spain": true,
	"uk": true, "canada": true, "mexico": true, "brazil": true,
	"argentina": true, "colombia": true, "venezuela": true,
	"chile": true, "peru": true, "ecuador": true, "bolivia": true,
	"ghana": true, "uganda": true, "slovenia": true, "hungary": true,
	"poland": true, "greece": true, "cyprus": true,
}

// nameVariants maps common short forms to their canonical form.
var nameVariants = map[string]string{
	"us":                       "United States",
	"u.s.":                     "United States",
	"u.s":                      "United States",
	"usa":                      "United States",
	"the united states":        "United States",
	"united states of america": "United States",
	"the us":                   "United States",
	"trump":                    "Donald Trump",
	"president trump":          "Donald Trump",
	"us president donald trump": "Donald Trump",
	"president donald trump":   "Donald Trump",
	"netanyahu":                "Benjamin Netanyahu",
	"prime minister netanyahu": "Benjamin Netanyahu",
	"pm netanyahu":             "Benjamin Netanyahu",
	"benjamin netanyahu":       "Benjamin Netanyahu",
	"xi":                       "Xi Jinping",
	"president xi":             "Xi Jinping",
	"xi jinping":               "Xi Jinping",
	"putin":                    "Vladimir Putin",
	"president putin":          "Vladimir Putin",
	"vladimir putin":           "Vladimir Putin",
	"mueller":                  "Robert Mueller",
	"robert mueller":           "Robert Mueller",
	"chair powell":             "Jerome Powell",
	"chair jerome powell":      "Jerome Powell",
	"jerome powell":            "Jerome Powell",
	"fed":                      "Federal Reserve",
	"the fed":                  "Federal Reserve",
	"us federal reserve":       "Federal Reserve",
	"the federal reserve":      "Federal Reserve",
	"boj":                      "Bank of Japan",
	"bok":                      "Bank of Korea",
	"ecb":                      "European Central Bank",
	"iaea":                     "IAEA",
	"who":                      "WHO",
	"nato":                     "NATO",
	"eu":                       "European Union",
	"irgc":                     "IRGC",
	"iran's irgc":              "IRGC",
	"islamic revolutionary guard corps":  "IRGC",
	"iran revolutionary guard corps":     "IRGC",
	"iran's revolutionary guards":        "IRGC",
	"iranian government":       "Iran",
	"trump administration":     "Trump Administration",
}

// RunEntityNormalization finds and merges duplicate entities.
// Returns the number of merges performed.
func RunEntityNormalization(ctx context.Context, db *sql.DB) int {
	merges := 0

	// Phase 1: Fix country kinds — merge ORG/TOPIC into PLACE
	for country := range countryNames {
		// Find PLACE version (or create it from highest-count version)
		rows, err := db.QueryContext(ctx, `
SELECT id, kind, item_count FROM entities
WHERE LOWER(name) = ? AND canonical_id IS NULL
ORDER BY
	CASE kind WHEN 'PLACE' THEN 0 ELSE 1 END,
	item_count DESC
`, country)
		if err != nil {
			continue
		}
		var entities []struct {
			ID    int64
			Kind  string
			Count int
		}
		for rows.Next() {
			var e struct {
				ID    int64
				Kind  string
				Count int
			}
			rows.Scan(&e.ID, &e.Kind, &e.Count)
			entities = append(entities, e)
		}
		rows.Close()

		if len(entities) <= 1 {
			continue
		}

		// Keep the first (PLACE preferred, then highest count)
		canonical := entities[0]
		// If canonical isn't PLACE, update its kind
		if canonical.Kind != "PLACE" {
			db.ExecContext(ctx, `UPDATE entities SET kind = 'PLACE' WHERE id = ?`, canonical.ID)
		}

		for _, e := range entities[1:] {
			if err := MergeEntities(ctx, db, e.ID, canonical.ID); err != nil {
				log.Printf("merge: country %s: %v", country, err)
				continue
			}
			merges++
		}
	}

	// Phase 2: Merge name variants
	for variant, canonical := range nameVariants {
		// Find the variant entity
		var fromID int64
		err := db.QueryRowContext(ctx, `
SELECT id FROM entities WHERE LOWER(name) = ? AND canonical_id IS NULL
`, strings.ToLower(variant)).Scan(&fromID)
		if err != nil {
			continue
		}

		// Find or determine the canonical entity
		var toID int64
		err = db.QueryRowContext(ctx, `
SELECT id FROM entities WHERE name = ? AND canonical_id IS NULL
ORDER BY item_count DESC LIMIT 1
`, canonical).Scan(&toID)
		if err != nil {
			// Canonical doesn't exist — just rename
			db.ExecContext(ctx, `UPDATE entities SET name = ? WHERE id = ?`, canonical, fromID)
			continue
		}

		if fromID == toID {
			continue
		}

		if err := MergeEntities(ctx, db, fromID, toID); err != nil {
			log.Printf("merge: %s -> %s: %v", variant, canonical, err)
			continue
		}
		merges++
	}

	// Phase 3: Merge same-name different-kind (keep highest count)
	// Collect all duplicates first, then close cursor before merging
	// (SQLite single-connection can't merge while cursor is open)
	type dupPair struct {
		id1, id2           int64
		kind1, kind2, name string
		count1, count2     int
	}
	var dups []dupPair
	dupRows, err := db.QueryContext(ctx, `
SELECT e1.id, e1.kind, e1.item_count, e2.id, e2.kind, e2.item_count, e1.name
FROM entities e1
JOIN entities e2 ON e1.name = e2.name AND e1.id < e2.id
WHERE e1.canonical_id IS NULL AND e2.canonical_id IS NULL
ORDER BY e1.item_count + e2.item_count DESC
`)
	if err == nil {
		for dupRows.Next() {
			var d dupPair
			dupRows.Scan(&d.id1, &d.kind1, &d.count1, &d.id2, &d.kind2, &d.count2, &d.name)
			dups = append(dups, d)
		}
		dupRows.Close()
	}
	for _, d := range dups {
		canonID, mergeID := d.id1, d.id2
		if d.count2 > d.count1 {
			canonID, mergeID = d.id2, d.id1
		}
		if countryNames[strings.ToLower(d.name)] {
			if d.kind1 == "PLACE" {
				canonID, mergeID = d.id1, d.id2
			} else if d.kind2 == "PLACE" {
				canonID, mergeID = d.id2, d.id1
			}
		}
		if err := MergeEntities(ctx, db, mergeID, canonID); err != nil {
			log.Printf("merge: dup %s: %v", d.name, err)
			continue
		}
		merges++
	}

	if merges > 0 {
		log.Printf("entity-normalize: %d merges", merges)
	}
	return merges
}
