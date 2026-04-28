# SituationMonitor Enhancements — Design Spec

**Date:** 2026-04-27
**Status:** Draft, pending user review

## Overview

Three independent enhancements to SituationMonitor, all reusing the existing
"systemd timer → shell script → Claude CLI with skill prompt" pattern that the
morning daily-briefing already uses. The morning briefing pipeline is **not
modified** — these features layer on top.

1. **Midday & evening briefing updates** — supplemental delta updates appended
   to the morning briefing page, each with their own audio.
2. **Situations view** — a standalone list page with Ollama-generated rolling
   snippets per situation and a click-through detail page showing the full
   item feed for a given situation.
3. **Weekly alpha refresh** — a Sunday-morning combined Claude job that runs
   all four alpha skills, refreshes assessments, prunes stale entries, and
   writes a per-domain weekly digest archived under `/alpha/digests`.

## Motivation

- The morning briefing is once-a-day; intraday breaking news has nowhere to
  surface in the existing UI.
- The alpha tab depends on manual skill invocation; without a recurring
  cadence it goes stale and degrades from "forecasting tool" to "graveyard
  of old assessments."
- The knowledge graph already tracks situations, but there is no UI surface
  optimized for browsing them by activity — the dashboard surfaces items,
  not situations, and the daily briefing is too high-level.

## Goals

- Same Claude-CLI pattern as the morning briefing for any text-generation
  job. Each new automated job is a `scripts/*.sh` driver + a
  `.claude/skills/*.md` prompt + a `~/.config/systemd/user/*.{timer,service}`
  pair.
- Additive schema changes only. No backfill required.
- Each feature independently shippable and reversible.

## Non-Goals

- Push notifications.
- RSS feed of briefings.
- Diff view between weekly digests.
- Automatic linking of new alpha assessments back to the daily briefing's
  stories.
- Modifying the morning daily-briefing skill or its outputs in any way.

---

## Feature 1: Midday & Evening Briefing Updates

### Behavior

Two additional Claude-CLI jobs run daily after the morning briefing:
**midday at 11:30 CT**, **evening at 17:30 CT**. Each appends a "delta"
update to the morning briefing's YAML file rather than producing a new page.

The delta is **pure update content** — only new items / breaking news since
the last slot. No re-summarization of morning topics unless they materially
evolved. No markets section, no themes, no `all_sources`. Just a one-line
headline plus 2-4 short story blocks.

Each slot also gets its own short audio file generated via the existing TTS
pipeline.

### YAML Schema Extension

`data/pages/YYYY-MM-DD.yaml` gains an optional top-level `updates` array.
The morning generator never writes this field; only the new delta generators
do. Existing pages remain valid.

```yaml
date: "2026-04-27"
weekday: Monday
summary: ...        # morning content unchanged
markets: ...
stories: [...]
themes: [...]
# ... rest of morning structure unchanged ...

updates:
  - slot: midday      # midday | evening
    generated_at: "2026-04-27T16:30:00Z"   # UTC ISO8601
    headline: "One-line summary of what's new since this morning"
    stories:
      - title: ...
        body: ...
        sources:
          - name: ...
            url: ...
    audio: "2026-04-27-midday.mp3"   # populated by audio post-step
```

### New Components

- `scripts/midday-briefing.sh` and `scripts/evening-briefing.sh` — same
  retry-with-backoff shape as `scripts/daily-briefing.sh`. Each reads its
  skill file and runs Claude CLI with the same allowed-tools set.
- `.claude/skills/midday-briefing.md` and `.claude/skills/evening-briefing.md`
  — delta-mode prompts. Each skill:
  1. Reads the existing `data/pages/YYYY-MM-DD.yaml` for context (what
     was covered earlier today).
  2. Fetches items from `/api/items` since the last `updates[-1].generated_at`,
     falling back to the morning's effective generation time when no prior
     update exists.
  3. Identifies breaking news and material updates to existing situations.
  4. **Appends** an entry to the `updates` array via atomic write
     (temp file + rename). Never rewrites morning fields.
  5. Headline plus 2-4 short story blocks max.
  6. Inherits the daily-briefing writing-style and banned-words list by
     including a directive: "Read `.claude/skills/daily-briefing.md` and
     follow its writing style and banned-patterns sections."
