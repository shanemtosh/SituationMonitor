#!/usr/bin/env python3
"""Generate an audio briefing MP3 from a daily briefing YAML file.

Usage:
    generate_audio.py <yaml_file> [--output <mp3_file>] [--voice <voice>]
    generate_audio.py --all [--voice <voice>]

If --output is omitted, writes to the same path with .mp3 extension.
If --all is given, generates audio for every YAML in data/pages/ that lacks an MP3.
"""

import argparse
import io
import os
import sys
import textwrap
from pathlib import Path

import soundfile as sf
from tts_prep import normalize as tts_normalize
import yaml


REPO_ROOT = Path(__file__).resolve().parent.parent.parent
PAGES_DIR = REPO_ROOT / "data" / "pages"


def update_to_script(data: dict, slot: str) -> str:
    """Convert one update slot (midday/evening) into a spoken script."""
    updates = data.get("updates") or []
    entry = next((u for u in updates if u.get("slot") == slot), None)
    if not entry:
        return ""
    date = data.get("date", "")
    weekday = data.get("weekday", "")
    label = "Midday update" if slot == "midday" else "Evening update"

    parts = [f"Situation Monitor {label} for {weekday}, {format_date(date)}.", ""]
    headline = clean(entry.get("headline", ""))
    if headline:
        parts.append(headline)
        parts.append("")

    stories = entry.get("stories") or []
    for i, story in enumerate(stories, 1):
        title = clean(story.get("title", ""))
        parts.append(f"Story {i}. {title}.")
        body = clean(story.get("body", ""))
        if body:
            parts.append(body)
        why = clean(story.get("why", ""))
        if why:
            parts.append(f"Here is why it matters. {why}")
        parts.append("")

    parts.append(f"End of {label.lower()}.")
    return "\n".join(parts)


def yaml_to_script(data: dict) -> str:
    """Convert a briefing YAML dict into a natural spoken script."""
    parts = []
    date = data.get("date", "")
    weekday = data.get("weekday", "")

    # Opening
    parts.append(f"Situation Monitor daily briefing for {weekday}, {format_date(date)}.")
    parts.append("")

    # Summary
    summary = clean(data.get("summary", ""))
    if summary:
        parts.append(summary)
        parts.append("")

    # Markets
    markets = data.get("markets", {})
    if markets:
        parts.append("Markets.")
        narrative = clean(markets.get("narrative", ""))
        if narrative:
            parts.append(narrative)
        parts.append("")

    # Stories
    stories = data.get("stories", [])
    if stories:
        for i, story in enumerate(stories, 1):
            title = clean(story.get("title", ""))
            parts.append(f"Story {i}. {title}.")
            body = clean(story.get("body", ""))
            if body:
                parts.append(body)
            body2 = clean(story.get("body2", ""))
            if body2:
                parts.append(body2)
            why = clean(story.get("why", ""))
            if why:
                parts.append(f"Here is why it matters. {why}")
            parts.append("")

    # Themes
    themes = data.get("themes", [])
    if themes:
        parts.append("Cross-cutting themes.")
        for theme in themes:
            title = clean(theme.get("title", ""))
            body = clean(theme.get("body", ""))
            parts.append(f"{title}. {body}")
            parts.append("")

    # Watchlist
    watchlist = data.get("watchlist", [])
    if watchlist:
        parts.append("Watchlist.")
        for item in watchlist:
            title = clean(item.get("title", ""))
            body = clean(item.get("body", ""))
            parts.append(f"{title}. {body}")
            parts.append("")

    # Closing
    parts.append("End of briefing.")

    return "\n".join(parts)


def format_date(date_str: str) -> str:
    """Convert 2026-03-27 to 'March 27th, 2026'."""
    months = [
        "", "January", "February", "March", "April", "May", "June",
        "July", "August", "September", "October", "November", "December",
    ]
    try:
        parts = date_str.split("-")
        year, month, day = int(parts[0]), int(parts[1]), int(parts[2])
        suffix = "th"
        if day % 10 == 1 and day != 11:
            suffix = "st"
        elif day % 10 == 2 and day != 12:
            suffix = "nd"
        elif day % 10 == 3 and day != 13:
            suffix = "rd"
        return f"{months[month]} {day}{suffix}, {year}"
    except (IndexError, ValueError):
        return date_str


def clean(text: str) -> str:
    """Normalize text for TTS via tts-prep library."""
    if not text:
        return ""
    return tts_normalize(text)


