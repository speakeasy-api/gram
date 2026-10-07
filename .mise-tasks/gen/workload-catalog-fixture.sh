#!/usr/bin/env bash

#MISE description="Write the workload platform catalog to the dashboard's test fixture"
#MISE dir="{{ config_root }}"

set -e

mise exec -- go generate ./server/internal/workloadpolicy/catalog
