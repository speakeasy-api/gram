#!/usr/bin/env bash

#MISE description="Regenerate Bazel BUILD files and Go module repos from the Go sources and go.mod"

#USAGE flag "--check" help="Fail if any BUILD file or MODULE.bazel is out of date instead of updating it"

set -euo pipefail

if [ "${usage_check:-false}" = "true" ]; then
  # -mode=diff prints the changes Gazelle would make and exits non-zero when
  # there are any.
  bazel run //:gazelle -- -mode=diff
  bazel mod tidy --lockfile_mode=error
  git diff --exit-code -- MODULE.bazel
  exit 0
fi

bazel run //:gazelle
bazel mod tidy
