#!/usr/bin/env bash

#MISE description="Test local Temporal lifecycle ordering without running services"

set -euo pipefail
node --test .mise-tasks/temporal/*.test.mts
