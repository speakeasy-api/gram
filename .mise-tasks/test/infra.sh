#!/usr/bin/env bash
#MISE dir="{{ config_root }}/infra"
#MISE description="Test the infra Python package (gram_infra). Extra args pass through to pytest."

#USAGE flag "--pubsub-emulator-host <host>" env="PUBSUB_EMULATOR_HOST" help="Host to connect to Pub/Sub emulator at. If not set, the emulator integration test will be skipped."

if [[ -n "$usage_pubsub_emulator_host" ]]; then
	export PUBSUB_EMULATOR_HOST="$usage_pubsub_emulator_host"
fi

# Strip the --pubsub-emulator-host flag (and its value) from the args before
# forwarding to pytest, which doesn't understand it.
pytest_args=()
while [[ $# -gt 0 ]]; do
	case "$1" in
	--pubsub-emulator-host)
		shift 2
		;;
	--pubsub-emulator-host=*)
		shift
		;;
	*)
		pytest_args+=("$1")
		shift
		;;
	esac
done

# uv run keeps the project venv in sync with uv.lock before invoking pytest. The
# dev extra provides pytest itself. The emulator integration test self-skips
# unless PUBSUB_EMULATOR_HOST is set.
exec uv run --extra dev pytest "${pytest_args[@]}"