- `scripts/generate-update-audio.sh {date} {slot}` — variant of the existing
  audio script that produces `data/pages/YYYY-MM-DD-{slot}.mp3`. Triggered
  via `OnSuccess=` on each briefing service.
- Two new systemd user units:
  - `midday-briefing.timer` (`OnCalendar=*-*-* 11:30:00`)
  - `midday-briefing.service` (oneshot, runs the script,
    `OnSuccess=midday-briefing-audio.service`)
  - `evening-briefing.timer` (`OnCalendar=*-*-* 17:30:00`)
  - `evening-briefing.service` (oneshot, `OnSuccess=evening-briefing-audio.service`)
- Two corresponding audio services that wrap `generate-update-audio.sh`.

### Server Changes

- `handleDailyPage` already loads the YAML; render `updates` as anchor
  sections (`#midday`, `#evening`) below the morning content. When `updates`
  is non-empty, add a "jump to" link bar directly below the morning audio
  player.
- New route: `GET /daily/{date}/{slot}/audio` — serves
  `data/pages/YYYY-MM-DD-{slot}.mp3`. Mirror of the existing
  `/daily/{date}/audio` handler.
- Dashboard (`/`) home link: when today's YAML has `updates`, the
  daily-briefing nav link displays a small badge such as
  "Briefing · midday" or "Briefing · midday + evening".
- `handleDailyIndex` (`/daily/`) shows small `+M` and `+E` markers next to
  dates whose YAML contains those update slots.

### Race Safety

Midday and evening cannot run concurrently (different times); morning is done
by 11:30. YAML writes are atomic via temp file + rename. Server reads the
file fresh on each request — no in-memory cache.

---

## Feature 2: Situations View

### Behavior

A standalone page (`/situations`) lists active situations ordered by recent
item activity, each with a short Ollama-generated rolling snippet describing
the latest state. Clicking into a situation goes to a detail page
(`/situations/{slug}`) showing the snippet plus the full chronological item
feed for that situation.

Snippets refresh hourly via a new background worker, with overhead controls.

### Schema Change

Two columns added to the `situations` table:

```sql
ALTER TABLE situations ADD COLUMN snippet TEXT NOT NULL DEFAULT '';
ALTER TABLE situations ADD COLUMN snippet_generated_at TEXT;
```

Both nullable / empty by default. Existing rows get `''` / `NULL` until the
snippet worker first hits them. No backfill needed.

### New Worker

`internal/snippet/worker.go` — runs in the main process alongside the
existing extract / RSS / translate / market workers. On a configurable tick
(default `1h`):

1. Query active situations ordered by last item activity (`MAX(linked_at)`
   from `situation_items`), capped at `SITUATION_SNIPPET_TOP_N` (default 30).
2. For each, skip if `MAX(linked_at) <= snippet_generated_at` — no new items
   since the last snippet, nothing to regenerate.
3. Pull the 5-10 most recent items linked to that situation.
4. Call Ollama with a tight prompt:
   *"Given these recent headlines about {situation_name}, write 1-2 sentences
   describing the latest state of this situation. No editorializing."*
5. Write `snippet` and `snippet_generated_at` back.
6. Sleep 60s between situations to spread load — never bursts more than one
   Ollama call per minute.

Wired into `cmd/situation-monitor/main.go` next to the other workers.
Disabled by setting `SITUATION_SNIPPET_ENABLED=false`.

### Ollama Function

Add `internal/ollama/situation_snippet.go` exposing
`GenerateSituationSnippet(ctx, model, situationName, items) (string, error)`.
Reuses the existing nemotron-mini configuration; model can be overridden
with `OLLAMA_SNIPPET_MODEL`.

### API Changes

- `GET /api/situations?status=active&order=activity&limit=` — extend the
  existing handler:
  - New `order` parameter accepts `activity` (orders by most recent
    `linked_at` descending). Default order remains `updated_at DESC`.
  - JSON response includes `snippet` and `snippet_generated_at`.
- `GET /situations` (HTML) — new page, renders the list.
- `GET /situations/{slug}` (HTML) — new page, renders detail view.

### UI

- `/situations` — list page. Each row: situation name (link), snippet (or
  `—` if not yet generated), item count, last activity (`3h ago`),
  child-count indicator if the situation has sub-situations. Sort dropdown:
  Activity / Item Count / Recently Created.
