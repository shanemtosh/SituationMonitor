package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type CalendarEventRow struct {
	ID               int64
	EventDate        string
	Title            string
	Description      string
	Domain           string
	Region           string
	EventType        string
	MarketRelevance  string
	AssessmentID     *int64
	Status           string
	CreatedAt        string
	UpdatedAt        string
}

func CreateCalendarEvent(ctx context.Context, db *sql.DB, e CalendarEventRow) (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	if e.Domain == "" {
		e.Domain = "geopolitics"
	}
	res, err := db.ExecContext(ctx, `
INSERT INTO alpha_calendar (event_date, title, description, domain, region, event_type, market_relevance, assessment_id, status, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, e.EventDate, e.Title, e.Description, e.Domain, e.Region, e.EventType, e.MarketRelevance, e.AssessmentID, e.Status, now, now)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func UpdateCalendarEvent(ctx context.Context, db *sql.DB, e CalendarEventRow) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.ExecContext(ctx, `
UPDATE alpha_calendar SET event_date=?, title=?, description=?, domain=?, region=?, event_type=?, market_relevance=?, assessment_id=?, status=?, updated_at=?
WHERE id=?
`, e.EventDate, e.Title, e.Description, e.Domain, e.Region, e.EventType, e.MarketRelevance, e.AssessmentID, e.Status, now, e.ID)
	return err
}

func GetCalendarEvent(ctx context.Context, db *sql.DB, id int64) (CalendarEventRow, error) {
	var e CalendarEventRow
	var aID sql.NullInt64
	err := db.QueryRowContext(ctx, `
SELECT id, event_date, title, description, domain, region, event_type, market_relevance, assessment_id, status, created_at, updated_at
FROM alpha_calendar WHERE id=?
`, id).Scan(&e.ID, &e.EventDate, &e.Title, &e.Description, &e.Domain, &e.Region, &e.EventType, &e.MarketRelevance, &aID, &e.Status, &e.CreatedAt, &e.UpdatedAt)
	if aID.Valid {
		aid := aID.Int64
		e.AssessmentID = &aid
	}
	return e, err
}

func DeleteCalendarEvent(ctx context.Context, db *sql.DB, id int64) error {
	_, err := db.ExecContext(ctx, `DELETE FROM alpha_calendar WHERE id=?`, id)
	return err
}

func ListCalendarEvents(ctx context.Context, db *sql.DB, from, to, domain, region, status string) ([]CalendarEventRow, error) {
	where := "1=1"
	var args []any
	if from != "" {
		where += " AND event_date >= ?"
		args = append(args, from)
	}
	if to != "" {
		where += " AND event_date <= ?"
		args = append(args, to)
	}
	if domain != "" {
		where += " AND domain=?"
		args = append(args, domain)
	}
	if region != "" {
		where += " AND region=?"
		args = append(args, region)
	}
	if status != "" {
		where += " AND status=?"
		args = append(args, status)
	}

	rows, err := db.QueryContext(ctx, fmt.Sprintf(`
SELECT id, event_date, title, description, domain, region, event_type, market_relevance, assessment_id, status, created_at, updated_at
FROM alpha_calendar WHERE %s ORDER BY event_date ASC
`, where), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []CalendarEventRow
	for rows.Next() {
		var e CalendarEventRow
		var aID sql.NullInt64
		if err := rows.Scan(&e.ID, &e.EventDate, &e.Title, &e.Description, &e.Domain, &e.Region, &e.EventType, &e.MarketRelevance, &aID, &e.Status, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, err
		}
		if aID.Valid {
			aid := aID.Int64
			e.AssessmentID = &aid
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
