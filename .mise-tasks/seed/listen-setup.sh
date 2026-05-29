#!/usr/bin/env bash

#MISE description="One-time setup for seed:listen — create a venv at .venv-listen and install faster-whisper deps."
#MISE dir="{{ config_root }}"

set -e

PY="${PYTHON_BIN:-python3.12}"
if ! command -v "$PY" >/dev/null 2>&1; then
  if command -v python3.13 >/dev/null 2>&1; then
    PY=python3.13
  elif command -v python3.14 >/dev/null 2>&1; then
    PY=python3.14
  else
    echo "no suitable python3.12/3.13/3.14 found; brew install python@3.12" >&2
    exit 1
  fi
fi

VENV=".venv-listen"
echo "creating $VENV with $PY ($("$PY" --version))"
"$PY" -m venv "$VENV"
"$VENV/bin/pip" install --quiet --upgrade pip
"$VENV/bin/pip" install --quiet faster-whisper sounddevice numpy
echo "done. run: mise run seed:listen"
