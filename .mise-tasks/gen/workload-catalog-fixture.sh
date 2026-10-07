#!/usr/bin/env bash

#MISE description="Write the workload platform catalog to the dashboard's test fixture"
#MISE dir="{{ config_root }}"
# The generator compiles server/gen and sqlc packages, so it runs after they are rewritten.
#MISE depends=["gen:goa-server","gen:sqlc-server"]

set -e

mise exec -- go generate ./server/internal/workloadpolicy/catalog
