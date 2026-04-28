# Evening Briefing Update

Generate a short delta update appended to today's existing morning briefing. Same delta-only scope as the midday update, but at the end of the US trading day — capture US close moves and after-hours news that didn't make midday.

## When to use

Invoked via `/evening-briefing` or by the systemd timer at 17:30 CT. Produces an entry appended to today's `data/pages/YYYY-MM-DD.yaml` under the top-level `updates:` array. The Go server renders it as an `#evening` anchor section beneath the morning content (and below the midday section if present).

If the morning briefing for today does not yet exist, **stop** — there is nothing to update. Tell the user no morning briefing was found and exit cleanly.

## Smoke-test mode

If the environment variable `SMOKE_TEST=1` is set when you start, log every API call, file read, and file write you would make but DO NOT actually POST or write anything.

## Instructions

### Step 0: Read today's briefing

```bash
DATE=$(date +%Y-%m-%d)
PAGES_DIR=/home/shane/Code/SituationMonitor/data/pages
BRIEFING_PATH="$PAGES_DIR/$DATE.yaml"
```

If `$BRIEFING_PATH` does not exist, stop. Otherwise read it for context.

Also read the existing `updates:` array. The newest entry's `generated_at` is the lower bound for "what's new" — typically that's the midday entry from earlier today.

### Step 1: Determine the cutoff time

The cutoff is the most recent `updates[-1].generated_at`. If there are no prior updates, fall back to the morning generation time.

### Step 2: Fetch items since cutoff

```bash
curl -s "http://localhost:8080/api/items?hours=${HOURS_SINCE}&limit=200&min_u=2" > /tmp/evening_items.json
curl -s "http://localhost:8080/api/situations?status=active&order=activity&limit=20" > /tmp/evening_sits.json
curl -s "http://localhost:8080/api/markets" > /tmp/evening_markets.json
```

Markets matter at the evening slot specifically — note any sharp closes (>2% moves on tracked instruments). Don't reproduce the full markets table; one sentence on the day's notable closes belongs in the headline or a single story body if relevant.

### Step 3: Identify what's actually new

Same delta filters as midday:

1. Drop noise (below urgency 3) unless it represents a new development on a tracked situation.
2. Drop re-runs of midday content unless materially advanced.
3. Keep breaking news from the afternoon / US close.
4. Keep material updates with new facts.
5. Keep notable shifts in tracked situations.

If zero new content, write `headline: "No material developments since the midday update."` and `stories: []`. Do not pad.

### Step 4: Web search for top developments

Brief verification + primary-source links per story. Don't dwell — this is supplemental.

### Step 5: Write the YAML delta — atomic append

Schema for the new entry:

```yaml
- slot: evening
  generated_at: "YYYY-MM-DDTHH:MM:SSZ"   # UTC, ISO8601
  headline: "One sentence summarizing the afternoon / market close."
  stories:
    - title: "Short headline"
      urgency: 4
      body: >
        Two to four sentences. New information only.
      why: >
        Optional one-liner.
      sources:
        - name: NPR
          url: https://example.com/article
```

**Limit:** 2-4 stories total.

Append using the same atomic temp-file + rename pattern as the midday skill:

```python
import yaml, pathlib, tempfile, os
path = pathlib.Path(BRIEFING_PATH)
data = yaml.safe_load(path.read_text())
data.setdefault("updates", []).append(NEW_ENTRY)
fd, tmp = tempfile.mkstemp(dir=str(path.parent), suffix=".yaml.tmp")
os.close(fd)
pathlib.Path(tmp).write_text(yaml.safe_dump(data, sort_keys=False, allow_unicode=True))
os.replace(tmp, str(path))
```

Validate:
```bash
curl -s -o /dev/null -w "%{http_code}\n" "http://localhost:8080/daily/$DATE"
```

### Step 6: Confirm

Tell the user the update is live at `http://localhost:8080/daily/$DATE#evening`.

## Writing style

Same as the morning briefing — read `.claude/skills/daily-briefing.md` Step 5 for the full list. No banned phrases, no formulaic transitions, vary paragraph length, lead with the change.

## Tools needed

- Bash with `curl` and `python3`
- WebSearch
- Read
