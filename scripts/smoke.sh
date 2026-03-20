#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

export LISTEN_ADDR="${LISTEN_ADDR:-127.0.0.1:18080}"
export RSS_POLL_SEC="${RSS_POLL_SEC:-600}"
export RSS_INGEST_ON_START="${RSS_INGEST_ON_START:-true}"
export SWEEP_POLL_SEC="${SWEEP_POLL_SEC:-0}"
export TRANSLATE_POLL_SEC="${TRANSLATE_POLL_SEC:-0}"
export MARKET_POLL_SEC="${MARKET_POLL_SEC:-0}"

go build -o /tmp/situation-monitor-smoke ./cmd/situation-monitor
/tmp/situation-monitor-smoke &
PID=$!
cleanup() { kill "$PID" 2>/dev/null || true; }
trap cleanup EXIT

if [[ "$LISTEN_ADDR" == http://* || "$LISTEN_ADDR" == https://* ]]; then
  BASE="$LISTEN_ADDR"
else
  BASE="http://$LISTEN_ADDR"
fi

for _ in $(seq 1 30); do
  if curl -sf "$BASE/health" >/dev/null; then
    break
  fi
  sleep 1
done

curl -sf "$BASE/health"
curl -sf "$BASE/api/items?limit=2" | head -c 600 || true
echo
