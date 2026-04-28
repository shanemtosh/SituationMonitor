# Midday Briefing Update

Generate a short delta update appended to today's existing morning briefing. This is supplemental — readers already saw the morning deep-dive. Lead with what's *new* since the morning, not a re-summarization.

## When to use

Invoked via `/midday-briefing` or by the systemd timer at 11:30 CT. Produces an entry appended to today's `data/pages/YYYY-MM-DD.yaml` under the top-level `updates:` array. The Go server renders it as a `#midday` anchor section beneath the morning content.

If the morning briefing for today does not yet exist, **stop** — there is nothing to update. Tell the user no morning briefing was found and exit cleanly.

## Smoke-test mode

If the environment variable `SMOKE_TEST=1` is set when you start, log every API call, file read, and file write you would make but DO NOT actually POST or write anything. Useful for validating the skill before wiring up systemd.

## Instructions

### Step 0: Read today's briefing

```bash
DATE=$(date +%Y-%m-%d)
PAGES_DIR=/home/shane/Code/SituationMonitor/data/pages
BRIEFING_PATH="$PAGES_DIR/$DATE.yaml"
```

If `$BRIEFING_PATH` does not exist, stop. Otherwise read it for context (top stories, themes, watchlist, social) — this is what you are *adding to*, not duplicating.

Also read the existing `updates:` array if any. The newest entry's `generated_at` is the lower bound for "what's new."

### Step 1: Determine the cutoff time

If `updates:` is empty, the cutoff is the morning generation time — use roughly the file's mtime or 06:30 CT today as a sensible default. If `updates:` already has entries, the cutoff is the most recent `generated_at`.

### Step 2: Fetch items since cutoff

Compute hours-since-cutoff (round up). Fetch via the running SituationMonitor:

```bash
HOURS_SINCE=$(python3 -c "import sys; ...")  # or compute via date arithmetic
curl -s "http://localhost:8080/api/items?hours=${HOURS_SINCE}&limit=200&min_u=2" > /tmp/midday_items.json
curl -s "http://localhost:8080/api/situations?status=active&order=activity&limit=20" > /tmp/midday_sits.json
```

Use `min_u=2` to filter low-relevance items. The order by activity surfaces situations that moved.

### Step 3: Identify what's actually new

This is a *delta* update. Apply these filters in order:

1. **Drop noise.** Anything below urgency 3 unless it represents a clear new development on a tracked situation.
2. **Drop morning re-runs.** If a story is just a fresh wire writeup of a topic the morning already covered with no new facts, skip it.
3. **Keep breaking news.** Genuinely new events (announcements, attacks, releases, decisions) since the morning.
4. **Keep material updates** to existing morning topics — only when there's *new information*, not just new framing.
5. **Keep notable shifts** in tracked situations — escalation, resolution, new actors entering.

If after filtering you have zero genuinely new items, write the update with `headline: "No material developments since the morning briefing."` and an empty `stories: []`. Do not invent content.

### Step 4: Web search for top developments

For each candidate story, briefly use `WebSearch` to verify it's still current and capture primary-source links. Don't sink time here — this is a fast update.

### Step 5: Write the YAML delta — atomic append

The file write must be safe against the server reading mid-write. Use a temp-file + rename pattern.

Construct the new entry as a YAML fragment with this schema:

```yaml
- slot: midday
  generated_at: "YYYY-MM-DDTHH:MM:SSZ"   # UTC, ISO8601
  headline: "One sentence — what's new since this morning."
  stories:
    - title: "Short headline"
      urgency: 4   # 5=CRIT 4=HIGH 3=MOD
      body: >
        Two to four sentences. New information only. Lead with the change,
        not the recap.
      why: >
        One short sentence on what shifts as a result. Optional.
      sources:
        - name: NPR
          url: https://example.com/article
```

**Limit:** 2-4 stories total. If you have more, you're not filtering hard enough — this is supplemental.

To append: read the existing YAML, parse it, append the new entry to `updates`, write to a temp file, rename atomically:

```python
import yaml, sys, datetime, pathlib, os, tempfile
path = pathlib.Path(BRIEFING_PATH)
data = yaml.safe_load(path.read_text())
data.setdefault("updates", []).append(NEW_ENTRY)
fd, tmp = tempfile.mkstemp(dir=str(path.parent), suffix=".yaml.tmp")
os.close(fd)
pathlib.Path(tmp).write_text(yaml.safe_dump(data, sort_keys=False, allow_unicode=True))
os.replace(tmp, str(path))
```

After writing, validate by curling the page:
```bash
curl -s -o /dev/null -w "%{http_code}\n" "http://localhost:8080/daily/$DATE"
```
Expect 200. If 500, your YAML is malformed — restore from the file's prior state (a quick git diff helps).

### Step 6: Confirm

Tell the user the update is live at `http://localhost:8080/daily/$DATE#midday`.

## Writing style

Same rules as the morning briefing — read `.claude/skills/daily-briefing.md` Step 5 for the full list. Highlights:

- Intelligence-briefing tone: concise, direct, analytical.
- Lead with the change, not the wind-up.
- Data-forward — anchor in numbers when available.
- No banned AI words / phrases (delve, underscore, robust, comprehensive, "It's worth noting", etc.).
- No formulaic transitions (Moreover, Furthermore, Additionally).
- Vary paragraph length.

## Tools needed

- Bash with `curl` and `python3` (local API + YAML manipulation; WebFetch can't reach localhost)
- WebSearch (verification + primary-source links)
- Read (existing YAML + this skill's references)
- Write (the temp-file rename pattern above goes through Bash, not Write, so YAML stays atomic)