- `/situations/{slug}` — detail page. Header: name + status pill + parent
  breadcrumb if any. Snippet block. Then the chronological feed of items
  (reusing the dashboard item card style — title, source, time, summary,
  urgency). Sidebar: top entities, child situations, "view in alpha" link
  if a `net_assessment` exists for this situation.
- New nav entry `Situations` between `Briefings` and `Alpha` in the existing
  nav strip across all templates.

---

## Feature 3: Weekly Alpha Refresh

### Behavior

A single combined Claude job runs Sunday at 02:00 CT. It executes all four
alpha skills in sequence (geopolitics → macro → semiconductors → energy),
refreshing assessments, pruning stale entries, and writing a per-domain
weekly digest. Updates flow into the existing `/alpha` UI; digests are
archived under `/alpha/digests`.

### New Components

- `scripts/weekly-alpha.sh` — driver script. Same retry-with-backoff shape
  as `scripts/daily-briefing.sh` (3 attempts, 5-min delay). The prompt is a
  short wrapper rather than a single skill:

  ```
  You are running a weekly combined alpha refresh. The current ISO week is YYYY-Www.
  Execute these four skills in sequence, in order:
    1. .claude/skills/geopolitical-alpha.md
    2. .claude/skills/macro-alpha.md
    3. .claude/skills/semiconductors-alpha.md
    4. .claude/skills/energy-alpha.md
  Read each skill file, complete its full instructions, then move to the next.
  Between skills, log "=== begin {domain} ===" and "=== end {domain} ===" so
  progress is greppable in journalctl.
  ```

- New systemd units:
  - `weekly-alpha.timer` (`OnCalendar=Sun *-*-* 02:00:00`, `Persistent=true`)
  - `weekly-alpha.service` (oneshot, `TimeoutStartSec=14400` for a 4-hour
    ceiling on the combined run)

### Skill Edits (the four existing alpha skills)

Each of `geopolitical-alpha.md`, `macro-alpha.md`, `semiconductors-alpha.md`,
`energy-alpha.md` gets:

- **"Weekly mode preamble"** at the top:
  > "When invoked as part of a weekly run with no specific event trigger,
  > default behavior is: scan all active situations and assessments in this
  > domain for material changes since last week (compare against last week's
  > digest if present), update any that moved, create assessments for
  > situations that crossed the threshold for needing one, then perform
  > stale cleanup and write the weekly digest."

- **"Step N: Stale assessment cleanup"** — query
  `/api/assessments?domain={d}&status=active`, walk each. If it hasn't
  received a `probability_update` in 60+ days OR its underlying situation
  is `resolved`, mark it `resolved` via `PUT /api/assessments/{id}` with
  `{"status": "resolved"}`. Resolved calendar events older than 90 days
  get deleted. Brief reason logged.

- **"Step N: Weekly digest"** — write a markdown file to
  `data/alpha/digests/YYYY-Www/{domain}.md`. Schema:

  ```markdown
  # {Domain} — Week {YYYY-Www}
  Generated: {timestamp}

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
  {2-3 sentence forward look}
  ```

Additionally:

- **`geopolitical-alpha.md` schema fix:** add `"domain": "geopolitics"` to
  the POST examples that currently omit it (macro/semi/energy already
  include it).
- **Writing-style backstop** in `macro-alpha.md`, `semiconductors-alpha.md`,
  `energy-alpha.md`: add a one-liner — "Writing style and banned-patterns:
  see `.claude/skills/daily-briefing.md` Step 5. Apply the same rules here."

### New Routes & UI

- `GET /alpha/digests` — index page. Reads `data/alpha/digests/`, lists weeks
  (`2026-W17`, `2026-W16`, …) sorted descending.
- `GET /alpha/digests/{week}` — single-week page. Reads all four
  `{domain}.md` files for that week, renders them as four collapsible
  sections. Markdown rendered via `goldmark` (add to deps if not present).
- New `Digests` link in the `/alpha` landing page header.

### Storage

`data/alpha/digests/YYYY-Www/{geopolitics,macro,semiconductors,energy}.md`.
Add `data/alpha/` to `.gitignore` (matches the existing handling of
`data/pages/`).

---

## Cross-Cutting

### Configuration

New env vars added to `internal/config/config.go`:

