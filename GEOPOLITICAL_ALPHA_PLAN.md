# Geopolitical Alpha — Implementation Plan

Build a constraint-based geopolitical analysis layer into SituationMonitor, inspired by Marko Papic's *Geopolitical Alpha* framework. The system should take the raw news flow, entity graph, and situation tracking that already exists and add structured analytical tools on top: constraint registries, net assessments with Bayesian updating, a geopolitical calendar, and data stream monitoring.

## The Papic Framework (reference for implementer)

Papic's core thesis: **don't forecast based on what policymakers *want* (preferences) — forecast based on what they *can actually do* (constraints).** Preferences are optional and subject to constraints; constraints are neither optional nor subject to preferences.

### Three Pillars

1. **Material Constraints** — economic realities (debt levels, trade dependencies, fiscal capacity), political capital (legislative majorities, coalition stability, approval ratings), institutional/bureaucratic limits, and geopolitical realities (military capability, geography, alliances) that physically box policymakers in.

2. **Diagnosticity** — some signals predict outcomes better than others. Constraints have high diagnosticity (if a leader literally cannot do something, they won't). Preferences have low diagnosticity (leaders say things they can't deliver all the time). The system should weight constraint-based signals over preference-based ones.

3. **Disposition Effect / Time Constraint** — under pressure (time, money, political survival), policymakers abandon preferences and submit to constraints. The system should track when actors are approaching these pressure points.

### Operationalization

**Net Assessment:** A structured analysis that "nets out" competing constraints to produce a Bayesian prior (probability of an outcome). The key output is not just the probability but the identification of the **fulcrum constraint** — the single constraint most likely to determine the outcome — and the **data streams** that would signal a change.

**Three Lenses:**
- **Discrete Event** — reactive analysis of sudden developments (assassinations, sanctions, military actions). Have pre-built constraint assessments ready for top risks.
- **Cyclical** — 12-18 month calendar of elections, summits, treaty deadlines, fiscal cliffs, military exercises. Produce net assessments ahead of each.
- **Around the Curve** — structural themes on 3-10 year horizons (e.g., "will the dollar lose reserve status?", "is deglobalization permanent?"). Challenge consensus assumptions.

**Bayesian Updating:** The prior from a net assessment is the starting point. As data streams deliver new information, posteriors update. The system should track: prior → new evidence → posterior, with a log of what changed and why.

### Constraint Types (from the book)

| Type | Examples | Mutability |
|------|----------|------------|
| **Political** | Legislative majorities, coalition partners, approval ratings, median voter position | Medium — shifts with elections, crises |
| **Economic** | Debt/GDP, current account, FX reserves, fiscal space, trade dependencies | Low-medium — moves slowly unless crisis |
| **Geopolitical** | Military balance, geography, alliance commitments, nuclear capability | Low — structural |
| **Constitutional/Legal** | Judicial review, treaty obligations, constitutional limits on power | Low — but can be amended under pressure |
| **Time** | Election cycles, term limits, legislative calendars, public attention span | Fixed — the clock always runs |

## What Already Exists (file references)

### Data Ingest Layer
- `internal/ingest/rss/ingest.go` — RSS headline aggregation from `config/feeds.txt`
- `internal/ingest/sweep/loop.go` — Grok-powered situation sweeps via OpenRouter
- `internal/openrouter/sweep.go` — sweep execution, structured JSON output
- `config/brief.txt` — sweep focus instructions (currently: US policy, geopolitics, markets, tech)
- `internal/market/loop.go` + `yahoo.go` — market quote snapshots

### Knowledge Graph
- `internal/extract/worker.go` — 4-pass pipeline: NER → clustering → situation creation → normalization
- `internal/ollama/ner.go` — entity extraction (PERSON/ORG/PLACE/TOPIC)
- `internal/ollama/summarize.go` — contextual briefing and situation naming
- `internal/store/entity.go` — entity CRUD, related-item queries, `UpsertEntity`, `FindRelatedItems`
- `internal/store/situation.go` — situation tracking with hierarchy (`SituationRow`, parent/child, `AutoLinkSituationHierarchy`)
- `internal/store/merge.go` — entity normalization and dedup rules
- `internal/store/ner.go` — clustering queries, batch operations

