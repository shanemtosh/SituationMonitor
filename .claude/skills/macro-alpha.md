# Macro & Monetary Policy Alpha

Perform constraint-based macroeconomic and monetary policy analysis. Forecast central bank actions, fiscal policy outcomes, and economic regime shifts based on material constraints rather than stated preferences.

## When to use

Invoked via `/macro-alpha`. Run when major economic data releases, central bank meetings, or fiscal policy events occur.

## Weekly mode

When invoked as part of the weekly combined run with no specific event trigger, the default behavior is:

1. Scan all active situations and assessments in this domain for material changes since the last weekly run.
2. Compare against last week's digest at `data/alpha/digests/{prev_week}/macro.md` if present — what was forecast, what is still relevant, what shifted.
3. Update assessments where evidence has moved the probability or weakened a fulcrum constraint.
4. Create new assessments only for situations that cross the threshold for needing one.
5. Perform Step C: stale cleanup.
6. Write this week's digest (Step D).

Prefer updating over creating. If nothing material changed in macro this week, the digest can be short and sections can be `_None._`.

## Smoke-test mode

If the environment variable `SMOKE_TEST=1` is set, log every API call and file write you would make but do NOT actually POST or write.

## Core Framework

**Papic's thesis applied to macro:** Central banks and fiscal policymakers say what they *want* to do (preferences — forward guidance, policy targets), but material constraints determine what they *actually do*. When inflation is sticky, the Fed *cannot* cut regardless of what markets want. When debt service costs hit fiscal limits, governments *cannot* spend regardless of promises.

**Constraint types for macro analysis:**

| Type | Examples | Mutability |
|------|----------|------------|
| **Monetary** | Fed funds rate path, balance sheet size, QT pace, reserve levels, forward guidance credibility | Medium |
| **Fiscal** | Deficit trajectory, debt/GDP, debt ceiling timeline, spending authority, tax revenue trends | Medium |
| **Labor** | Unemployment rate, wage growth, participation rate, JOLTS openings, immigration flows | Low-medium |
| **Inflationary** | CPI/PCE trend, supply-side shocks, housing/shelter inflation, inflation expectations | Low |
| **Time** | FOMC meeting dates, data release calendar, fiscal year deadlines, election cycles | Fixed |

**Three lenses:**
- **Discrete** — sudden macro events (bank failures, commodity shocks, debt ceiling crises)
- **Cyclical** — FOMC meeting calendar, quarterly data releases, fiscal year deadlines
- **Structural** — secular inflation regime, reserve currency status, demographic trends

## Instructions

### Step 0: Review current state

```bash
# Current macro assessments
curl -s "http://localhost:8080/api/assessments?status=active&domain=macro" > /tmp/macro_assessments.json

# Current macro constraints
curl -s "http://localhost:8080/api/constraints?status=active&domain=macro" > /tmp/macro_constraints.json

# Upcoming macro events
curl -s "http://localhost:8080/api/calendar?status=upcoming&domain=macro" > /tmp/macro_calendar.json

# Recent items with economic relevance
curl -s "http://localhost:8080/api/items?hours=72&limit=200&min_u=2" > /tmp/macro_items.json

# Market data
curl -s "http://localhost:8080/api/markets" > /tmp/macro_markets.json
```

Filter items for macro-relevant content (Fed, inflation, employment, GDP, fiscal, Treasury).

### Step 1: Gather economic data

Use the Reef Data API for FRED series:
```bash
# Read the API key
REEF_KEY=$(grep REEF_DATA_API_KEY ~/Code/reef-insights-v6/.env | cut -d= -f2)

# Key series to check:
# Federal funds rate
curl -s "https://data.reefinsights.com/api/platform/v1/fred/series/FEDFUNDS?limit=6" -H "Authorization: Bearer $REEF_KEY"

# CPI (inflation)
curl -s "https://data.reefinsights.com/api/platform/v1/fred/series/CPIAUCSL?limit=6" -H "Authorization: Bearer $REEF_KEY"

# Unemployment rate
curl -s "https://data.reefinsights.com/api/platform/v1/fred/series/UNRATE?limit=6" -H "Authorization: Bearer $REEF_KEY"

# 10-year Treasury yield
curl -s "https://data.reefinsights.com/api/platform/v1/fred/series/DGS10?limit=12" -H "Authorization: Bearer $REEF_KEY"

# GDP growth
curl -s "https://data.reefinsights.com/api/platform/v1/fred/series/A191RL1Q225SBEA?limit=4" -H "Authorization: Bearer $REEF_KEY"
```

