#!/usr/bin/env bash

#MISE dir="{{ config_root }}/pystreams"
#MISE description="Publish a burst of PresidioAnalysis messages to the local emulator to profile the PresidioAnalyzer subscription"

# Requires PUBSUB_EMULATOR_HOST (set by default in mise.toml) and a locally
# running `mise run start:pystreams-multi` to consume the load. See
# pystreams/src/pystreams/cmd/presidio_load.py for the full runbook and flags.
# Invoke the module directly (rather than the `presidio-load` console script) so
# this works under --no-sync even before the entry point has been installed.
exec uv run --no-sync python -m pystreams.cmd.presidio_load "$@"