def generate_audio(script: str, output_path: Path, voice: str = "af_heart"):
    """Generate MP3 audio from a text script using Kokoro TTS."""
    os.environ["HF_HOME"] = str(Path.home() / ".cache" / "huggingface")
    from kokoro import KPipeline

    print(f"  Generating audio ({len(script)} chars)...")
    pipeline = KPipeline(lang_code="a", repo_id="hexgrad/Kokoro-82M")

    # Generate all audio chunks
    audio_chunks = []
    for gs, ps, audio in pipeline(script, voice=voice):
        audio_chunks.append(audio)

    if not audio_chunks:
        print("  ERROR: No audio generated")
        return False

    # Concatenate all chunks
    import numpy as np
    full_audio = np.concatenate(audio_chunks)

    duration = len(full_audio) / 24000
    print(f"  Audio duration: {duration:.0f}s ({duration/60:.1f} min)")

    # Write WAV to buffer, then convert to MP3 via ffmpeg
    wav_buf = io.BytesIO()
    sf.write(wav_buf, full_audio, 24000, format="WAV")
    wav_buf.seek(0)

    # Use ffmpeg to convert WAV to MP3
    import subprocess
    result = subprocess.run(
        ["ffmpeg", "-y", "-i", "pipe:0", "-codec:a", "libmp3lame", "-qscale:a", "2", str(output_path)],
        input=wav_buf.read(),
        capture_output=True,
    )
    if result.returncode != 0:
        print(f"  ffmpeg error: {result.stderr.decode()[:200]}")
        return False

    size_mb = output_path.stat().st_size / (1024 * 1024)
    print(f"  Saved: {output_path} ({size_mb:.1f} MB)")
    return True


def process_file(yaml_path: Path, output_path: Path | None, voice: str, slot: str | None = None):
    """Process a single YAML file into an audio briefing.

    If slot is set, render only that update slot ("midday" / "evening") and
    write to <stem>-<slot>.mp3 next to the YAML.
    """
    print(f"Processing {yaml_path.name}{' slot=' + slot if slot else ''}...")

    with open(yaml_path) as f:
        data = yaml.safe_load(f)

    if not data:
        print(f"  Skipping: empty YAML")
        return False

    if slot:
        script = update_to_script(data, slot)
        if not script:
            print(f"  Skipping: no '{slot}' update entry in YAML")
            return False
        if output_path is None:
            output_path = yaml_path.with_name(f"{yaml_path.stem}-{slot}.mp3")
        script_path = yaml_path.with_name(f"{yaml_path.stem}-{slot}.txt")
    else:
        script = yaml_to_script(data)
        if output_path is None:
            output_path = yaml_path.with_suffix(".mp3")
        script_path = yaml_path.with_suffix(".txt")

    script_path.write_text(script)
    print(f"  Script: {script_path} ({len(script)} chars)")

    return generate_audio(script, output_path, voice)


def main():
    parser = argparse.ArgumentParser(description="Generate audio briefings from YAML files")
    parser.add_argument("yaml_file", nargs="?", help="YAML briefing file to process")
    parser.add_argument("--output", "-o", help="Output MP3 path")
    parser.add_argument("--voice", "-v", default="af_heart", help="Kokoro voice (default: af_heart)")
    parser.add_argument("--all", action="store_true", help="Process all YAML files missing MP3s")
    parser.add_argument("--force", action="store_true", help="Regenerate even if MP3 exists")
    parser.add_argument("--script-only", action="store_true", help="Only generate text scripts, no audio")
    parser.add_argument("--slot", choices=["midday", "evening"],
                        help="Render only the given update slot (writes <stem>-<slot>.mp3)")
    args = parser.parse_args()

    if args.all:
        yamls = sorted(PAGES_DIR.glob("*.yaml"))
        if not yamls:
            print("No YAML files found in", PAGES_DIR)
            return
        for yp in yamls:
            mp3 = yp.with_suffix(".mp3")
            if mp3.exists() and not args.force:
                print(f"Skipping {yp.name} (MP3 exists)")
                continue
            if args.script_only:
                with open(yp) as f:
                    data = yaml.safe_load(f)
                if data:
                    script = yaml_to_script(data)
                    txt = yp.with_suffix(".txt")
                    txt.write_text(script)
                    print(f"{yp.name} -> {txt.name} ({len(script)} chars)")
            else:
                process_file(yp, None, args.voice)
    elif args.yaml_file:
        yp = Path(args.yaml_file)
        out = Path(args.output) if args.output else None
        process_file(yp, out, args.voice, slot=args.slot)
    else:
        parser.print_help()


if __name__ == "__main__":
    main()