| Var | Default | Notes |
|---|---|---|
| `SITUATION_SNIPPET_INTERVAL` | `1h` | snippet worker tick |
| `SITUATION_SNIPPET_TOP_N` | `30` | cap per cycle |
| `SITUATION_SNIPPET_ENABLED` | `true` | kill switch |
| `OLLAMA_SNIPPET_MODEL` | falls back to `OLLAMA_MODEL` | per-purpose override |

### Schema Migration

The project already declares a `schema_migrations` table. The current
schema block in `internal/db/db.go` uses inline `CREATE TABLE IF NOT
EXISTS`; the migration table appears unused so far. For these two new
columns, use `PRAGMA table_info(situations)` to detect whether each column
exists, and `ALTER TABLE` only when missing. This matches the existing
"safe to run repeatedly" pattern of the schema block. Migration tracking
via `schema_migrations` can be introduced separately if the project
later needs ordered, complex migrations — out of scope here.

### Error Handling

- **Briefing skill fails:** existing 3× retry handles transient API/Ollama
  hiccups. After 3 failures, systemd marks the service failed and
  journalctl captures it. **No silent fallback** — an absent
  `updates[].midday` entry on the page is the user-visible signal that
  midday didn't run. No placeholder.
- **YAML append race:** atomic write (write to `*.yaml.tmp`, rename).
  Server reads file fresh on each request.
- **Ollama down for snippets:** worker logs the error and skips that
  situation for the cycle. Existing snippet stays. After 3 consecutive
  cycle failures for the *same* situation, log a warning but keep trying.
- **Weekly alpha hits API rate limits / Reef Data outage:** skill-level
  reporting in the digest as "data unavailable, skipped X." No
  script-level retry beyond the standard 3.
- **Audio generation fails:** existing `briefing-audio.service` pattern
  — `OnFailure` doesn't roll back the briefing; audio just stays missing
  and the page renders without an audio player.

### Observability

- All systemd units inherit existing journalctl pattern
  (`journalctl --user -u <unit>`).
- Snippet worker logs one line per cycle:
  `snippet: refreshed N/30 situations in Xs (skipped Y as fresh)`.
- Weekly alpha's "begin/end" log markers between domains make grep easy.

### Testing

- **Unit tests:** Ollama snippet prompt builder, YAML append-update
  helper (must round-trip without losing fields), situations sort/filter
  SQL.
- **Integration:** spin up SQLite + run snippet worker for one tick
  against a fixture DB; assert snippets written.
- **Manual smoke:** each new skill prompt includes a directive at the
  top — *"If the environment variable `SMOKE_TEST=1` is set when you
  start, log every API call and file write you would make but do not
  actually POST or write."* The driver scripts honor this by passing the
  env through to the Claude CLI. Run with `SMOKE_TEST=1
  scripts/midday-briefing.sh` before enabling timers.
- **Skill content itself is not tested** — that's prompt engineering,
  validated by reading actual outputs after the first real run.

### Deployment

Per `reference_deployment.md`, SituationMonitor runs as a systemd user
service on a VPS, served at situation.mto.sh via nginx. Deploy by commit /
push / rebuild / restart.

For these features:

- New systemd unit files added under `~/.config/systemd/user/` on the VPS;
  `systemctl --user daemon-reload` then `systemctl --user enable --now` for
  each timer after deploy.
- Schema migration runs automatically on next start.
- New scripts checked into `scripts/`, made executable, paths absolute
  (mirror `daily-briefing.sh`).
- `claude` CLI must be present on the VPS at `/home/shane/.local/bin/claude`
  (already true — used by the existing daily briefing).

### Build & Rollout Sequence

For the implementation plan to follow, in this order:

1. **Schema + situations view.** Additive DB column, new worker, new
   routes/templates. Smallest blast radius. Ship and verify snippets
   generating + page renders.
2. **Midday/evening briefings.** YAML schema extension, two new skills,
   two new scripts, two new timer/service pairs, server template updates
   for `updates` rendering + audio routes. Ship one slot first (midday),
   verify, then evening.
3. **Weekly alpha refresh.** Skill edits (stale cleanup, weekly mode,
   digest, writing-style fixes), wrapper script, timer, digest UI. Run
   manually first to validate digest output before enabling the timer.

Each phase is independently shippable and reversible.
