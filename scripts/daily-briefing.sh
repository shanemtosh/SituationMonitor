#!/bin/bash
# Daily briefing with retry logic.
# Retries up to 3 times with 5-minute gaps on failure.

MAX_RETRIES=3
RETRY_DELAY=300
CLAUDE="/home/shane/.local/bin/claude"
SKILL="$(cat /home/shane/Code/SituationMonitor/.claude/skills/daily-briefing.md)"

for attempt in $(seq 1 $MAX_RETRIES); do
    echo "$(date '+%Y-%m-%d %H:%M:%S') Attempt $attempt/$MAX_RETRIES"

    if $CLAUDE -p "$SKILL" \
        --max-turns 75 \
        --allowedTools 'Bash(*)' 'Read(*)' 'Write(*)' 'WebSearch(*)' 'Glob(*)' 'Grep(*)' 'WebFetch(*)'; then
        echo "$(date '+%Y-%m-%d %H:%M:%S') Briefing completed successfully on attempt $attempt"
        exit 0
    fi

    EXIT_CODE=$?
    echo "$(date '+%Y-%m-%d %H:%M:%S') Attempt $attempt failed (exit code $EXIT_CODE)"

    if [ $attempt -lt $MAX_RETRIES ]; then
        echo "Retrying in ${RETRY_DELAY}s..."
        sleep $RETRY_DELAY
    fi
done

echo "$(date '+%Y-%m-%d %H:%M:%S') All $MAX_RETRIES attempts failed"
exit 1