### HTTP / API Layer
- `internal/httpserver/server.go` — all route mounts (line ~57-95)
- `internal/httpserver/intel.go` — knowledge graph endpoints: `/api/brief/{id}`, `/api/situations`, `/api/entities`
- `internal/httpserver/manage.go` — entity/situation CRUD endpoints
- `internal/httpserver/pages.go` — saved items, settings pages

### Database
- `internal/db/db.go` — SQLite schema + migrations. Key tables: `items`, `sweeps`, `entities`, `item_entities`, `situations`, `situation_items`, `market_quotes`, `users`, `sessions`, `user_actions`
- Database file: `./data/situation.db`

### Daily Briefings
- `.claude/skills/daily-briefing.md` — Claude skill that curates the knowledge graph and produces daily YAML briefings
- `data/pages/YYYY-MM-DD.yaml` — generated briefing files
- Rendered at `/daily/{date}` with index at `/daily/`

### External Data (available but not yet wired into SitMon)
- **Reef Data API** — FRED economic series, NAR/Realtor/Zillow housing data. API key in reef-insights-v6 `.env` (`REEF_DATA_API_KEY`). Upstream at `http://127.0.0.1:3001/api/platform/v1/`. Can be called directly from SitMon since it runs on the same machine.
- **Web search** — available via Claude sessions for ad-hoc research

### Config
- `internal/config/config.go` — all env var parsing
- `cmd/situation-monitor/main.go` — entry point, worker startup

## Implementation Plan

### Phase 1: Schema & Data Model

Add new tables to `internal/db/db.go` migration:

```sql
-- A constraint tracked against a situation or region
CREATE TABLE IF NOT EXISTS constraints (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    situation_id    INTEGER REFERENCES situations(id) ON DELETE SET NULL,
    region          TEXT NOT NULL DEFAULT '',
    type            TEXT NOT NULL,  -- political, economic, geopolitical, constitutional, time
    name            TEXT NOT NULL,
    description     TEXT NOT NULL DEFAULT '',
    mutability      TEXT NOT NULL DEFAULT 'medium',  -- low, medium, high
    direction       TEXT NOT NULL DEFAULT 'neutral',  -- constraining, enabling, neutral
    evidence        TEXT NOT NULL DEFAULT '',  -- current evidence/data supporting this constraint
    data_streams    TEXT NOT NULL DEFAULT '[]',  -- JSON array of data stream descriptors to monitor
    status          TEXT NOT NULL DEFAULT 'active',  -- active, resolved, dormant
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_constraints_situation ON constraints(situation_id);
CREATE INDEX IF NOT EXISTS idx_constraints_type ON constraints(type);
CREATE INDEX IF NOT EXISTS idx_constraints_region ON constraints(region);

-- A net assessment: structured Bayesian analysis of a situation
CREATE TABLE IF NOT EXISTS net_assessments (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    situation_id        INTEGER NOT NULL REFERENCES situations(id) ON DELETE CASCADE,
    lens                TEXT NOT NULL DEFAULT 'cyclical',  -- discrete, cyclical, structural
    title               TEXT NOT NULL,
    summary             TEXT NOT NULL DEFAULT '',
    prior_probability   REAL,  -- 0.0 to 1.0, nullable for non-binary assessments
    current_probability REAL,
    fulcrum_constraint_id INTEGER REFERENCES constraints(id),
    base_case           TEXT NOT NULL DEFAULT '',  -- most likely outcome narrative
    bull_case           TEXT NOT NULL DEFAULT '',
    bear_case           TEXT NOT NULL DEFAULT '',
    investment_implications TEXT NOT NULL DEFAULT '',
    status              TEXT NOT NULL DEFAULT 'active',  -- active, superseded, resolved
    created_at          TEXT NOT NULL,
    updated_at          TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_net_assessments_situation ON net_assessments(situation_id);
CREATE INDEX IF NOT EXISTS idx_net_assessments_status ON net_assessments(status);

-- Links net assessments to the constraints they weigh
CREATE TABLE IF NOT EXISTS assessment_constraints (
    assessment_id   INTEGER NOT NULL REFERENCES net_assessments(id) ON DELETE CASCADE,
    constraint_id   INTEGER NOT NULL REFERENCES constraints(id) ON DELETE CASCADE,
    weight          TEXT NOT NULL DEFAULT 'medium',  -- low, medium, high, fulcrum
    notes           TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (assessment_id, constraint_id)
);

-- Bayesian update log: each time new evidence shifts a probability
CREATE TABLE IF NOT EXISTS probability_updates (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    assessment_id   INTEGER NOT NULL REFERENCES net_assessments(id) ON DELETE CASCADE,
    prior           REAL NOT NULL,
    posterior        REAL NOT NULL,
    evidence        TEXT NOT NULL,  -- what changed
    source_item_id  INTEGER REFERENCES items(id),  -- optional link to the news item that triggered it
    constraint_id   INTEGER REFERENCES constraints(id),  -- which constraint was affected
    created_at      TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_prob_updates_assessment ON probability_updates(assessment_id);

-- Geopolitical calendar: upcoming events with market relevance
CREATE TABLE IF NOT EXISTS geo_calendar (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    event_date      TEXT NOT NULL,  -- YYYY-MM-DD or YYYY-MM for approximate
    title           TEXT NOT NULL,
    description     TEXT NOT NULL DEFAULT '',
    region          TEXT NOT NULL DEFAULT '',
    event_type      TEXT NOT NULL DEFAULT 'other',  -- election, summit, deadline, military, fiscal, referendum
    market_relevance TEXT NOT NULL DEFAULT 'medium',  -- low, medium, high
    assessment_id   INTEGER REFERENCES net_assessments(id),  -- linked assessment if one exists
    status          TEXT NOT NULL DEFAULT 'upcoming',  -- upcoming, active, passed
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_geo_calendar_date ON geo_calendar(event_date);
CREATE INDEX IF NOT EXISTS idx_geo_calendar_status ON geo_calendar(status);

-- Data stream monitors: specific metrics/signals to watch for constraint changes
CREATE TABLE IF NOT EXISTS data_streams (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    constraint_id   INTEGER NOT NULL REFERENCES constraints(id) ON DELETE CASCADE,
    name            TEXT NOT NULL,
    description     TEXT NOT NULL DEFAULT '',
    source_type     TEXT NOT NULL DEFAULT 'manual',  -- fred, reef, rss_keyword, sweep_keyword, manual
    source_config   TEXT NOT NULL DEFAULT '{}',  -- JSON config (e.g., FRED series ID, keyword patterns)
    last_value      TEXT,
    last_checked_at TEXT,
    threshold_note  TEXT NOT NULL DEFAULT '',  -- human description of what level would shift the assessment
    created_at      TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_data_streams_constraint ON data_streams(constraint_id);
```

