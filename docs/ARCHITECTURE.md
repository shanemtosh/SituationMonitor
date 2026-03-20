# Situation Monitor — architecture (v0)

## Goals

- **Implementation language: Go** (single binary, straightforward HTTP + SQLite + scheduled jobs).
- Run as a normal process on one machine; reach it over Tailscale.
- Reduce manual scanning of news and X by aggregating **story cards**, **scheduled “situation” sweeps**, and **rate-limited alerts**.
- **OpenRouter (Grok family)** for agentic / search-backed topic hunting (budget for API costs).
- **Translation locally** (this machine is powerful enough for an LLM or dedicated MT stack).

## High-level shape

```
┌─────────────────────────────────────────────────────────────┐
│  Scheduler (robfig/cron or time.Ticker in-process)         │
└──────────────┬──────────────────────────────┬───────────────┘
               │                              │
               ▼                              ▼
┌──────────────────────────┐    ┌────────────────────────────┐
│  OpenRouter sweep        │    │  Structured feeds (opt.) │
│  Grok + search tools     │    │  RSS, market APIs, etc.    │
│  → JSON “situation” blob │    │  → normalized rows        │
└────────────┬─────────────┘    └─────────────┬──────────────┘
             │                                │
             └────────────┬───────────────────┘
                          ▼
              ┌───────────────────────┐
              │  Store (SQLite first) │
              │  dedupe / cluster     │
              └───────────┬───────────┘
                          ▼
              ┌───────────────────────┐
              │  Local translator     │
              │  (only if needed)     │
              └───────────┬───────────┘
                          ▼
              ┌───────────────────────┐
              │  Web UI + alerts      │
              └───────────────────────┘
```

## 1. OpenRouter + Grok as the “hunter”

**Role:** Periodic **situation sweeps** — you pass a structured brief (topics, regions, entities, what to ignore, time window). The model uses **tool-backed search** (OpenRouter/xAI expose search with **per-search pricing** on many Grok routes — budget separately from tokens).

**Contract:** Force **JSON output** (or JSON in a fenced block you parse) with stable fields, e.g.:

- `stories[]`: title, one-line summary, why it matters (to your brief), urgency 1–5, primary region, tags
- `sources[]`: URL, title, published time if known, is_x_or_social boolean
- `x_angle`: optional bullets — “what’s noisy on X about this” only when search actually surfaced X-native discussion

**Operational rules:**

- **Idempotency:** hash `(canonical_url || title+date)` before insert; merge into existing clusters.
- **Cost control:** fixed sweep schedule (e.g. every 30–60 min for “breaking”, 2×/day for deep pass), max tool calls per run, hard token cap per run.
- **Verification:** treat model output as **pointers**; store links; optionally fetch page title/snippet via HTTP for sanity (separate from LLM).

**X vs native X API:** Grok’s search tools (via OpenRouter) are **not guaranteed to be “full X firehose”** the way enterprise X API is. They are usually strong for **“what people are saying / what’s trending in public discourse”** when search includes social-indexed content. **Validate** on your first week: compare hits to what you see manually for 2–3 test queries. If gaps matter, add RSS + one more ingestor later — don’t bet the whole product on a single path.

**Model choice (iterate):** start with a **fast** Grok for frequent sweeps; optionally a **heavier** pass daily for synthesis. Swap model IDs in config without code changes.

## 2. Local translation

**Policy:** Detect language on ingest; translate **title + short summary** for display when `lang ∉ your_reading_set`.

**Options (pick one primary):**

| Approach | Notes |
|----------|--------|
| **Ollama + multilingual LLM** | Flexible, same stack as other local NLP; slightly heavier. |
| **Dedicated MT** (e.g. Marian / NLLB via `ctranslate2`, Argos) | Fast, cheap CPU/GPU; less “rewrite-y” than chat models. |

**Storage:** keep `original_title`, `translated_title`, `translator_model`, `translated_at`.

## 3. No Docker — process model

- **Stack:** **Go** — single static binary, `database/sql` + `modernc.org/sqlite` (pure Go, no CGO).
- **Dev:** `go run ./cmd/situation-monitor` or `go build -o situation-monitor ./cmd/situation-monitor && ./situation-monitor`.
- **Prod on your box:** `systemd --user` unit: `Restart=always`, env file for secrets (`OPENROUTER_API_KEY`, DB path).
- **Tailscale:** bind HTTP server to `100.x.y.z` or `0.0.0.0` with firewall rules so only Tailscale interface is exposed; or use `tailscale serve`.

## 4. Secrets

- OpenRouter key: environment only, never committed.
- Optional: local Ollama URL, model names in config file (non-secret).

## 5. MVP build order

1. Config + SQLite schema for `items` / `sweeps` / `clusters`.
2. **RSS/Atom ingest** from `config/feeds.txt` → `items`.
3. **OpenRouter sweep** → `items` (`source_kind=sweep`) + `sweeps` history + JSON response retained (truncated).
4. **Dashboard** at `/` + JSON: `/api/items`, `/api/markets`, `/api/sweeps`.
5. **Ollama translation** worker for `title_translated` / `summary_translated`.
6. **ntfy** alerts for sweep items with urgency ≥ `ALERT_MIN_URGENCY`, rate-limited per hour.
7. **Yahoo v7** quote snapshots → `market_quotes` (unofficial; may be blocked).

## 6. International news ingestion (implemented: RSS)

**We are not “scraping” article HTML.** Outlets publish **RSS and Atom** feeds; those are the stable, polite way to pull headlines and links (and often a summary or full text depending on the feed). You curate **URLs in `config/feeds.txt`** — mix US, global, and non‑English feeds as you like.

- **Dedup:** `external_id` is `g:<guid>` when the item has a GUID, else `u:<sha256(url)>` so the same URL from overlapping feeds collapses to one row.
- **Provenance:** `feed_url` stores which feed line produced the row.
- **Polling:** In-process ticker (`RSS_POLL_SEC`, default 900). Set `RSS_POLL_SEC=0` to disable.
- **Translation / language:** Not wired yet; `lang` stays empty until detection + local MT/LLM.

For paywalled or non‑syndicated pages, options later are **official APIs**, **aggregator APIs** (where licensed), or **readability-style fetch** only where terms allow — still not generic “scrape every homepage.”

## 7. Open questions (close during implementation)

- Exact OpenRouter model IDs and whether your chosen model’s **search** includes the surfaces you care about for “X-like” signal.
- Reading languages list and “never translate” allowlist (proper nouns, tickers).
- Which non‑English RSS feeds to add once translation is hooked up.