Use `WebSearch` for latest Fed commentary, economic analysis, and fiscal policy developments.

### Step 2: Constraint analysis and assessment creation

Follow the same pattern as geopolitical-alpha:
1. Identify material constraints on each actor (Fed, ECB, Congress, Treasury)
2. Determine constraint direction (tightening/easing/neutral)
3. Identify the fulcrum constraint
4. Build base/bull/bear scenarios
5. Log via API with `"domain": "macro"`

```bash
# Example: create a macro constraint
curl -s -X POST http://localhost:8080/api/constraints -d '{
  "domain": "macro",
  "region": "fed",
  "type": "inflationary",
  "name": "Core PCE stuck above 3%",
  "description": "...",
  "mutability": "low",
  "direction": "constraining",
  "evidence": "...",
  "status": "active"
}'

# Example: create a macro assessment
curl -s -X POST http://localhost:8080/api/assessments -d '{
  "situation_id": SITUATION_ID,
  "domain": "macro",
  "lens": "cyclical",
  "title": "Fed Rate Path: Probability of Cut Before September",
  "summary": "...",
  "prior_probability": 0.30,
  "base_case": "...",
  "bull_case": "...",
  "bear_case": "...",
  "investment_implications": "..."
}'

# Calendar: FOMC meetings, data releases
curl -s -X POST http://localhost:8080/api/calendar -d '{
  "domain": "macro",
  "event_date": "2026-05-06",
  "title": "FOMC Meeting",
  "region": "fed",
  "event_type": "fomc",
  "market_relevance": "high"
}'
```

Calendar event types: `fomc`, `data_release`, `testimony`, `fiscal_deadline`, `other`
Region values: `fed`, `ecb`, `boj`, `boe`, `fiscal`, `labor`

### Step C: Stale assessment cleanup (weekly mode)

```bash
curl -s "http://localhost:8080/api/assessments?domain=macro&status=active" > /tmp/macro_active.json
```

Mark as resolved any active assessment where:
- Underlying situation has `status: resolved`.
- Most recent `probability_update` is older than 60 days AND no related items in the last 30 days.
- The macro context that motivated the assessment has materially shifted (e.g. the FOMC decision it forecast already happened).

```bash
curl -s -X PUT http://localhost:8080/api/assessments/{ID} -d '{
  "status": "resolved",
  "summary": "Resolved during weekly cleanup — situation has wound down."
}'
```

Delete `status: passed` calendar events older than 90 days. Be conservative.

### Step D: Weekly digest (weekly mode)

Write a markdown digest to `data/alpha/digests/YYYY-Www/macro.md`. Use the same schema as the geopolitical digest — see `.claude/skills/geopolitical-alpha.md` Step D for the canonical structure. Empty sections write `_None._`, never omit a heading.

```bash
WEEK=$(date +%G-W%V)
DIGEST_DIR="/home/shane/Code/SituationMonitor/data/alpha/digests/$WEEK"
mkdir -p "$DIGEST_DIR"
DIGEST_FILE="$DIGEST_DIR/macro.md"
```

## Writing style

Same as the morning briefing — see `.claude/skills/daily-briefing.md` Step 5 for the full banned-pattern list. Intelligence-briefing tone, data-forward, source-attributed, no AI tells.

## Tools needed

- Bash with `curl` (local API + Reef Data API)
- Bash with `python3` (data parsing)
- WebSearch (Fed commentary, economic analysis)
- Read (Reef API key)
