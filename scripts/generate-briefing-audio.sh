#!/bin/bash
# Generate audio for today's daily briefing YAML.
# Called by briefing-audio.service after daily-briefing.service succeeds.

TODAY=$(date +%Y-%m-%d)
YAML_FILE="/home/shane/Code/SituationMonitor/data/pages/${TODAY}.yaml"
VENV="/home/shane/Code/SituationMonitor/scripts/tts/.venv"

if [ ! -f "$YAML_FILE" ]; then
    echo "$(date '+%Y-%m-%d %H:%M:%S') No briefing YAML for $TODAY, skipping audio"
    exit 0
fi

MP3_FILE="${YAML_FILE%.yaml}.mp3"
if [ -f "$MP3_FILE" ]; then
    echo "$(date '+%Y-%m-%d %H:%M:%S') Audio already exists for $TODAY, skipping"
    exit 0
fi

echo "$(date '+%Y-%m-%d %H:%M:%S') Generating audio for $TODAY..."
VIRTUAL_ENV="$VENV" "$VENV/bin/python" \
    /home/shane/Code/SituationMonitor/scripts/tts/generate_audio.py \
    "$YAML_FILE" 2>&1

if [ $? -eq 0 ]; then
    echo "$(date '+%Y-%m-%d %H:%M:%S') Audio generation complete"
else
    echo "$(date '+%Y-%m-%d %H:%M:%S') Audio generation failed"
    exit 1
fi
