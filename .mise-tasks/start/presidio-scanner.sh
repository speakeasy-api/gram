#!/usr/bin/env bash

#MISE dir="{{ config_root }}/presidio-scanner"
#MISE description="Start the Presidio scanner that consumes gram.risk.v1.PresidioRequest messages"

set -e

# `uv run` provisions/syncs the project's virtualenv on demand before running.
# PUBSUB_EMULATOR_HOST is provided by mise.toml; the scanner defaults the GCP
# project to "my-project-id" for the local emulator.
exec uv run presidio-scanner "$@"
