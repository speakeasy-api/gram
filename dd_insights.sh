#!/usr/bin/env bash
set -euo pipefail

export PATH="$HOME/.local/bin:/usr/local/bin:/opt/homebrew/bin:/usr/bin:/bin:$PATH"

cd "$HOME/github.com/speakeasy-api/gram"

"$HOME/.local/bin/claude" --print "Run the datadog-insights skill"
