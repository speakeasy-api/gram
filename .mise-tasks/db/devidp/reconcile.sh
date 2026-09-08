#!/usr/bin/env bash

#MISE dir="{{ config_root }}/dev-idp"
#MISE description="Reconcile the local dev-idp SQLite database with the current schema"

set -e

go run ./cmd/reconcile-schema
