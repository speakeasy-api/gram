#!/usr/bin/env bash
#MISE dir="{{ config_root }}/infra"
#MISE description="Test the infra Python package (gram_infra). Extra args pass through to pytest."

# uv run keeps the project venv in sync with uv.lock before invoking pytest. The
# dev extra provides pytest itself. The emulator integration test self-skips
# unless PUBSUB_EMULATOR_HOST is set.
exec uv run --extra dev pytest "$@"
