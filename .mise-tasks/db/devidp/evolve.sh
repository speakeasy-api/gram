#!/usr/bin/env bash

#MISE dir="{{ config_root }}/dev-idp"
#MISE description="Temporarily evolve local dev-idp databases for the 2026-09-07 schema changes"

# Remove this task and its callers after 2026-10-15, when local environments
# can be assumed to have run the evolution.
set -e

go run ./cmd/evolve-schema
