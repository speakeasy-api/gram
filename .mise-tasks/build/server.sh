#!/usr/bin/env bash

#MISE description="Build the gram server with Bazel into server/bin/gram"

#USAGE flag "--readonly" help="Fail instead of updating BUILD files or MODULE.bazel.lock"

set -euo pipefail

args=(--stamp)
if [ "${usage_readonly:-false}" = "true" ]; then
  args+=(--lockfile_mode=error)
else
  # A new package or import needs its BUILD file before Bazel can build it.
  mise run -q bazel:gazelle
fi

# The git SHA and GRAM_ASSISTANT_RUNTIME_IMAGE_HASH are stamped in by
# tools/bazel/workspace_status.sh.
bazel build "${args[@]}" //server:server

out=$(bazel cquery "${args[@]}" --output=files //server:server 2>/dev/null)
mkdir -p server/bin
install -m 0755 "$out" server/bin/gram
echo "Built server/bin/gram"
