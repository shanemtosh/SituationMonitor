# Semiconductors & Technology Alpha

Perform constraint-based analysis of the semiconductor industry and technology supply chains. Forecast capacity decisions, trade policy outcomes, and technology transitions based on material constraints.

## When to use

Invoked via `/semiconductors-alpha`. Run when major industry events occur: earnings, fab announcements, export control changes, capacity data releases.

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

## Tools needed

- Bash with `curl` (local API)
- Bash with `python3` (data parsing)
- WebSearch (industry research, earnings data, trade policy)
- Read (existing files if needed)
