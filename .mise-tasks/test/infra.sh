#!/usr/bin/env bash
#MISE dir="{{ config_root }}/infra"
#MISE description="Test the infra Python package (gram_infra). Extra args pass through to pytest."

#USAGE flag "--pubsub-emulator-host <host>" env="PUBSUB_EMULATOR_HOST" help="Host to connect to Pub/Sub emulator at. If not set, the emulator integration test will be skipped."

if [[ -n "$usage_pubsub_emulator_host" ]]; then
	export PUBSUB_EMULATOR_HOST="$usage_pubsub_emulator_host"
fi

# uv run keeps the project venv in sync with uv.lock before invoking pytest. The
# dev extra provides pytest itself. The emulator integration test self-skips
# unless PUBSUB_EMULATOR_HOST is set.
exec uv run --extra dev pytest "$@"
