#!/usr/bin/env bash

#MISE description="Check that every management API endpoint records an authz decision in tests"
#MISE dir="{{ config_root }}"
#USAGE flag "--records <dir>" help="Directory of coverage records written by test runs with GRAM_AUTHZ_COVERAGE_DIR set" required=#true
#USAGE flag "--write" help="Rewrite the allowlist from this report (only after a full test run)"

set -euo pipefail

args=(-records "${usage_records}")
if [ "${usage_write:-false}" = "true" ]; then
  args+=(-write)
fi

go run ./ci/cmd/authzcoverage "${args[@]}"
