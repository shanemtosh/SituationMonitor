package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type AssessmentRow struct {
	ID                    int64
	SituationID           int64
	Domain                string
	Lens                  string
	Title                 string
	Summary               string
	PriorProbability      *float64
	CurrentProbability    *float64
	FulcrumConstraintID   *int64
	BaseCase              string
	BullCase              string
	BearCase              string
	InvestmentImplications string
	Status                string
	CreatedAt             string
	UpdatedAt             string
}

type AssessmentConstraintRow struct {
	AssessmentID int64
	ConstraintID int64
	Weight       string
	Notes        string
}

type ProbabilityUpdateRow struct {
	ID            int64
	AssessmentID  int64
	Prior         float64
	Posterior     float64
	Evidence      string
	SourceItemID  *int64
	ConstraintID  *int64
	CreatedAt     string
}

func CreateAssessment(ctx context.Context, db *sql.DB, a AssessmentRow) (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	if a.Domain == "" {
		a.Domain = "geopolitics"
	}
	res, err := db.ExecContext(ctx, `
INSERT INTO net_assessments (situation_id, domain, lens, title, summary, prior_probability, current_probability, fulcrum_constraint_id, base_case, bull_case, bear_case, investment_implications, status, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, a.SituationID, a.Domain, a.Lens, a.Title, a.Summary, a.PriorProbability, a.CurrentProbability, a.FulcrumConstraintID, a.BaseCase, a.BullCase, a.BearCase, a.InvestmentImplications, a.Status, now, now)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func UpdateAssessment(ctx context.Context, db *sql.DB, a AssessmentRow) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.ExecContext(ctx, `
UPDATE net_assessments SET situation_id=?, domain=?, lens=?, title=?, summary=?, prior_probability=?, current_probability=?, fulcrum_constraint_id=?, base_case=?, bull_case=?, bear_case=?, investment_implications=?, status=?, updated_at=?
WHERE id=?
`, a.SituationID, a.Domain, a.Lens, a.Title, a.Summary, a.PriorProbability, a.CurrentProbability, a.FulcrumConstraintID, a.BaseCase, a.BullCase, a.BearCase, a.InvestmentImplications, a.Status, now, a.ID)
	return err
}

func GetAssessment(ctx context.Context, db *sql.DB, id int64) (AssessmentRow, error) {
	var a AssessmentRow
	var prior, current sql.NullFloat64
	var fulcrumID sql.NullInt64
	err := db.QueryRowContext(ctx, `
SELECT id, situation_id, domain, lens, title, summary, prior_probability, current_probability, fulcrum_constraint_id, base_case, bull_case, bear_case, investment_implications, status, created_at, updated_at
FROM net_assessments WHERE id=?
`, id).Scan(&a.ID, &a.SituationID, &a.Domain, &a.Lens, &a.Title, &a.Summary, &prior, &current, &fulcrumID, &a.BaseCase, &a.BullCase, &a.BearCase, &a.InvestmentImplications, &a.Status, &a.CreatedAt, &a.UpdatedAt)
	if prior.Valid {
		a.PriorProbability = &prior.Float64
	}
	if current.Valid {
		a.CurrentProbability = &current.Float64
	}
	if fulcrumID.Valid {
		fid := fulcrumID.Int64
		a.FulcrumConstraintID = &fid
	}
	return a, err
}

func DeleteAssessment(ctx context.Context, db *sql.DB, id int64) error {
	_, err := db.ExecContext(ctx, `DELETE FROM net_assessments WHERE id=?`, id)
	return err
}

func ListAssessments(ctx context.Context, db *sql.DB, situationID *int64, domain, lens, status string) ([]AssessmentRow, error) {
	where := "1=1"
	var args []any
	if situationID != nil {
		where += " AND situation_id=?"
		args = append(args, *situationID)
	}
	if domain != "" {
		where += " AND domain=?"
		args = append(args, domain)
	}
	if lens != "" {
		where += " AND lens=?"
		args = append(args, lens)
	}
	if status != "" {
		where += " AND status=?"
		args = append(args, status)
	}

	rows, err := db.QueryContext(ctx, fmt.Sprintf(`
SELECT id, situation_id, domain, lens, title, summary, prior_probability, current_probability, fulcrum_constraint_id, base_case, bull_case, bear_case, investment_implications, status, created_at, updated_at
FROM net_assessments WHERE %s ORDER BY datetime(updated_at) DESC
`, where), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AssessmentRow
	for rows.Next() {
		var a AssessmentRow
		var prior, current sql.NullFloat64
		var fulcrumID sql.NullInt64
		if err := rows.Scan(&a.ID, &a.SituationID, &a.Domain, &a.Lens, &a.Title, &a.Summary, &prior, &current, &fulcrumID, &a.BaseCase, &a.BullCase, &a.BearCase, &a.InvestmentImplications, &a.Status, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, err
		}
		if prior.Valid {
			a.PriorProbability = &prior.Float64
		}
		if current.Valid {
			a.CurrentProbability = &current.Float64
		}
		if fulcrumID.Valid {
			fid := fulcrumID.Int64
			a.FulcrumConstraintID = &fid
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// LinkAssessmentConstraint links a constraint to an assessment with weight.
func LinkAssessmentConstraint(ctx context.Context, db *sql.DB, assessmentID, constraintID int64, weight, notes string) error {
	_, err := db.ExecContext(ctx, `
INSERT OR REPLACE INTO assessment_constraints (assessment_id, constraint_id, weight, notes)
VALUES (?, ?, ?, ?)
`, assessmentID, constraintID, weight, notes)
	return err
}

// UnlinkAssessmentConstraint removes a constraint from an assessment.
func UnlinkAssessmentConstraint(ctx context.Context, db *sql.DB, assessmentID, constraintID int64) error {
	_, err := db.ExecContext(ctx, `DELETE FROM assessment_constraints WHERE assessment_id=? AND constraint_id=?`, assessmentID, constraintID)
	return err
}

// GetAssessmentConstraints returns constraints linked to an assessment.
func GetAssessmentConstraints(ctx context.Context, db *sql.DB, assessmentID int64) ([]AssessmentConstraintRow, error) {
	rows, err := db.QueryContext(ctx, `
SELECT assessment_id, constraint_id, weight, notes FROM assessment_constraints WHERE assessment_id=?
`, assessmentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AssessmentConstraintRow
	for rows.Next() {
		var ac AssessmentConstraintRow
		if err := rows.Scan(&ac.AssessmentID, &ac.ConstraintID, &ac.Weight, &ac.Notes); err != nil {
			return nil, err
		}
		out = append(out, ac)
	}
	return out, rows.Err()
}

// CreateProbabilityUpdate logs a Bayesian update and updates the assessment's current probability.
func CreateProbabilityUpdate(ctx context.Context, db *sql.DB, u ProbabilityUpdateRow) (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx, `
INSERT INTO probability_updates (assessment_id, prior, posterior, evidence, source_item_id, constraint_id, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
`, u.AssessmentID, u.Prior, u.Posterior, u.Evidence, u.SourceItemID, u.ConstraintID, now)
	if err != nil {
		return 0, err
	}

	// Update the assessment's current probability
	_, err = tx.ExecContext(ctx, `
UPDATE net_assessments SET current_probability=?, updated_at=? WHERE id=?
`, u.Posterior, now, u.AssessmentID)
	if err != nil {
		return 0, err
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// ListProbabilityUpdates returns the update history for an assessment.
func ListProbabilityUpdates(ctx context.Context, db *sql.DB, assessmentID int64) ([]ProbabilityUpdateRow, error) {
	rows, err := db.QueryContext(ctx, `
SELECT id, assessment_id, prior, posterior, evidence, source_item_id, constraint_id, created_at
FROM probability_updates WHERE assessment_id=? ORDER BY datetime(created_at) DESC
`, assessmentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ProbabilityUpdateRow
	for rows.Next() {
		var u ProbabilityUpdateRow
		var srcID, cID sql.NullInt64
		if err := rows.Scan(&u.ID, &u.AssessmentID, &u.Prior, &u.Posterior, &u.Evidence, &srcID, &cID, &u.CreatedAt); err != nil {
			return nil, err
		}
		if srcID.Valid {
			sid := srcID.Int64
			u.SourceItemID = &sid
		}
		if cID.Valid {
			cid := cID.Int64
			u.ConstraintID = &cid
		}
		out = append(out, u)
	}
	return out, rows.Err()
}
