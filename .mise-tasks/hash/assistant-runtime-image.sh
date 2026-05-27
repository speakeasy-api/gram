#!/usr/bin/env bash

#MISE description="Print a deterministic content hash for the assistant runtime image sources"
#MISE dir="{{ config_root }}"

set -euo pipefail

git rev-parse HEAD:agents | cut -c1-12
