#!/usr/bin/env bash

#MISE description="Run mocked local Stripe setup tests (no Stripe mutations)"

set -euo pipefail
node --test .mise-tasks/stripe/*.test.mts
