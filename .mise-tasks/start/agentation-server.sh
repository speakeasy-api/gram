#!/usr/bin/env bash

#MISE description="Run the Agentation server under Pitchfork"
#MISE dir="{{ config_root }}"
#MISE hide=true

set -euo pipefail

export AGENTATION_STORE=memory
exec aube exec --no-install -F dashboard agentation-mcp server --host 127.0.0.1 --port "${GRAM_AGENTATION_PORT:?}"
