# Semiconductors & Technology Alpha

Perform constraint-based analysis of the semiconductor industry and technology supply chains. Forecast capacity decisions, trade policy outcomes, and technology transitions based on material constraints.

## When to use

Invoked via `/semiconductors-alpha`. Run when major industry events occur: earnings, fab announcements, export control changes, capacity data releases.

## Weekly mode

When invoked as part of the weekly combined run with no specific event trigger, the default behavior is:

1. Scan all active situations and assessments in this domain for material changes since the last weekly run.
2. Compare against last week's digest at `data/alpha/digests/{prev_week}/semiconductors.md` if present.
3. Update assessments where evidence has moved the probability or weakened a fulcrum constraint.
4. Create new assessments only for situations that cross the threshold.
5. Perform Step C: stale cleanup.
6. Write this week's digest (Step D).

Prefer updating over creating. If nothing material changed in semiconductors this week, the digest can be short.

## Smoke-test mode

If `SMOKE_TEST=1` is set, log every API call and file write you would make but do NOT actually POST or write.

## Core Framework

**Papic's thesis applied to semiconductors:** Companies and governments state preferences (build more fabs, achieve chip independence, lead in AI), but physics, capital, and geopolitical constraints determine actual outcomes. TSMC *cannot* build a fab in 6 months regardless of subsidies. China *cannot* replicate EUV lithography regardless of investment. Intel *cannot* recapture process leadership without solving yield at scale.

**Constraint types for semiconductor analysis:**

| Type | Examples | Mutability |
|------|----------|------------|
| **Physical** | Node advancement pace (3nm→2nm), EUV yield rates, thermal/power limits, packaging tech | Low |
| **Capacity** | Fab utilization rates, construction timelines (2-5yr), equipment lead times (ASML backlog) | Low |
| **Regulatory** | US export controls (BIS entity list), CHIPS Act disbursement, EU Chips Act, Japan restrictions | Medium |
| **Economic** | Capex budgets, hyperscaler AI spend, consumer demand cycles, memory pricing dynamics | Medium |
| **Competitive** | Market share (TSMC vs Samsung vs Intel foundry), IP moats, customer allocation priority | Medium |

**Three lenses:**
- **Discrete** — sudden events (new export controls, fab fire/earthquake, surprise earnings)
- **Cyclical** — quarterly earnings, annual capex guidance, equipment order cycles, memory pricing cycles
- **Structural** — US-China tech decoupling, AI compute demand curve, Moore's Law economics

## Instructions

### Step 0: Review current state

```bash
# Current semiconductor assessments
curl -s "http://localhost:8080/api/assessments?status=active&domain=semiconductors" > /tmp/semi_assessments.json

# Current constraints
curl -s "http://localhost:8080/api/constraints?status=active&domain=semiconductors" > /tmp/semi_constraints.json

# Upcoming events
curl -s "http://localhost:8080/api/calendar?status=upcoming&domain=semiconductors" > /tmp/semi_calendar.json

# Recent semiconductor items — filter by relevant feeds and entities
curl -s "http://localhost:8080/api/items?hours=72&limit=200&min_u=2" > /tmp/semi_items.json
```

Filter items from semiconductor-specific feeds (DigiTimes, TrendForce, EE Times, SemiEngineering, SemiAnalysis, KED Global) and by entities (TSMC, Intel, Samsung, NVIDIA, SK Hynix, ASML, etc.).

### Step 1: Gather industry data

Check key entities in the knowledge graph:
```bash
# Entity coverage for major semiconductor companies
for entity in TSMC Intel Samsung NVIDIA "SK Hynix" ASML Micron; do
  curl -s "http://localhost:8080/api/entities/$entity" | python3 -c "import json,sys;d=json.load(sys.stdin);e=d.get('entity',{});print(f'{e.get(\"name\",\"?\"):15s} {e.get(\"item_count\",0):3d} items')" 2>/dev/null
done
```

Use `WebSearch` for latest fab updates, capacity data, export control developments, earnings previews.

### Step 2: Constraint analysis and assessment creation

```bash
# Example: create a semiconductor constraint
curl -s -X POST http://localhost:8080/api/constraints -d '{
  "domain": "semiconductors",
  "region": "foundry",
  "type": "capacity",
  "name": "TSMC Arizona Fab 1 timeline slippage",
  "description": "...",
  "mutability": "low",
  "direction": "constraining",
  "evidence": "...",
  "status": "active"
}'

# Example assessment
curl -s -X POST http://localhost:8080/api/assessments -d '{
  "situation_id": SITUATION_ID,
  "domain": "semiconductors",
  "lens": "structural",
  "title": "US Foundry Independence: Probability of 20% Domestic Production by 2030",
  "summary": "...",
  "prior_probability": 0.15,
  "base_case": "...",
  "bull_case": "...",
  "bear_case": "...",
  "investment_implications": "..."
}'

# Calendar: earnings, trade actions, conferences
curl -s -X POST http://localhost:8080/api/calendar -d '{
  "domain": "semiconductors",
  "event_date": "2026-04-17",
  "title": "TSMC Q1 2026 Earnings",
  "region": "foundry",
  "event_type": "earnings",
  "market_relevance": "high"
}'
```

Calendar event types: `earnings`, `product_launch`, `regulation`, `trade_action`, `conference`, `other`
Region values: `foundry`, `memory`, `equipment`, `fabless`, `packaging`

### Step C: Stale assessment cleanup (weekly mode)

```bash
curl -s "http://localhost:8080/api/assessments?domain=semiconductors&status=active" > /tmp/semi_active.json
```

Mark as resolved any active assessment where:
- Underlying situation has `status: resolved`.
- Most recent `probability_update` is older than 60 days AND no related items in the last 30 days.
- The forecast event already happened (e.g. earnings released, fab announcement made).

```bash
curl -s -X PUT http://localhost:8080/api/assessments/{ID} -d '{
  "status": "resolved",
  "summary": "Resolved during weekly cleanup — situation has wound down."
}'
```

Delete `status: passed` calendar events older than 90 days. Be conservative.

### Step D: Weekly digest (weekly mode)

Write a markdown digest to `data/alpha/digests/YYYY-Www/semiconductors.md` using the schema in `.claude/skills/geopolitical-alpha.md` Step D. Empty sections write `_None._`, never omit a heading.

```bash
WEEK=$(date +%G-W%V)
DIGEST_DIR="/home/shane/Code/SituationMonitor/data/alpha/digests/$WEEK"
mkdir -p "$DIGEST_DIR"
DIGEST_FILE="$DIGEST_DIR/semiconductors.md"
```

## Writing style

Same as the morning briefing — see `.claude/skills/daily-briefing.md` Step 5 for the full banned-pattern list. Intelligence-briefing tone, data-forward, source-attributed, no AI tells.

## Tools needed

- Bash with `curl` (local API)
- Bash with `python3` (data parsing)
- WebSearch (industry research, earnings data, trade policy)
- Read (existing files if needed)
