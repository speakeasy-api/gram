#!/usr/bin/env bash

#MISE description="Print a deterministic content hash for the code runner image sources"
#MISE dir="{{ config_root }}"
#MISE hide=true

set -euo pipefail

git rev-parse HEAD:agents/code-runner | cut -c1-12
