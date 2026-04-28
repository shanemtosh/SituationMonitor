package store

import (
	"context"
	"database/sql"
	"time"
)

type DataStreamRow struct {
	ID            int64
	ConstraintID  int64
	Name          string
	Description   string
	SourceType    string
	SourceConfig  string // JSON
	LastValue     *string
	LastCheckedAt *string
	ThresholdNote string
	CreatedAt     string
}

func CreateDataStream(ctx context.Context, db *sql.DB, ds DataStreamRow) (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := db.ExecContext(ctx, `
INSERT INTO data_streams (constraint_id, name, description, source_type, source_config, last_value, last_checked_at, threshold_note, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
`, ds.ConstraintID, ds.Name, ds.Description, ds.SourceType, ds.SourceConfig, ds.LastValue, ds.LastCheckedAt, ds.ThresholdNote, now)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func UpdateDataStream(ctx context.Context, db *sql.DB, ds DataStreamRow) error {
	_, err := db.ExecContext(ctx, `
UPDATE data_streams SET constraint_id=?, name=?, description=?, source_type=?, source_config=?, last_value=?, last_checked_at=?, threshold_note=?
WHERE id=?
`, ds.ConstraintID, ds.Name, ds.Description, ds.SourceType, ds.SourceConfig, ds.LastValue, ds.LastCheckedAt, ds.ThresholdNote, ds.ID)
	return err
}

func GetDataStream(ctx context.Context, db *sql.DB, id int64) (DataStreamRow, error) {
	var ds DataStreamRow
	var lastVal, lastChecked sql.NullString
	err := db.QueryRowContext(ctx, `
SELECT id, constraint_id, name, description, source_type, source_config, last_value, last_checked_at, threshold_note, created_at
FROM data_streams WHERE id=?
`, id).Scan(&ds.ID, &ds.ConstraintID, &ds.Name, &ds.Description, &ds.SourceType, &ds.SourceConfig, &lastVal, &lastChecked, &ds.ThresholdNote, &ds.CreatedAt)
	if lastVal.Valid {
		ds.LastValue = &lastVal.String
	}
	if lastChecked.Valid {
		ds.LastCheckedAt = &lastChecked.String
	}
	return ds, err
}

func DeleteDataStream(ctx context.Context, db *sql.DB, id int64) error {
	_, err := db.ExecContext(ctx, `DELETE FROM data_streams WHERE id=?`, id)
	return err
}

func ListDataStreams(ctx context.Context, db *sql.DB, constraintID *int64) ([]DataStreamRow, error) {
	where := "1=1"
	var args []any
	if constraintID != nil {
		where += " AND constraint_id=?"
		args = append(args, *constraintID)
	}

	rows, err := db.QueryContext(ctx, `
SELECT id, constraint_id, name, description, source_type, source_config, last_value, last_checked_at, threshold_note, created_at
FROM data_streams WHERE `+where+` ORDER BY name ASC
`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []DataStreamRow
	for rows.Next() {
		var ds DataStreamRow
		var lastVal, lastChecked sql.NullString
		if err := rows.Scan(&ds.ID, &ds.ConstraintID, &ds.Name, &ds.Description, &ds.SourceType, &ds.SourceConfig, &lastVal, &lastChecked, &ds.ThresholdNote, &ds.CreatedAt); err != nil {
			return nil, err
		}
		if lastVal.Valid {
			ds.LastValue = &lastVal.String
		}
		if lastChecked.Valid {
			ds.LastCheckedAt = &lastChecked.String
		}
		out = append(out, ds)
	}
	return out, rows.Err()
}
