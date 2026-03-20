package store

import (
	"context"
	"testing"
	"time"

	"situationmonitor/internal/db"
)

func TestListItems_filterHours(t *testing.T) {
	sqlDB, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	ctx := context.Background()
	old := time.Now().UTC().Add(-100 * time.Hour).Format(time.RFC3339)
	new := time.Now().UTC().Format(time.RFC3339)

	_, err = sqlDB.ExecContext(ctx, `
INSERT INTO items (created_at, source_kind, external_id, title, urgency, tags_json)
VALUES (?, 'rss', 'g:1', 'old', 2, '[]'),
       (?, 'rss', 'g:2', 'new', 4, '[]')
`, old, new)
	if err != nil {
		t.Fatal(err)
	}

	rows, err := ListItems(ctx, sqlDB, ItemFilter{Hours: 48, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("want 1 row, got %d", len(rows))
	}
	if rows[0].Title != "new" {
		t.Fatalf("got %q", rows[0].Title)
	}
}