New files to create:
- `internal/store/constraint.go` — CRUD for constraints table
- `internal/store/assessment.go` — CRUD for net_assessments, assessment_constraints, probability_updates
- `internal/store/calendar.go` — CRUD for geo_calendar
- `internal/store/datastream.go` — CRUD for data_streams

### Phase 2: API Endpoints

Add to `internal/httpserver/server.go` route mounts and implement in a new file `internal/httpserver/alpha.go`:

```
# Constraints
GET    /api/constraints?situation_id=&type=&region=&status=
POST   /api/constraints                         — create
PUT    /api/constraints/{id}                     — update
DELETE /api/constraints/{id}

# Net Assessments
GET    /api/assessments?situation_id=&lens=&status=
GET    /api/assessments/{id}                     — detail with constraints and update history
POST   /api/assessments                          — create
PUT    /api/assessments/{id}                      — update
POST   /api/assessments/{id}/update-probability   — log a Bayesian update

# Calendar
GET    /api/calendar?from=&to=&region=&status=
POST   /api/calendar
PUT    /api/calendar/{id}
DELETE /api/calendar/{id}

# Data Streams
GET    /api/data-streams?constraint_id=
POST   /api/data-streams
PUT    /api/data-streams/{id}
DELETE /api/data-streams/{id}

# Composite / Analysis
GET    /api/alpha/dashboard                      — overview: active assessments, upcoming calendar, recent updates
GET    /api/alpha/situation/{slug}               — full constraint + assessment view for a situation
```

### Phase 3: UI — Alpha Dashboard

Create `internal/httpserver/alpha.html` — a new page mounted at `/alpha` that provides:

1. **Active Net Assessments** — cards showing each tracked situation with current probability, fulcrum constraint, last update date, and trend direction
2. **Constraint Map** — for a selected assessment, show all constraints organized by type with their direction (constraining/enabling) and mutability
3. **Bayesian Update Log** — timeline of probability changes with evidence citations linked to news items
4. **Geopolitical Calendar** — timeline/list of upcoming events with market relevance indicators
5. **Data Stream Status** — which streams have been checked recently, any threshold alerts

