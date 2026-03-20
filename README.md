# Situation Monitor

Single-process Go service that aggregates **RSS/Atom headlines**, periodic **OpenRouter (Grok) situation sweeps** with structured JSON storage, optional **Yahoo Finance quotes**, **local Ollama translation**, and **ntfy** alerts for high-urgency sweep items. Intended to run at home and be reached over **Tailscale**.

## Quick start

```bash
export OPENROUTER_API_KEY=sk-or-v1-...   # enables sweeps
# optional: export NTFY_TOPIC=your-secret-topic
go run ./cmd/situation-monitor
```

Open `http://127.0.0.1:8080/` for the dashboard, or `GET /api/items`, `/api/markets`, `/api/sweeps`.

Build a binary:

```bash
go build -o situation-monitor ./cmd/situation-monitor
./situation-monitor
```

## Configuration (environment)

| Variable | Default | Purpose |
|----------|---------|---------|
| `LISTEN_ADDR` | `127.0.0.1:8080` | HTTP bind address |
| `DATABASE_PATH` | `./data/situation.db` | SQLite path |
| **OpenRouter** | | |
| `OPENROUTER_API_KEY` | — | Required for sweeps |
| `OPENROUTER_MODEL` | `x-ai/grok-4-fast` | Model id |
| `OPENROUTER_BASE_URL` | `https://openrouter.ai/api/v1` | API base |
| `OPENROUTER_JSON_OBJECT` | `true` | Set `false` if the model rejects `response_format` |
| `OPENROUTER_HTTP_TIMEOUT_SEC` | `120` | HTTP client timeout |
| `SWEEP_BRIEF_FILE` | `config/brief.txt` | Instructions injected into the sweep prompt |
| `SWEEP_POLL_SEC` | `3600` | Sweep interval; `0` disables |
| `SWEEP_ON_START` | `true` | Run one sweep at startup |
| **RSS** | | |
| `RSS_FEEDS_FILE` | `config/feeds.txt` | One feed URL per line |
| `RSS_POLL_SEC` | `900` | `0` disables RSS worker |
| `RSS_FETCH_TIMEOUT_SEC` | `45` | Per-feed HTTP timeout |
| `RSS_USER_AGENT` | `SituationMonitor/1.0 …` | Sent to feed servers |
| `RSS_INGEST_ON_START` | `true` | Ingest feeds immediately |
| **Translation (Ollama)** | | |
| `OLLAMA_BASE_URL` | `http://127.0.0.1:11434` | Ollama base |
| `OLLAMA_TRANSLATE_MODEL` | — | e.g. `llama3.2` — empty disables worker |
| `TRANSLATE_TARGET_LANG` | `English` | Target language name in the prompt |
| `TRANSLATE_POLL_SEC` | `600` | `0` disables |
| `TRANSLATE_BATCH` | `15` | Max items per pass |
| `TRANSLATE_ON_START` | `true` | Run one pass at startup |
| **Markets** | | |
| `MARKET_SYMBOLS` | — | Comma-separated tickers, e.g. `SPY,GLD,QQQ` — empty disables |
| `MARKET_POLL_SEC` | `120` | `0` disables |
| `MARKET_FETCH_TIMEOUT_SEC` | `30` | HTTP timeout |
| `MARKET_ON_START` | `true` | Fetch once at startup |
| **Alerts (ntfy)** | | |
| `NTFY_SERVER` | `https://ntfy.sh` | Self-hosted or public |
| `NTFY_TOPIC` | — | If empty, alerts are skipped |
| `NTFY_TOKEN` | — | Optional Bearer token for protected topics |
| `ALERT_MIN_URGENCY` | `4` | Minimum sweep urgency to notify |
| `ALERT_MAX_PER_HOUR` | `12` | Global cap on ntfy posts / hour |

Copy `.env.example` as a starting point. Keep secrets out of git.

## Layout

```
cmd/situation-monitor/   # main
internal/alerts/         # ntfy + rate limit
internal/brief/          # sweep brief file loader
internal/config/
internal/db/             # SQLite + migrations
internal/feeds/          # feed URL list
internal/httpserver/     # dashboard + JSON API
internal/ingest/rss/
internal/ingest/sweep/   # OpenRouter loop
internal/market/         # Yahoo quote fetch
internal/ollama/         # translation JSON chat
internal/openrouter/     # HTTP client + sweep parser
internal/store/          # DB access
config/feeds.txt
config/brief.txt
scripts/smoke.sh         # optional local smoke (needs curl + go)
```

## Tests & smoke

```bash
go test ./...
./scripts/smoke.sh       # RSS ingest + HTTP checks (disables sweep/translate/market by default)
```

RSS smoke (with network) was verified in CI-style Docker: server starts, ingests feeds, `/api/items` returns rows.

## Notes

- **Sweeps** ask the model for JSON stories with sources; rows are stored as `source_kind=sweep`. Model quality and tool/search behavior depend on the **exact** OpenRouter model id and provider settings.
- **Yahoo** quotes are unofficial; symbols may fail if Yahoo blocks your IP.
- **Translation** uses one Ollama `/api/chat` call per backlog item — tune `TRANSLATE_BATCH` and intervals.

See `docs/ARCHITECTURE.md` for design background.
