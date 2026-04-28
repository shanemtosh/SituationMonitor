package store

import (
	"context"
	"database/sql"
)

type DomainSummaryRow struct {
	Domain            string
	ActiveAssessments int
	ActiveConstraints int
	UpcomingEvents    int
	LastUpdated       string
}

func ListDomainSummaries(ctx context.Context, db *sql.DB) ([]DomainSummaryRow, error) {
	rows, err := db.QueryContext(ctx, `
SELECT
	d.domain,
	(SELECT COUNT(*) FROM net_assessments WHERE domain=d.domain AND status='active') as active_assessments,
	(SELECT COUNT(*) FROM constraints WHERE domain=d.domain AND status='active') as active_constraints,
	(SELECT COUNT(*) FROM alpha_calendar WHERE domain=d.domain AND status='upcoming' AND event_date >= date('now')) as upcoming_events,
	COALESCE((SELECT MAX(updated_at) FROM net_assessments WHERE domain=d.domain), '') as last_updated
FROM (
	SELECT DISTINCT domain FROM net_assessments
	UNION
	SELECT DISTINCT domain FROM constraints
	UNION
	SELECT DISTINCT domain FROM alpha_calendar
) d
ORDER BY active_assessments DESC
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []DomainSummaryRow
	for rows.Next() {
		var s DomainSummaryRow
		if err := rows.Scan(&s.Domain, &s.ActiveAssessments, &s.ActiveConstraints, &s.UpcomingEvents, &s.LastUpdated); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
