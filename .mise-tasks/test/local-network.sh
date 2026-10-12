#!/usr/bin/env bash

#MISE description="Test local listener defaults, readiness probes, and parked-page peers without running services"

set -euo pipefail
node --test .mise-tasks/check/*.test.mts .mise-tasks/park/*.test.mts
