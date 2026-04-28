# Energy Alpha

Perform constraint-based analysis of energy markets and the energy transition. Forecast oil/gas supply-demand, renewable buildout, and energy policy outcomes based on material constraints.

## When to use

Invoked via `/energy-alpha`. Run when major energy events occur: OPEC+ decisions, pipeline disruptions, sanctions changes, renewable policy shifts, energy market moves.

## Weekly mode

When invoked as part of the weekly combined run with no specific event trigger, the default behavior is:

1. Scan all active situations and assessments in this domain for material changes since the last weekly run.
2. Compare against last week's digest at `data/alpha/digests/{prev_week}/energy.md` if present.
3. Update assessments where evidence has moved the probability or weakened a fulcrum constraint.
4. Create new assessments only for situations that cross the threshold.
5. Perform Step C: stale cleanup.
6. Write this week's digest (Step D).

Prefer updating over creating. If nothing material changed in energy this week, the digest can be short.

## Smoke-test mode

If `SMOKE_TEST=1` is set, log every API call and file write you would make but do NOT actually POST or write.

## Core Framework

**Papic's thesis applied to energy:** Governments and OPEC members state production preferences, but geology, infrastructure, and geopolitical constraints determine actual output. Saudi Arabia *cannot* pump more than spare capacity allows. Europe *cannot* replace Russian gas overnight regardless of policy. Nuclear plants *cannot* be built in under a decade regardless of political will.

**Constraint types for energy analysis:**

| Type | Examples | Mutability |
|------|----------|------------|
| **Supply** | OPEC+ quotas, US shale output, refinery capacity, SPR levels, production decline rates | Medium |
| **Demand** | Economic growth, EV adoption rate, industrial consumption, seasonal patterns | Medium |
| **Infrastructure** | Pipeline capacity, LNG terminals, grid interconnects, storage capacity, refinery complexity | Low |
| **Regulatory** | Emissions rules, nuclear licensing, renewable mandates, drilling permits, carbon pricing | Medium |
| **Geopolitical** | Sanctions (Russia/Iran), chokepoint risk (Hormuz/Suez), producer alliances, energy weaponization | Low |

**Three lenses:**
- **Discrete** — sudden events (pipeline attack, refinery fire, sanctions announcement, Hormuz closure)
- **Cyclical** — OPEC+ meetings, seasonal demand patterns, inventory reports, policy review cycles
- **Structural** — energy transition pace, peak oil demand timing, grid decarbonization, nuclear renaissance

## Instructions

### Step 0: Review current state

```bash
# Current energy assessments
curl -s "http://localhost:8080/api/assessments?status=active&domain=energy" > /tmp/energy_assessments.json

# Current constraints
curl -s "http://localhost:8080/api/constraints?status=active&domain=energy" > /tmp/energy_constraints.json

# Upcoming events
curl -s "http://localhost:8080/api/calendar?status=upcoming&domain=energy" > /tmp/energy_calendar.json

# Recent items — energy is often interleaved with geopolitics
curl -s "http://localhost:8080/api/items?hours=72&limit=200&min_u=2" > /tmp/energy_items.json

# Market data for energy instruments
curl -s "http://localhost:8080/api/markets" > /tmp/energy_markets.json
```

Filter items for energy-relevant content (oil, gas, OPEC, pipeline, solar, nuclear, grid, LNG, refinery).

Note: Energy analysis often overlaps with geopolitics (sanctions, Hormuz). Cross-reference existing geopolitics constraints and assessments — link rather than duplicate.

### Step 1: Gather energy data

Use Reef Data API for energy price series:
```bash
REEF_KEY=$(grep REEF_DATA_API_KEY ~/Code/reef-insights-v6/.env | cut -d= -f2)

# Oil prices (WTI)
curl -s "https://data.reefinsights.com/api/platform/v1/fred/series/DCOILWTICO?limit=20" -H "Authorization: Bearer $REEF_KEY"

# Natural gas (Henry Hub)
curl -s "https://data.reefinsights.com/api/platform/v1/fred/series/DHHNGSP?limit=20" -H "Authorization: Bearer $REEF_KEY"

# Gasoline prices
curl -s "https://data.reefinsights.com/api/platform/v1/fred/series/GASREGW?limit=12" -H "Authorization: Bearer $REEF_KEY"
```

Use `WebSearch` for OPEC+ decisions, EIA/IEA reports, renewable capacity data, energy policy developments.

### Step 2: Constraint analysis and assessment creation

```bash
# Example: create an energy constraint
curl -s -X POST http://localhost:8080/api/constraints -d '{
  "domain": "energy",
  "region": "oil",
  "type": "supply",
  "name": "OPEC+ spare capacity at 10-year low",
  "description": "...",
  "mutability": "low",
  "direction": "constraining",
  "evidence": "...",
  "status": "active"
}'

# Example assessment
curl -s -X POST http://localhost:8080/api/assessments -d '{
  "situation_id": SITUATION_ID,
  "domain": "energy",
  "lens": "cyclical",
  "title": "Oil Price Direction: Probability of Brent Below $90 Within 90 Days",
  "summary": "...",
  "prior_probability": 0.35,
  "base_case": "...",
  "bull_case": "...",
  "bear_case": "...",
  "investment_implications": "..."
}'

# Calendar: OPEC meetings, data releases
curl -s -X POST http://localhost:8080/api/calendar -d '{
  "domain": "energy",
  "event_date": "2026-06-01",
  "title": "OPEC+ Ministerial Meeting",
  "region": "oil",
  "event_type": "opec_meeting",
  "market_relevance": "high"
}'
```

Calendar event types: `opec_meeting`, `regulatory`, `data_release`, `policy`, `earnings`, `other`
Region values: `oil`, `gas`, `renewables`, `nuclear`, `grid`, `lng`

### Step C: Stale assessment cleanup (weekly mode)

```bash
curl -s "http://localhost:8080/api/assessments?domain=energy&status=active" > /tmp/energy_active.json
```

Mark as resolved any active assessment where:
- Underlying situation has `status: resolved`.
- Most recent `probability_update` is older than 60 days AND no related items in the last 30 days.
- The forecast event already happened (e.g. OPEC+ decided, sanction took effect).

```bash
curl -s -X PUT http://localhost:8080/api/assessments/{ID} -d '{
  "status": "resolved",
  "summary": "Resolved during weekly cleanup — situation has wound down."
}'
```

Delete `status: passed` calendar events older than 90 days. Be conservative.

### Step D: Weekly digest (weekly mode)

Write a markdown digest to `data/alpha/digests/YYYY-Www/energy.md` using the schema in `.claude/skills/geopolitical-alpha.md` Step D. Empty sections write `_None._`, never omit a heading.

```bash
WEEK=$(date +%G-W%V)
DIGEST_DIR="/home/shane/Code/SituationMonitor/data/alpha/digests/$WEEK"
mkdir -p "$DIGEST_DIR"
DIGEST_FILE="$DIGEST_DIR/energy.md"
```

## Writing style

Same as the morning briefing — see `.claude/skills/daily-briefing.md` Step 5 for the full banned-pattern list. Intelligence-briefing tone, data-forward, source-attributed, no AI tells.

## Tools needed

- Bash with `curl` (local API + Reef Data API)
- Bash with `python3` (data parsing)
- WebSearch (energy market research, OPEC, EIA/IEA)
- Read (Reef API key)
