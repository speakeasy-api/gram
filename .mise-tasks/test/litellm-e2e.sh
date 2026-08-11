#!/usr/bin/env bash

#MISE description="Run the pinned LiteLLM real-proxy end-to-end suite"
#MISE dir="{{ config_root }}/server"

set -euo pipefail

gotestsum --format-hide-empty-pkg -- -tags=inv.debug,litellm_e2e ./internal/litellm -run '^TestLiteLLMProxyE2E$' -count=1 -timeout=20m
