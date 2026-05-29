#!/usr/bin/env bash

#MISE description="Listen on the mic for 'release the hounds' and fire the seed-hooks demo. Run 'mise run seed:listen-setup' once first."
#MISE dir="{{ config_root }}"

#USAGE flag "--trigger <phrase>" default="release the hounds" help="Phrase to listen for."
#USAGE flag "--model <name>" default="tiny.en" help="faster-whisper model (tiny.en fastest, base.en more accurate)."
#USAGE flag "--chunk <seconds>" default="3.0" help="Seconds per listen window."
#USAGE flag "--cooldown <seconds>" default="5.0" help="Seconds to wait after firing before listening again."

set -e

VENV_PY=".venv-listen/bin/python3"
if [ ! -x "$VENV_PY" ]; then
  echo "venv missing; run: mise run seed:listen-setup" >&2
  exit 1
fi

exec "$VENV_PY" scripts/listen-hounds.py \
  --trigger "${usage_trigger:-release the hounds}" \
  --model "${usage_model:-tiny.en}" \
  --chunk "${usage_chunk:-3.0}" \
  --cooldown "${usage_cooldown:-5.0}"
