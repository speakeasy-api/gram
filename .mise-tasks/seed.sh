#!/usr/bin/env bash

#MISE description="Seed the local development organization (data + your user, API key)"
#MISE dir="{{ config_root }}"
#MISE depends=["ensure-stack"]

set -euo pipefail

# The local seed is the demo org seed retargeted at the dev-idp's default org
# (see server/internal/demoseed/spec.go): the same SQL, the same code path
# production runs daily, but writable and with you adopted into it as an admin.
# It also seeds the shared demo org itself, untargeted, so /explore-demo shows
# the same data locally as it does in production. You are not a member there —
# the session enters it through auth.enterDemo, exactly as in production.
#
# Idempotent and fast, so there is no completion marker to short-circuit on —
# just run it. Everything it used to write back into mise.local.toml
# (GRAM_API_KEY) is now a fixed value checked into mise.toml.
cd server
go run . demo-seed --local "$@"

# The Gram-owned catalog is global; seed the Okta-mapped vendors so server
# suggestions have something to match locally. Only the database override is
# shared with the demo seed; its other flags do not apply here.
catalog_args=()
while [ $# -gt 0 ]; do
  case "$1" in
    --database-url) catalog_args+=("$1" "$2"); shift ;;
    --database-url=*) catalog_args+=("$1") ;;
  esac
  shift
done
exec go run . registry-okta-seed ${catalog_args[@]+"${catalog_args[@]}"}
