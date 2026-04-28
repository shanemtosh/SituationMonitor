#!/bin/bash
# Generate audio for one update slot (midday/evening) from today's briefing YAML.
# Called by midday-briefing-audio.service / evening-briefing-audio.service via OnSuccess=.
#
# Usage: generate-update-audio.sh <slot>
#   slot: midday | evening

set -u

SLOT="${1:-}"
if [ "$SLOT" != "midday" ] && [ "$SLOT" != "evening" ]; then
    echo "$(date '+%Y-%m-%d %H:%M:%S') generate-update-audio.sh: slot must be 'midday' or 'evening' (got '$SLOT')"
    exit 2
fi

TODAY=$(date +%Y-%m-%d)
YAML_FILE="/home/shane/Code/SituationMonitor/data/pages/${TODAY}.yaml"
VENV="/home/shane/Code/SituationMonitor/scripts/tts/.venv"

if [ ! -f "$YAML_FILE" ]; then
    echo "$(date '+%Y-%m-%d %H:%M:%S') No briefing YAML for $TODAY, skipping $SLOT audio"
    exit 0
fi

MP3_FILE="/home/shane/Code/SituationMonitor/data/pages/${TODAY}-${SLOT}.mp3"
if [ -f "$MP3_FILE" ]; then
    echo "$(date '+%Y-%m-%d %H:%M:%S') $SLOT audio already exists for $TODAY, skipping"
    exit 0
fi

echo "$(date '+%Y-%m-%d %H:%M:%S') Generating $SLOT audio for $TODAY..."
VIRTUAL_ENV="$VENV" "$VENV/bin/python" \
    /home/shane/Code/SituationMonitor/scripts/tts/generate_audio.py \
    --slot "$SLOT" \
    "$YAML_FILE" 2>&1

if [ $? -eq 0 ]; then
    echo "$(date '+%Y-%m-%d %H:%M:%S') $SLOT audio generation complete"
else
    echo "$(date '+%Y-%m-%d %H:%M:%S') $SLOT audio generation failed"
    exit 1
fi
