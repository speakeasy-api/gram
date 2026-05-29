#!/usr/bin/env python3
"""Hotword listener for the seed-hooks demo.

Records short audio windows from the default mic, runs each through faster-whisper,
and fires `mise run seed:hook-events --org-slug speakeasy` whenever the trigger
phrase ("release the hounds") is heard.

Run via:  mise run seed:listen

Deps (one-time):
    pip install faster-whisper sounddevice numpy
"""

from __future__ import annotations

import argparse
import difflib
import re
import shlex
import subprocess
import sys
import time
from collections import deque

try:
    import numpy as np
    import sounddevice as sd
    from faster_whisper import WhisperModel
except ImportError as exc:
    sys.exit(
        f"missing dependency: {exc}\n\n"
        "Install with:  pip install faster-whisper sounddevice numpy"
    )

SAMPLE_RATE = 16000


_NORMALIZE_RE = re.compile(r"[^a-z0-9\s]")


def normalize(text: str) -> str:
    """Lowercase, strip punctuation, collapse whitespace."""
    return " ".join(_NORMALIZE_RE.sub(" ", text.lower()).split())


def trigger_matches(heard: str, trigger: str, threshold: float) -> bool:
    """Loose match: normalize both sides, accept on substring OR sliding-window
    similarity above threshold. Tolerates trailing punctuation, missing plurals,
    misheard articles, etc."""
    h = normalize(heard)
    t = normalize(trigger)
    if not t:
        return False
    if t in h:
        return True
    heard_words = h.split()
    trigger_words = t.split()
    if len(heard_words) < len(trigger_words):
        return False
    for i in range(len(heard_words) - len(trigger_words) + 1):
        window = " ".join(heard_words[i : i + len(trigger_words)])
        if difflib.SequenceMatcher(None, window, t).ratio() >= threshold:
            return True
    return False


def resolve_device(value: str | None):
    """Return a device index/name suitable for sounddevice.

    None = system default. Numeric strings become an int (device index).
    Otherwise treated as a substring matched against the device name.
    """
    if value is None:
        return None
    try:
        return int(value)
    except ValueError:
        return value


def main() -> None:
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--trigger", default="release the hounds",
                   help="Phrase to listen for (case-insensitive substring).")
    p.add_argument("--command",
                   default="mise run seed:hook-events --org-slug speakeasy",
                   help="Shell command to run when the trigger fires.")
    p.add_argument("--model", default="tiny.en",
                   help="faster-whisper model. tiny.en (fastest), base.en (more accurate).")
    p.add_argument("--chunk", type=float, default=3.0,
                   help="Seconds per listen window.")
    p.add_argument("--cooldown", type=float, default=5.0,
                   help="Seconds to wait after firing before listening again.")
    p.add_argument("--device", default=None,
                   help="Input device index or name substring. Omit for system default.")
    p.add_argument("--silence-threshold", type=float, default=0.001,
                   help="Skip chunks below this mean-amplitude. Lower = more sensitive.")
    p.add_argument("--list-devices", action="store_true",
                   help="Print available audio devices and exit.")
    p.add_argument("--match-threshold", type=float, default=0.75,
                   help="Fuzzy match similarity threshold (0-1). Lower = more lenient.")
    p.add_argument("--buffer-seconds", type=float, default=8.0,
                   help="Look back this many seconds of transcribed text when matching, so the trigger phrase can straddle two chunks.")
    args = p.parse_args()

    if args.list_devices:
        print(sd.query_devices())
        return

    device = resolve_device(args.device)
    try:
        info = sd.query_devices(device if device is not None else None, kind="input")
    except Exception as exc:
        sys.exit(f"could not open input device {args.device!r}: {exc}")

    print(f"input device: [{info['index']}] {info['name']}", flush=True)
    print(f"loading whisper model {args.model!r}...", flush=True)
    model = WhisperModel(args.model, device="cpu", compute_type="int8")

    trigger = args.trigger
    cmd = shlex.split(args.command)
    chunk_frames = int(SAMPLE_RATE * args.chunk)

    print(f"listening for '{args.trigger}' (Ctrl-C to stop)", flush=True)
    last_fire = 0.0
    silent_streak = 0
    recent: deque[tuple[float, str]] = deque()
    while True:
        try:
            audio = sd.rec(
                chunk_frames,
                samplerate=SAMPLE_RATE,
                channels=1,
                dtype="float32",
                device=device,
            )
            sd.wait()
            audio = audio.flatten()
            level = float(np.abs(audio).mean())

            if level < args.silence_threshold:
                silent_streak += 1
                if silent_streak in (3, 10, 30):
                    print(
                        f"  (silence {silent_streak * args.chunk:.0f}s — level={level:.5f}; "
                        f"check mic permission / device with --list-devices)",
                        flush=True,
                    )
                continue
            silent_streak = 0

            segments, _ = model.transcribe(audio, beam_size=1, language="en")
            text = " ".join(s.text.lower() for s in segments).strip()
            if not text:
                print(f"  (audio level={level:.4f}, no speech detected)", flush=True)
                continue

            print(f"  heard [{level:.4f}]: {text!r}", flush=True)

            # Append + evict by age so the trigger can span chunk boundaries.
            now = time.time()
            recent.append((now, text))
            while recent and now - recent[0][0] > args.buffer_seconds:
                recent.popleft()
            combined = " ".join(t for _, t in recent)

            if (
                trigger_matches(combined, trigger, args.match_threshold)
                and (now - last_fire) > args.cooldown
            ):
                print(f"  -> trigger matched, running: {' '.join(cmd)}", flush=True)
                subprocess.run(cmd)
                last_fire = now
                recent.clear()
        except KeyboardInterrupt:
            print("\nstopped.", flush=True)
            return


if __name__ == "__main__":
    main()
