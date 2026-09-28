#!/usr/bin/env bash

#MISE description="Check that every Postgres and ClickHouse column has a data classification"
#MISE dir="{{ config_root }}/server"

# Builds both desired-state schemas in throwaway containers (Docker required)
# and fails if any stored column lacks a comment ending in `@access: <class>`.
# The same test runs in CI as part of the server test suite. Classes and the
# comment format are defined in server/internal/dataclassification.

set -eo pipefail

exec go test -count=1 ./internal/dataclassification/...
