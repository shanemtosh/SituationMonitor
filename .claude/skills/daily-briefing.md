# Daily Briefing Generator

Generate a daily situation briefing for the SituationMonitor web app.

## When to use

Invoked via `/daily-briefing` or on a recurring schedule. Produces a YAML data file that the Go server renders through an HTML template.

## Instructions

### Step 1: Gather data from local APIs

Fetch from the running SituationMonitor at `http://127.0.0.1:8080`:

1. **Items** — `WebFetch` → `http://127.0.0.1:8080/api/items?hours=24&limit=200`
2. **Markets** — `WebFetch` → `http://127.0.0.1:8080/api/markets`
3. **Sweeps** — `WebFetch` → `http://127.0.0.1:8080/api/sweeps?limit=10`

If the server is not reachable, tell the user and stop.

### Step 2: Identify top stories and themes

From the collected items, identify:
- **5–8 top stories** weighted by urgency, source diversity, and recency
- **2–3 emerging themes** connecting multiple items
- **Notable X/social signals** from sweep data

### Step 3: Web search for depth

For each top story and theme, use `WebSearch` to:
- Get latest developments (stories evolve after RSS pickup)
- Find additional context and primary sources
- Verify claims with multiple perspectives
- Catch breaking news the feeds missed

### Step 4: Write the YAML briefing

Write a YAML file to `data/pages/YYYY-MM-DD.yaml`. The Go server renders it at `/daily/YYYY-MM-DD`.

**YAML schema** — follow this structure exactly:

```yaml
date: "YYYY-MM-DD"
weekday: Friday
summary: >
  3–5 sentence executive summary. Lead with the most significant development.

markets:
  narrative: >
    Brief paragraph on market dynamics and drivers.
  instruments:
    - name: Brent Crude
      price: "$110.95"
      move: "+3.3%"
      direction: neg    # neg | pos | neutral
    - name: SPY (S&P 500)
      price: "$556"
      move: "-0.57%"
      direction: neg

stories:
  - title: Short headline
    urgency: 5          # 5=CRIT 4=HIGH 3=MOD 2=LOW 1=INFO
    body: >
      Main paragraph of analysis.
    body2: >
      Optional second paragraph for additional detail.
    why: >
      Why it matters — significance and implications.
    sources:
      - name: NPR
        url: https://example.com/article

themes:
  - title: Theme name
    body: >
      Analysis connecting multiple stories.

social: >
  X/Twitter signals, trending topics, sentiment.

watchlist:
  - title: Thing to watch
    body: >
      Why and what to look for.

all_sources:
  - name: NPR
    title: Full article title
    url: https://example.com/article
```

**Writing style:**
- Intelligence-briefing tone: concise, analytical, direct
- Lead with significance and implications, not just events
- Include "why it matters" for each major story
- No fluff, no hedging — be direct about assessments and uncertainties

### Step 5: Confirm

Tell the user the briefing is ready at `http://127.0.0.1:8080/daily/YYYY-MM-DD`.

Reference `data/pages/2026-03-20.yaml` as the canonical example of correct structure.

## Tools needed

- WebFetch (local APIs)
- WebSearch (context and verification)
- Write (save YAML file)
- Read (check existing briefings if needed)
