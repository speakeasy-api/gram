#!/usr/bin/env bash

#MISE description="Record LiteLLM 1.94.0 contract fixtures from the pinned Docker image"
#MISE dir="{{ config_root }}"

set -euo pipefail

python3 local/litellm/contract-fixtures/generate.py
