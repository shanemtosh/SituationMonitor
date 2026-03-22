# Daily Briefing Generator

Generate a daily situation briefing for the SituationMonitor web app.

## When to use

Invoked via `/daily-briefing` or on a recurring schedule. Produces a YAML data file that the Go server renders through an HTML template.

## Instructions

### Step 0: Curate the knowledge graph

Before gathering data, clean up entities and situations so the briefing has accurate context.

**0a. Review situations for duplicates and hierarchy**

```bash
curl -s "http://localhost:8080/api/situations?status=active&tree=true" | python3 -m json.tool
```

Look for:
- **Duplicate situations** that describe the same event (e.g. "Hormuz standoff" and "straits of hormuz") — merge them:
  ```bash
  curl -s -X POST http://localhost:8080/api/situations/merge -d '{"from_id": FROM, "to_id": TO}'
  ```
- **Missing parent relationships** (e.g. "Diego Garcia attack" should be a child of "Iran-US conflict") — set parent:
  ```bash
  curl -s -X POST http://localhost:8080/api/situations/ID/parent -d '{"parent_id": PARENT_ID}'
  ```
- **Bad names** — rename:
  ```bash
  curl -s -X POST http://localhost:8080/api/situations/ID/rename -d '{"name": "Better Name"}'
  ```
- **Resolved situations** — mark as resolved:
  ```bash
  curl -s -X POST http://localhost:8080/api/situations/ID/status -d '{"status": "resolved"}'
  ```
- **Junk situations** (celebrity gossip, etc.) — delete:
  ```bash
  curl -s -X DELETE http://localhost:8080/api/situations/ID
  ```

**0b. Review top entities for junk or duplicates**

```bash
curl -s "http://localhost:8080/api/entities?limit=50" | python3 -c "import json,sys; [print(f'{e[\"item_count\"]:3d}x [{e[\"kind\"]:6s}] id={e[\"id\"]} {e[\"name\"]}') for e in json.load(sys.stdin)]"
```

Look for:
- **Duplicate entities** (same person/org, different name form) — merge:
  ```bash
  curl -s -X POST http://localhost:8080/api/entities/merge -d '{"from_id": FROM, "to_id": TO}'
  ```
- **Wrong kind** (country listed as ORG instead of PLACE) — the auto-normalizer should catch most of these, but you can rename if needed
- **Junk entities** (generic terms like "authorities", "residents") — delete:
  ```bash
  curl -s -X DELETE http://localhost:8080/api/entities/ID
  ```

Use your judgment and web search context to decide what's a real entity vs noise. Spend at most 2-3 minutes on this — it's housekeeping, not the main task. Focus on high-count entities and situations that will appear in today's briefing.

### Step 1: Gather data from local APIs

Use `curl` via the Bash tool to fetch from the running SituationMonitor (the `WebFetch` tool cannot connect to localhost):

1. `curl -s "http://localhost:8080/api/items?hours=24&limit=300" > /tmp/sm_items.json`
2. `curl -s "http://localhost:8080/api/markets" > /tmp/sm_markets.json`
3. `curl -s "http://localhost:8080/api/sweeps?limit=10" > /tmp/sm_sweeps.json`
4. `curl -s "http://localhost:8080/api/situations?status=active&limit=20" > /tmp/sm_situations.json`

Then use python3 to parse and analyze the JSON (the items response is typically 300+ KB).

For any situation that appears relevant to today's top stories, fetch its timeline:
- `curl -s "http://localhost:8080/api/situations/{slug}" > /tmp/sm_sit_{slug}.json`

This gives you the full item history for that evolving story — use it to describe how the situation has developed over time.

For prominent entities (people, orgs, places) that appear across multiple stories, you can optionally check:
- `curl -s "http://localhost:8080/api/entities/{name}"` — returns item count, first/last seen, and recent coverage

If the server is not reachable, tell the user and stop.

### Step 2: Process the global feed data

The items API returns data from **19 sources** across multiple regions, languages, and industries:

- **Western**: BBC, NYT, Guardian, Al Jazeera
- **East Asia**: NHK (Japanese, auto-translated), Nikkei Asia, Yonhap, Korea Herald, CGTN, Taipei Times
- **South/SE Asia**: Hindustan Times, SCMP
- **Semiconductors & Memory**: DigiTimes (Taiwan supply chain), TrendForce (DRAM/NAND/foundry), EE Times, SemiEngineering, SemiAnalysis, KED Global (Samsung/SK Hynix/Korea industry)
- **Supply Chain**: Supply Chain Dive
- **X/Social**: Grok sweep items (source_kind=sweep)

