#!/bin/bash
# Weekly combined alpha refresh — runs all four domain skills in sequence.
# Updates assessments, prunes stale ones, writes per-domain digest under
# data/alpha/digests/YYYY-Www/.

set -u

MAX_RETRIES=3
RETRY_DELAY=300
CLAUDE="/home/shane/.local/bin/claude"

# Use a heredoc wrapper instead of cat'ing one of the four skill files —
# the wrapper instructs Claude to read each skill in sequence.
PROMPT=$(cat <<'EOF'
You are running the weekly combined alpha refresh for SituationMonitor.
Compute the current ISO week with `date +%G-W%V` (e.g. 2026-W17). Read each
skill file in sequence and complete its full instructions before moving to
the next. Between skills, log "=== begin {domain} ===" and "=== end {domain} ==="
so progress is greppable in journalctl.

In order:
  1. /home/shane/Code/SituationMonitor/.claude/skills/geopolitical-alpha.md
  2. /home/shane/Code/SituationMonitor/.claude/skills/macro-alpha.md
  3. /home/shane/Code/SituationMonitor/.claude/skills/semiconductors-alpha.md
  4. /home/shane/Code/SituationMonitor/.claude/skills/energy-alpha.md

Each skill operates in "weekly mode" — read its Weekly Mode section and
follow it. The intent is: scan for material changes since last week,
update assessments where evidence has shifted them, perform stale cleanup
(Step C), and write a per-domain weekly digest (Step D).

When all four complete, summarize what was done across domains in a single
final paragraph: total assessments updated/created/resolved, biggest
probability shifts, and any constraints that flipped.
EOF
)

for attempt in $(seq 1 $MAX_RETRIES); do
    echo "$(date '+%Y-%m-%d %H:%M:%S') Weekly alpha attempt $attempt/$MAX_RETRIES"

    if $CLAUDE -p "$PROMPT" \
        --max-turns 200 \
        --allowedTools 'Bash(*)' 'Read(*)' 'Write(*)' 'WebSearch(*)' 'Glob(*)' 'Grep(*)' 'WebFetch(*)'; then
        echo "$(date '+%Y-%m-%d %H:%M:%S') Weekly alpha completed on attempt $attempt"
        exit 0
    fi

    EXIT_CODE=$?
    echo "$(date '+%Y-%m-%d %H:%M:%S') Attempt $attempt failed (exit $EXIT_CODE)"

    if [ $attempt -lt $MAX_RETRIES ]; then
        echo "Retrying in ${RETRY_DELAY}s..."
        sleep $RETRY_DELAY
    fi
done

echo "$(date '+%Y-%m-%d %H:%M:%S') All $MAX_RETRIES weekly alpha attempts failed"
exit 1
