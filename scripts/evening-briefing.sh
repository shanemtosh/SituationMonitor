#!/bin/bash
# Evening briefing delta — appends an `updates[]` entry to today's morning YAML.
# Mirrors daily-briefing.sh: 3 retries, 5-min gap on failure.

set -u

MAX_RETRIES=3
RETRY_DELAY=300
CLAUDE="/home/shane/.local/bin/claude"
SKILL="$(cat /home/shane/Code/SituationMonitor/.claude/skills/evening-briefing.md)"

for attempt in $(seq 1 $MAX_RETRIES); do
    echo "$(date '+%Y-%m-%d %H:%M:%S') Evening attempt $attempt/$MAX_RETRIES"

    if $CLAUDE -p "$SKILL" \
        --max-turns 40 \
        --allowedTools 'Bash(*)' 'Read(*)' 'Write(*)' 'WebSearch(*)' 'Glob(*)' 'Grep(*)' 'WebFetch(*)'; then
        echo "$(date '+%Y-%m-%d %H:%M:%S') Evening update completed on attempt $attempt"
        exit 0
    fi

    EXIT_CODE=$?
    echo "$(date '+%Y-%m-%d %H:%M:%S') Attempt $attempt failed (exit $EXIT_CODE)"

    if [ $attempt -lt $MAX_RETRIES ]; then
        echo "Retrying in ${RETRY_DELAY}s..."
        sleep $RETRY_DELAY
    fi
done

echo "$(date '+%Y-%m-%d %H:%M:%S') All $MAX_RETRIES evening attempts failed"
exit 1
