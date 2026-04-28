# Geopolitical Alpha Analysis

Perform constraint-based geopolitical analysis using Marko Papic's Geopolitical Alpha framework. This skill is the primary tool for creating and updating net assessments, constraint inventories, probability updates, and the geopolitical calendar.

## When to use

Invoked via `/geopolitical-alpha`. Run periodically (weekly or when major developments occur) to:
- Create new net assessments for emerging situations
- Update existing assessments with new evidence (Bayesian updates)
- Maintain the constraint registry
- Populate and update the geopolitical calendar
- Review data streams for constraint changes

## Weekly mode

When invoked as part of the weekly combined run with no specific event trigger, the default behavior is:

1. Scan all active situations and assessments in this domain for material changes since the last weekly run.
2. Compare against last week's digest at `data/alpha/digests/{prev_week}/geopolitics.md` if present — what was forecast, what is still relevant, what shifted.
3. Update assessments where evidence has moved the probability or weakened a fulcrum constraint.
4. Create new assessments only for situations that cross the threshold for needing one (high item count, market-relevant, decision-point approaching).
5. Perform Step C: stale cleanup.
6. Write this week's digest (Step D).

The weekly run prefers updating over creating. Don't flood the system with new assessments — focus on quality over coverage. If nothing material changed in a domain this week, the digest can be short and sections can be `_None._`.

## Smoke-test mode

If the environment variable `SMOKE_TEST=1` is set, log every API call and file write you would make but do NOT actually POST or write. Useful for validating the skill before enabling the systemd timer.

## The Papic Framework (analytical guide)

**Core thesis:** Don't forecast based on what policymakers *want* (preferences) — forecast based on what they *can actually do* (constraints). Preferences are optional and subject to constraints; constraints are neither optional nor subject to preferences.

**Constraint types:**
| Type | Examples | Mutability |
|------|----------|------------|
| **Political** | Legislative majorities, coalition partners, approval ratings, median voter | Medium |
| **Economic** | Debt/GDP, current account, FX reserves, fiscal space, trade dependencies | Low-medium |
| **Geopolitical** | Military balance, geography, alliances, nuclear capability | Low |
| **Constitutional** | Judicial review, treaty obligations, constitutional limits | Low |
| **Time** | Election cycles, term limits, legislative calendars | Fixed |

**Key principles:**
- Constraints have high diagnosticity (predictive value). Preferences have low diagnosticity.
- Under pressure (time, money, political survival), policymakers abandon preferences and submit to constraints.
- The **fulcrum constraint** is the single constraint most likely to determine the outcome.
- **Net assessments** net out competing constraints to produce a Bayesian prior.

**Three analytical lenses:**
- **Discrete** — reactive analysis of sudden developments
- **Cyclical** — 12-18 month calendar of elections, summits, deadlines
- **Structural** — 3-10 year horizon themes

## Instructions

### Step 0: Review current state

Fetch current assessments, situations, and recent news:

```bash
# Current assessments
curl -s "http://localhost:8080/api/assessments?status=active&domain=geopolitics" > /tmp/alpha_assessments.json

# Active situations from the knowledge graph
curl -s "http://localhost:8080/api/situations?status=active&tree=true&limit=30" > /tmp/alpha_situations.json

# Recent high-urgency items (last 48h)
curl -s "http://localhost:8080/api/items?hours=48&limit=200&min_u=2" > /tmp/alpha_items.json

# Upcoming calendar events
curl -s "http://localhost:8080/api/calendar?status=upcoming&domain=geopolitics" > /tmp/alpha_calendar.json

# Current constraints
curl -s "http://localhost:8080/api/constraints?status=active&domain=geopolitics" > /tmp/alpha_constraints.json

# Market data for context
curl -s "http://localhost:8080/api/markets" > /tmp/alpha_markets.json
```

Parse these with python3 to understand the current analytical landscape.

### Step 1: Identify situations needing assessment

Compare active situations against existing assessments. Prioritize creating assessments for:
1. Situations with high item counts and no assessment
2. Situations with recent activity spikes
3. Situations with market-relevant implications
4. Situations approaching a decision point or deadline

Also check: do any existing assessments need updating based on new evidence in the items feed?

### Step 2: For each situation needing work, perform constraint analysis

For each situation you're assessing (new or update):

**2a. Research constraints**

