#!/usr/bin/env bash

#MISE description="Start a local LiteLLM proxy"
#MISE dir="{{ config_root }}"

set -euo pipefail

if [[ -z "${OPENROUTER_DEV_KEY:-}" || "${OPENROUTER_DEV_KEY}" == "unset" ]]; then
  echo "OPENROUTER_DEV_KEY is not set. Run 'mise run zero:openrouter' first." >&2
  exit 1
fi

cleanup() {
  docker compose --profile litellm stop litellm >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM

docker compose --profile litellm up --menu=false litellm
