package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type ConstraintRow struct {
	ID           int64
	SituationID  *int64
	Domain       string
	Region       string
	Type         string
	Name         string
	Description  string
	Mutability   string
	Direction    string
	Evidence     string
	DataStreams  string // JSON array
	Status       string
	CreatedAt    string
	UpdatedAt    string
}

func CreateConstraint(ctx context.Context, db *sql.DB, c ConstraintRow) (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	if c.Domain == "" {
		c.Domain = "geopolitics"
	}
	var sitID any
	if c.SituationID != nil {
		sitID = *c.SituationID
	}
	res, err := db.ExecContext(ctx, `
INSERT INTO constraints (situation_id, domain, region, type, name, description, mutability, direction, evidence, data_streams, status, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, sitID, c.Domain, c.Region, c.Type, c.Name, c.Description, c.Mutability, c.Direction, c.Evidence, c.DataStreams, c.Status, now, now)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func UpdateConstraint(ctx context.Context, db *sql.DB, c ConstraintRow) error {
	now := time.Now().UTC().Format(time.RFC3339)
	var sitID any
	if c.SituationID != nil {
		sitID = *c.SituationID
	}
	_, err := db.ExecContext(ctx, `
UPDATE constraints SET situation_id=?, domain=?, region=?, type=?, name=?, description=?, mutability=?, direction=?, evidence=?, data_streams=?, status=?, updated_at=?
WHERE id=?
`, sitID, c.Domain, c.Region, c.Type, c.Name, c.Description, c.Mutability, c.Direction, c.Evidence, c.DataStreams, c.Status, now, c.ID)
	return err
}

func GetConstraint(ctx context.Context, db *sql.DB, id int64) (ConstraintRow, error) {
	var c ConstraintRow
	var sitID sql.NullInt64
	err := db.QueryRowContext(ctx, `
SELECT id, situation_id, domain, region, type, name, description, mutability, direction, evidence, data_streams, status, created_at, updated_at
FROM constraints WHERE id=?
`, id).Scan(&c.ID, &sitID, &c.Domain, &c.Region, &c.Type, &c.Name, &c.Description, &c.Mutability, &c.Direction, &c.Evidence, &c.DataStreams, &c.Status, &c.CreatedAt, &c.UpdatedAt)
	if sitID.Valid {
		sid := sitID.Int64
		c.SituationID = &sid
	}
	return c, err
}

func DeleteConstraint(ctx context.Context, db *sql.DB, id int64) error {
	_, err := db.ExecContext(ctx, `DELETE FROM constraints WHERE id=?`, id)
	return err
}

func ListConstraints(ctx context.Context, db *sql.DB, situationID *int64, domain, ctype, region, status string) ([]ConstraintRow, error) {
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
	if ctype != "" {
		where += " AND type=?"
		args = append(args, ctype)
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
SELECT id, situation_id, domain, region, type, name, description, mutability, direction, evidence, data_streams, status, created_at, updated_at
FROM constraints WHERE %s ORDER BY datetime(updated_at) DESC
`, where), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ConstraintRow
	for rows.Next() {
		var c ConstraintRow
		var sitID sql.NullInt64
		if err := rows.Scan(&c.ID, &sitID, &c.Domain, &c.Region, &c.Type, &c.Name, &c.Description, &c.Mutability, &c.Direction, &c.Evidence, &c.DataStreams, &c.Status, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		if sitID.Valid {
			sid := sitID.Int64
			c.SituationID = &sid
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
