package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// Open opens SQLite and applies migrations.
func Open(path string) (*sql.DB, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=journal_mode(wal)", path)
	d, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	d.SetMaxOpenConns(4)
	d.SetConnMaxLifetime(30 * time.Minute)
	if err := d.Ping(); err != nil {
		_ = d.Close()
		return nil, err
	}
	if err := migrate(d); err != nil {
		_ = d.Close()
		return nil, err
	}
	return d, nil
}

func tableHasColumn(ctx context.Context, d *sql.DB, table, col string) (bool, error) {
	q := fmt.Sprintf(`SELECT 1 FROM pragma_table_info(%q) WHERE name = ? LIMIT 1`, table)
	var one int
	err := d.QueryRowContext(ctx, q, col).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func tryAddColumn(ctx context.Context, d *sql.DB, table, col, decl string) error {
	ok, err := tableHasColumn(ctx, d, table, col)
	if err != nil || ok {
		return err
	}
	_, err = d.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %q ADD COLUMN %q %s", table, col, decl))
	return err
}

func migrate(d *sql.DB) error {
	const schema = `
CREATE TABLE IF NOT EXISTS schema_migrations (
	id INTEGER PRIMARY KEY,
	applied_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS sweeps (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	started_at TEXT NOT NULL,
	finished_at TEXT,
	model TEXT NOT NULL,
	prompt_version TEXT NOT NULL,
	status TEXT NOT NULL,
	error_message TEXT,
	raw_response_ref TEXT,
	response_json TEXT
);

CREATE TABLE IF NOT EXISTS items (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	created_at TEXT NOT NULL,
	source_kind TEXT NOT NULL,
	external_id TEXT,
	title TEXT NOT NULL,
	summary TEXT,
	url TEXT,
	feed_url TEXT,
	urgency INTEGER NOT NULL DEFAULT 3,
	lang TEXT,
	title_translated TEXT,
	summary_translated TEXT,
	translator_model TEXT,
	translated_at TEXT,
	tags_json TEXT NOT NULL DEFAULT '[]',
	cluster_key TEXT,
	sweep_id INTEGER REFERENCES sweeps(id),
	alert_sent_at TEXT,
	UNIQUE(source_kind, external_id)
);

CREATE INDEX IF NOT EXISTS idx_items_created ON items(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_items_cluster ON items(cluster_key);
CREATE INDEX IF NOT EXISTS idx_items_sweep ON items(sweep_id);
CREATE INDEX IF NOT EXISTS idx_items_urgency ON items(urgency DESC);

CREATE TABLE IF NOT EXISTS market_quotes (
	symbol TEXT PRIMARY KEY,
	name TEXT,
	price REAL,
	change_pct REAL,
	currency TEXT,
	fetched_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS alert_log (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	sent_at TEXT NOT NULL,
	channel TEXT NOT NULL,
	item_id INTEGER NOT NULL REFERENCES items(id),
	digest TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_alert_log_sent ON alert_log(sent_at);

CREATE TABLE IF NOT EXISTS entities (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	name        TEXT NOT NULL,
	kind        TEXT NOT NULL,
	first_seen  TEXT NOT NULL,
	last_seen   TEXT NOT NULL,
	item_count  INTEGER NOT NULL DEFAULT 0,
	UNIQUE(name, kind)
);

CREATE INDEX IF NOT EXISTS idx_entities_kind ON entities(kind);
CREATE INDEX IF NOT EXISTS idx_entities_name ON entities(name);

CREATE TABLE IF NOT EXISTS item_entities (
	item_id    INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
	entity_id  INTEGER NOT NULL REFERENCES entities(id) ON DELETE CASCADE,
	PRIMARY KEY (item_id, entity_id)
);

CREATE INDEX IF NOT EXISTS idx_item_entities_item   ON item_entities(item_id);
CREATE INDEX IF NOT EXISTS idx_item_entities_entity ON item_entities(entity_id);

CREATE TABLE IF NOT EXISTS situations (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	name        TEXT NOT NULL,
	slug        TEXT NOT NULL UNIQUE,
	description TEXT NOT NULL DEFAULT '',
	status      TEXT NOT NULL DEFAULT 'active',
	created_at  TEXT NOT NULL,
	updated_at  TEXT NOT NULL,
	item_count  INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_situations_updated ON situations(updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_situations_status  ON situations(status);

CREATE TABLE IF NOT EXISTS situation_items (
	situation_id INTEGER NOT NULL REFERENCES situations(id) ON DELETE CASCADE,
	item_id      INTEGER NOT NULL REFERENCES items(id) ON DELETE CASCADE,
	linked_at    TEXT NOT NULL,
	PRIMARY KEY (situation_id, item_id)
);

CREATE INDEX IF NOT EXISTS idx_situation_items_sit  ON situation_items(situation_id);
CREATE INDEX IF NOT EXISTS idx_situation_items_item ON situation_items(item_id);

CREATE TABLE IF NOT EXISTS users (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	username   TEXT NOT NULL UNIQUE,
	email      TEXT NOT NULL,
	password   TEXT NOT NULL,
	created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS sessions (
	token      TEXT PRIMARY KEY,
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	created_at TEXT NOT NULL DEFAULT (datetime('now')),
	expires_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_expires ON sessions(expires_at);

CREATE TABLE IF NOT EXISTS user_actions (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	item_id    INTEGER REFERENCES items(id) ON DELETE CASCADE,
	action     TEXT NOT NULL,
	data       TEXT,
	created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_user_actions_user   ON user_actions(user_id, action);
CREATE INDEX IF NOT EXISTS idx_user_actions_item   ON user_actions(item_id);

-- Geopolitical Alpha: constraints tracked against situations/regions
CREATE TABLE IF NOT EXISTS constraints (
	id              INTEGER PRIMARY KEY AUTOINCREMENT,
	situation_id    INTEGER REFERENCES situations(id) ON DELETE SET NULL,
	region          TEXT NOT NULL DEFAULT '',
	type            TEXT NOT NULL,
	name            TEXT NOT NULL,
	description     TEXT NOT NULL DEFAULT '',
	mutability      TEXT NOT NULL DEFAULT 'medium',
	direction       TEXT NOT NULL DEFAULT 'neutral',
	evidence        TEXT NOT NULL DEFAULT '',
	data_streams    TEXT NOT NULL DEFAULT '[]',
	status          TEXT NOT NULL DEFAULT 'active',
	created_at      TEXT NOT NULL,
	updated_at      TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_constraints_situation ON constraints(situation_id);
CREATE INDEX IF NOT EXISTS idx_constraints_type ON constraints(type);
CREATE INDEX IF NOT EXISTS idx_constraints_region ON constraints(region);

-- Geopolitical Alpha: net assessments (Bayesian analysis of a situation)
CREATE TABLE IF NOT EXISTS net_assessments (
	id                    INTEGER PRIMARY KEY AUTOINCREMENT,
	situation_id          INTEGER NOT NULL REFERENCES situations(id) ON DELETE CASCADE,
	lens                  TEXT NOT NULL DEFAULT 'cyclical',
	title                 TEXT NOT NULL,
	summary               TEXT NOT NULL DEFAULT '',
	prior_probability     REAL,
	current_probability   REAL,
	fulcrum_constraint_id INTEGER REFERENCES constraints(id),
	base_case             TEXT NOT NULL DEFAULT '',
	bull_case             TEXT NOT NULL DEFAULT '',
	bear_case             TEXT NOT NULL DEFAULT '',
	investment_implications TEXT NOT NULL DEFAULT '',
	status                TEXT NOT NULL DEFAULT 'active',
	created_at            TEXT NOT NULL,
	updated_at            TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_net_assessments_situation ON net_assessments(situation_id);
CREATE INDEX IF NOT EXISTS idx_net_assessments_status ON net_assessments(status);

-- Links assessments to the constraints they weigh
CREATE TABLE IF NOT EXISTS assessment_constraints (
	assessment_id   INTEGER NOT NULL REFERENCES net_assessments(id) ON DELETE CASCADE,
	constraint_id   INTEGER NOT NULL REFERENCES constraints(id) ON DELETE CASCADE,
	weight          TEXT NOT NULL DEFAULT 'medium',
	notes           TEXT NOT NULL DEFAULT '',
	PRIMARY KEY (assessment_id, constraint_id)
);

-- Bayesian update log
CREATE TABLE IF NOT EXISTS probability_updates (
	id              INTEGER PRIMARY KEY AUTOINCREMENT,
	assessment_id   INTEGER NOT NULL REFERENCES net_assessments(id) ON DELETE CASCADE,
	prior           REAL NOT NULL,
	posterior        REAL NOT NULL,
	evidence        TEXT NOT NULL,
	source_item_id  INTEGER REFERENCES items(id),
	constraint_id   INTEGER REFERENCES constraints(id),
	created_at      TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_prob_updates_assessment ON probability_updates(assessment_id);

-- Geopolitical calendar
CREATE TABLE IF NOT EXISTS geo_calendar (
	id              INTEGER PRIMARY KEY AUTOINCREMENT,
	event_date      TEXT NOT NULL,
	title           TEXT NOT NULL,
	description     TEXT NOT NULL DEFAULT '',
	region          TEXT NOT NULL DEFAULT '',
	event_type      TEXT NOT NULL DEFAULT 'other',
	market_relevance TEXT NOT NULL DEFAULT 'medium',
	assessment_id   INTEGER REFERENCES net_assessments(id),
	status          TEXT NOT NULL DEFAULT 'upcoming',
	created_at      TEXT NOT NULL,
	updated_at      TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_geo_calendar_date ON geo_calendar(event_date);
CREATE INDEX IF NOT EXISTS idx_geo_calendar_status ON geo_calendar(status);

-- Data stream monitors for constraint changes
CREATE TABLE IF NOT EXISTS data_streams (
	id              INTEGER PRIMARY KEY AUTOINCREMENT,
	constraint_id   INTEGER NOT NULL REFERENCES constraints(id) ON DELETE CASCADE,
	name            TEXT NOT NULL,
	description     TEXT NOT NULL DEFAULT '',
	source_type     TEXT NOT NULL DEFAULT 'manual',
	source_config   TEXT NOT NULL DEFAULT '{}',
	last_value      TEXT,
	last_checked_at TEXT,
	threshold_note  TEXT NOT NULL DEFAULT '',
	created_at      TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_data_streams_constraint ON data_streams(constraint_id);

`
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := d.ExecContext(ctx, schema); err != nil {
		return err
	}

	// Upgrades for DBs created before newer columns/tables existed.
	for _, step := range []struct {
		table string
		col   string
		decl  string
	}{
		{"items", "feed_url", "TEXT"},
		{"items", "alert_sent_at", "TEXT"},
		{"sweeps", "response_json", "TEXT"},
		{"items", "content_text", "TEXT"},
		{"items", "content_translated", "TEXT"},
		{"items", "content_fetched_at", "TEXT"},
		{"items", "entities_extracted_at", "TEXT"},
		{"entities", "canonical_id", "INTEGER REFERENCES entities(id)"},
		{"situations", "parent_id", "INTEGER REFERENCES situations(id)"},
		{"items", "brief_text", "TEXT"},
		{"items", "brief_at", "TEXT"},
		// Multi-domain alpha: add domain column to alpha tables
		{"constraints", "domain", "TEXT NOT NULL DEFAULT 'geopolitics'"},
		{"net_assessments", "domain", "TEXT NOT NULL DEFAULT 'geopolitics'"},
		{"geo_calendar", "domain", "TEXT NOT NULL DEFAULT 'geopolitics'"},
		// Situation rolling snippet (Ollama-generated)
		{"situations", "snippet", "TEXT NOT NULL DEFAULT ''"},
		{"situations", "snippet_generated_at", "TEXT"},
	} {
		if err := tryAddColumn(ctx, d, step.table, step.col, step.decl); err != nil {
			return err
		}
	}

	// Rename geo_calendar -> alpha_calendar (idempotent)
	var hasGeo int
	if d.QueryRowContext(ctx, `SELECT 1 FROM sqlite_master WHERE type='table' AND name='geo_calendar'`).Scan(&hasGeo) == nil {
		_, _ = d.ExecContext(ctx, `ALTER TABLE geo_calendar RENAME TO alpha_calendar`)
	}

	// Domain indexes
	for _, idx := range []string{
		`CREATE INDEX IF NOT EXISTS idx_constraints_domain ON constraints(domain)`,
		`CREATE INDEX IF NOT EXISTS idx_net_assessments_domain ON net_assessments(domain)`,
		`CREATE INDEX IF NOT EXISTS idx_alpha_calendar_domain ON alpha_calendar(domain)`,
	} {
		if _, err := d.ExecContext(ctx, idx); err != nil {
			return err
		}
	}

	return nil
}
