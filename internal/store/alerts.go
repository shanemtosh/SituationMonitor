package store

import (
	"context"
	"database/sql"
	"time"
)

// SweepAlertCandidate is a sweep item eligible for push notification.
type SweepAlertCandidate struct {
	ID      int64
	Title   string
	Summary string
	URL     string
	Urgency int
}

// AlertCountSince returns how many alerts were sent on a channel since t.
func AlertCountSince(ctx context.Context, db *sql.DB, channel string, since time.Time) (int, error) {
	s := since.UTC().Format(time.RFC3339)
	var n int
	err := db.QueryRowContext(ctx, `
SELECT COUNT(*) FROM alert_log WHERE channel = ? AND datetime(sent_at) >= datetime(?)
`, channel, s).Scan(&n)
	return n, err
}

// InsertAlertLog records a sent notification.
func InsertAlertLog(ctx context.Context, db *sql.DB, channel string, itemID int64, digest string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.ExecContext(ctx, `
INSERT INTO alert_log (sent_at, channel, item_id, digest) VALUES (?, ?, ?, ?)
`, now, channel, itemID, digest)
	return err
}

// MarkItemAlerted sets alert_sent_at on an item.
func MarkItemAlerted(ctx context.Context, db *sql.DB, itemID int64) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.ExecContext(ctx, `UPDATE items SET alert_sent_at = ? WHERE id = ?`, now, itemID)
	return err
}

// ListSweepItemsNeedingAlert returns high-urgency sweep items for this sweep that were not alerted.
func ListSweepItemsNeedingAlert(ctx context.Context, db *sql.DB, sweepID int64, minUrgency int) ([]SweepAlertCandidate, error) {
	rows, err := db.QueryContext(ctx, `
SELECT id, title, COALESCE(summary,''), COALESCE(url,''), urgency
FROM items
WHERE sweep_id = ?
  AND urgency >= ?
  AND (alert_sent_at IS NULL OR alert_sent_at = '')
ORDER BY urgency DESC, id ASC
`, sweepID, minUrgency)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SweepAlertCandidate
	for rows.Next() {
		var r SweepAlertCandidate
		if err := rows.Scan(&r.ID, &r.Title, &r.Summary, &r.URL, &r.Urgency); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