**Important**: Each item has both original and translated fields:
- `title` — original headline (may be in Japanese, Korean, etc.)
- `title_translated` — English translation (if available)
- `summary` — original RSS snippet
- `summary_translated` — English translation of summary
- `lang` — detected source language (ja, zh, ko, en, etc.)
- `feed_url` — which feed it came from

**Always prefer translated fields** when available. Use `title_translated` over `title` and `summary_translated` over `summary` for non-English items.

**Actively look for non-Western perspectives**: stories from NHK, Yonhap, CGTN, SCMP, and Hindustan Times often carry angles that BBC/NYT don't. When a story appears in both Western and Asian sources, note where the framing differs — this is valuable intelligence.

### Step 3: Identify top stories and themes

From the collected items, identify:
- **5–8 top stories** weighted by urgency, source diversity, recency, AND geographic breadth
- **2–3 emerging themes** connecting multiple items across regions
- **Industry signals** from semiconductor and supply chain feeds — TSMC capacity, memory pricing, fab investments, supply disruptions
- **Notable X/social signals** from sweep data (source_kind=sweep)
- **Situation timelines** — if an active situation exists for a story, use its item history to describe how it evolved. Reference when the story first appeared, how many sources covered it, and what changed
- **Regional divergences** — when Asian and Western sources frame the same event differently, call it out

Prioritize stories that appear across multiple regional sources — if NHK, BBC, and Al Jazeera all cover something, it's globally significant. A story only appearing in one regional source may still be important as a local signal.

**Industry coverage is first-class**: DigiTimes and TrendForce carry signals about chip supply, memory pricing, and fab capacity that move markets and affect geopolitics. When a TrendForce memory price report or a DigiTimes TSMC capacity story appears, include it — these are early indicators that mainstream sources pick up days later.

### Step 4: Web search for depth

For each top story and theme, use `WebSearch` to:
- Get latest developments (stories evolve after RSS pickup)
- Find additional context and primary sources
- Verify claims with multiple perspectives
- Catch breaking news the feeds missed
- Search for regional context on stories from non-Western sources

### Step 5: Write the YAML briefing

Write a YAML file to `data/pages/YYYY-MM-DD.yaml`. The Go server renders it at `/daily/YYYY-MM-DD`.

**YAML schema** — follow this structure exactly:

```yaml
date: "YYYY-MM-DD"
weekday: Friday
summary: >
  3–5 sentence executive summary. Lead with the most significant development.
  Mention if key insights came from non-Western sources.

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
      If a situation timeline exists for this story, describe how it has
      evolved: when it first appeared, how coverage changed, what's new today.
    sources:
      - name: NPR
        url: https://example.com/article
      - name: NHK        # include non-Western sources when relevant
        url: https://example.com/article

themes:
  - title: Theme name
    body: >
      Analysis connecting multiple stories across regions.

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

**YAML safety — avoid parse errors:**
- **Quotes in titles**: If a title contains quotation marks, wrap the entire value in double quotes and use single quotes inside (e.g. `title: "'Winding down' vs. reality"`). Unescaped quotes in bare YAML values will cause parse failures and a 500 error on the server.
- **Colons in values**: Values containing `:` followed by a space must be quoted.
- **Special characters**: `#`, `&`, `*`, `!`, `|`, `>`, `{`, `}`, `[`, `]` at the start of a value need quoting.
- **After writing, validate** by curling the page: `curl -s -o /dev/null -w "%{http_code}" "http://localhost:8080/daily/YYYY-MM-DD"` — expect 200. If 500, fix the YAML.

**Writing style:**
- Intelligence-briefing tone: concise, analytical, direct
- Lead with significance and implications, not just events
- Include "why it matters" for each major story
- When citing non-English sources, credit them (e.g. "per NHK reporting" or "according to Yonhap")
- Note when Western and Asian coverage diverges on framing or emphasis
- No fluff, no hedging — be direct about assessments and uncertainties

### Step 6: Confirm

Tell the user the briefing is ready at `http://127.0.0.1:8080/daily/YYYY-MM-DD`.

Reference `data/pages/2026-03-20.yaml` as the canonical example of correct structure.

## Tools needed

- Bash with `curl` (local API fetches — WebFetch cannot reach localhost)
- Bash with `python3` (parsing large JSON responses)
- WebSearch (context and verification)
- Write (save YAML file)
- Read (check existing briefings if needed)