Use `WebSearch` to research the material constraints around the situation:
- Political constraints (who controls what, approval ratings, legislative math)
- Economic constraints (debt levels, trade data, fiscal space)
- Geopolitical constraints (military balance, geography, alliances)
- Constitutional/legal constraints (what can/can't be done legally)
- Time constraints (elections, deadlines, term limits)

Use the Reef Data API for economic data when relevant:
```bash
# Example: fetch a FRED series for economic constraint evidence
curl -s "https://data.reefinsights.com/api/platform/v1/fred/series/GDP?limit=4" \
  -H "Authorization: Bearer $REEF_DATA_API_KEY"
```

The Reef Data API key is in the reef-insights-v6 project `.env` file. Read it if needed:
```bash
grep REEF_DATA_API_KEY ~/Code/reef-insights-v6/.env
```

**2b. Create/update constraints via API**

For each identified constraint (note `"domain": "geopolitics"` is required):
```bash
curl -s -X POST http://localhost:8080/api/constraints -d '{
  "situation_id": SITUATION_ID,
  "domain": "geopolitics",
  "region": "US",
  "type": "political",
  "name": "Congressional majority composition",
  "description": "Republicans hold slim House majority (220-215), limiting legislative options",
  "mutability": "medium",
  "direction": "constraining",
  "evidence": "Current seat count as of March 2026",
  "status": "active"
}'
```

**2c. Build the net assessment**

Apply the Papic framework:
1. List all constraints and their directions (constraining/enabling/neutral)
2. Identify which constraints are binding vs. slack
3. Identify the **fulcrum constraint** — the one most likely to determine the outcome
4. Net out competing constraints to estimate probability
5. Develop base/bull/bear cases
6. Identify investment implications

**2d. Create/update the assessment**

```bash
# Create new assessment
curl -s -X POST http://localhost:8080/api/assessments -d '{
  "situation_id": SITUATION_ID,
  "lens": "cyclical",
  "title": "US-China Tariff Escalation Through 2026 Midterms",
  "summary": "Constraints analysis suggests 75% probability tariffs remain or escalate...",
  "prior_probability": 0.75,
  "fulcrum_constraint_id": CONSTRAINT_ID,
  "base_case": "Status quo with incremental escalation. Both parties compete on hawkishness.",
  "bull_case": "Negotiated deal with face-saving concessions if economic pain reaches threshold.",
  "bear_case": "Taiwan crisis triggers full decoupling, broad sanctions.",
  "investment_implications": "Overweight domestic manufacturers, underweight China-exposed supply chains."
}'

# Link constraints to assessment
curl -s -X POST http://localhost:8080/api/assessments/ASSESSMENT_ID/constraints -d '{
  "constraint_id": CONSTRAINT_ID,
  "weight": "fulcrum",
  "notes": "Election cycle prevents de-escalation"
}'
```

**2e. Log Bayesian updates for existing assessments**

When new evidence shifts the probability:
```bash
curl -s -X POST http://localhost:8080/api/assessments/ASSESSMENT_ID/update-probability -d '{
  "prior": 0.75,
  "posterior": 0.80,
  "evidence": "Bipartisan bill introduced for 50% tariff on Chinese EVs — both parties competing on hawkishness, reinforcing political constraint",
  "source_item_id": ITEM_ID,
  "constraint_id": CONSTRAINT_ID
}'
```

### Step 3: Maintain the geopolitical calendar

Add upcoming events that could affect assessments:
```bash
curl -s -X POST http://localhost:8080/api/calendar -d '{
  "event_date": "2026-11-03",
  "title": "US Midterm Elections",
  "description": "Full House + 1/3 Senate. Key for trade policy direction.",
  "region": "US",
  "event_type": "election",
  "market_relevance": "high",
  "assessment_id": ASSESSMENT_ID
}'
```

Event types: `election`, `summit`, `deadline`, `military`, `fiscal`, `referendum`, `other`
Market relevance: `low`, `medium`, `high`

Mark past events:
```bash
curl -s -X PUT http://localhost:8080/api/calendar/EVENT_ID -d '{"status": "passed"}'
```

### Step 4: Set up data streams for monitoring

For key constraints, define what data to watch:
```bash
curl -s -X POST http://localhost:8080/api/data-streams -d '{
  "constraint_id": CONSTRAINT_ID,
  "name": "US imports from China",
  "description": "Monthly trade data showing import dependency",
  "source_type": "fred",
  "source_config": "{\"series_id\": \"IMP0015\"}",
  "threshold_note": "Decline below $30B/month would signal meaningful decoupling"
}'
```

Source types: `fred`, `reef`, `rss_keyword`, `sweep_keyword`, `manual`

### Step 5: Summary output

After completing analysis, output a summary to the user:
- How many assessments were created/updated
- Any significant probability shifts
- Key upcoming calendar events
- Any constraints that changed status
- Recommendations for what to watch

### Step C: Stale assessment cleanup (weekly mode)

Walk active assessments for this domain and resolve those that have wound down. Be conservative — the goal is removing dead weight, not pruning aggressively.

```bash
curl -s "http://localhost:8080/api/assessments?domain=geopolitics&status=active" > /tmp/geo_active.json
```

For each assessment, mark as resolved if any of:
- Underlying situation has `status: resolved`.
- Most recent `probability_update` is older than 60 days AND no related items in the last 30 days.
- Situation has effectively wound down based on the news flow.

```bash
curl -s -X PUT http://localhost:8080/api/assessments/{ID} -d '{
  "status": "resolved",
  "summary": "Resolved during weekly cleanup — situation has wound down."
}'
```

Also delete calendar events with `status: passed` older than 90 days:
```bash
curl -s -X DELETE http://localhost:8080/api/calendar/{ID}
```

When in doubt, leave it. Log a one-line reason for each resolution.

### Step D: Weekly digest (weekly mode)

Write a markdown digest summarizing this week's analytical activity. Path:

```bash
WEEK=$(date +%G-W%V)   # e.g. 2026-W17
DIGEST_DIR="/home/shane/Code/SituationMonitor/data/alpha/digests/$WEEK"
mkdir -p "$DIGEST_DIR"
DIGEST_FILE="$DIGEST_DIR/geopolitics.md"
```

Schema (use exactly this structure — empty sections write `_None._`, never omit a heading):

```markdown
# Geopolitics — Week {YYYY-Www}
Generated: {ISO timestamp}

## Probability shifts this week
- {Assessment title}: {prior} → {posterior} ({Δ%}). {one-line reason}

## New assessments
- {title} — prior {p}, fulcrum: {constraint name}

## Resolved this week
- {title} — outcome: {short note}

## New constraints
- {name} ({type}, {direction}) — {one line}

## Upcoming events (next 30 days)
- {date} — {title} ({event_type}, {market_relevance})

## What to watch
{2-3 sentence forward look — only the most material items, no padding}
```

Use the same writing style and banned-pattern rules as the morning briefing. See `.claude/skills/daily-briefing.md` Step 5 for the full list.

## Writing style

Same as the daily briefing:
- Intelligence-briefing tone: concise, analytical, direct
- Data-forward — anchor claims in specific numbers
- Source-attributed — cite sources inline
- Take positions — this is analysis, not wire service neutrality
- No banned AI writing patterns (see daily-briefing.md for full list)

## Tools needed

- Bash with `curl` (local API reads/writes — WebFetch cannot reach localhost)
- Bash with `python3` (parsing large JSON responses)
- WebSearch (constraint research and verification)
- Read (check Reef API key, existing files)

## API Reference (quick)

```
# Constraints
GET    /api/constraints?situation_id=&type=&region=&status=
POST   /api/constraints                         {situation_id, region, type, name, description, mutability, direction, evidence, status}
GET    /api/constraints/{id}
PUT    /api/constraints/{id}                     (partial update, any fields)
DELETE /api/constraints/{id}

# Net Assessments
GET    /api/assessments?situation_id=&lens=&status=
POST   /api/assessments                          {situation_id, lens, title, summary, prior_probability, fulcrum_constraint_id, base_case, bull_case, bear_case, investment_implications}
GET    /api/assessments/{id}                     (includes linked constraints + update history)
PUT    /api/assessments/{id}                     (partial update)
DELETE /api/assessments/{id}
POST   /api/assessments/{id}/update-probability  {prior, posterior, evidence, source_item_id?, constraint_id?}
POST   /api/assessments/{id}/constraints         {constraint_id, weight, notes}
DELETE /api/assessments/{aid}/constraints/{cid}

# Calendar
GET    /api/calendar?from=&to=&region=&status=
POST   /api/calendar                             {event_date, title, description, region, event_type, market_relevance, assessment_id?}
GET    /api/calendar/{id}
PUT    /api/calendar/{id}                        (partial update)
DELETE /api/calendar/{id}

# Data Streams
GET    /api/data-streams?constraint_id=
POST   /api/data-streams                         {constraint_id, name, description, source_type, source_config, threshold_note}
PUT    /api/data-streams/{id}                    (partial update)
DELETE /api/data-streams/{id}

# Dashboard (composite)
GET    /api/alpha/dashboard                      (active assessments + upcoming calendar + recent updates)
```