Keep the UI consistent with the existing SitMon dashboard style (see `internal/httpserver/dashboard.html` for patterns). Server-rendered HTML + vanilla JS, no build step.

### Phase 4: AI-Assisted Assessment Generation

Create `internal/ollama/alpha.go` (or use OpenRouter for higher quality):

1. **Auto-generate constraint inventory** — given a situation and its linked items, have the LLM identify material constraints organized by type. Prompt should reference Papic's framework explicitly.

2. **Auto-generate net assessment draft** — given a situation + constraints + recent items, produce: base/bull/bear cases, suggested prior probability, fulcrum constraint identification, and data streams to monitor.

3. **Auto-suggest probability updates** — when new sweep items or RSS items arrive that are linked to a situation with an active assessment, have the LLM evaluate whether the evidence warrants a probability update and by how much.

These should produce drafts that are reviewed/edited by the user, not auto-committed. Surface them via the UI or the daily briefing.

### Phase 5: Data Stream Automation

Create `internal/alpha/worker.go` — a new worker goroutine (started in `cmd/situation-monitor/main.go`) that periodically:

1. Checks FRED series via the Reef Data API (`http://127.0.0.1:3001/api/platform/v1/`) for data streams configured with `source_type=fred`. Requires `REEF_DATA_API_KEY` env var.

2. Scans recent RSS items and sweep results for keyword matches on data streams configured with `source_type=rss_keyword` or `source_type=sweep_keyword`.

3. When a data stream value crosses a threshold or a keyword pattern fires, flags the linked constraint and assessment for review.

Config env vars to add to `internal/config/config.go`:
```
REEF_DATA_API_KEY      — API key for reef platform data
REEF_DATA_BASE_URL     — default http://127.0.0.1:3001/api/platform/v1
ALPHA_POLL_SEC         — data stream check interval, default 3600
ALPHA_ON_START         — run one check at startup, default true
```

### Phase 6: Daily Briefing Integration

Update the daily briefing skill (`.claude/skills/daily-briefing.md`) to include a "Geopolitical Alpha" section that:

1. Lists active net assessments with current probabilities
2. Flags any assessments where probability shifted in the last 24 hours
3. Lists upcoming calendar events in the next 7-14 days
4. Highlights any data stream alerts

## Build Order

1. Schema + store layer (Phase 1) — can be tested independently
2. API endpoints (Phase 2) — enables manual data entry immediately
3. UI (Phase 3) — makes it usable day-to-day
4. AI drafting (Phase 4) — accelerates assessment creation
5. Data stream automation (Phase 5) — closes the monitoring loop
6. Briefing integration (Phase 6) — ties it into daily workflow

Each phase should be a commit or small PR. Tests should follow existing patterns in `internal/httpserver/server_test.go` and `internal/store/list_test.go`.

## Example: How It Would Work

**Situation:** "US-China Trade Tensions" (already tracked via the knowledge graph)

**Constraints identified:**
- Political: US election cycle (Nov 2026 midterms) — mutability: fixed/time
- Political: median voter sentiment on China (consistently hawkish) — mutability: low
- Economic: US import dependency on Chinese manufacturing — mutability: medium (reshoring underway)
- Economic: China's US Treasury holdings as leverage — mutability: medium
- Geopolitical: Taiwan Strait military balance — mutability: low
- Constitutional: presidential tariff authority under existing trade law — mutability: low

**Fulcrum constraint:** US election cycle — both parties compete on being tough on China, constraining any de-escalation before midterms.

**Data streams to monitor:**
- FRED: US imports from China (series `IMP0015`)
- RSS keyword: "tariff", "trade war", "Section 301"
- Sweep keyword: "Taiwan", "TSMC", "semiconductor export controls"

**Net assessment:** 75% probability tariffs remain or escalate through 2026. Base case: status quo with incremental escalation. Bull case (for US equities): negotiated deal with face-saving concessions. Bear case: Taiwan crisis triggers full decoupling.

**Bayesian update example:** New sweep item reports bipartisan bill for 50% tariff on Chinese EVs → posterior moves to 80% (constraint reinforced: both parties competing on hawkishness).
