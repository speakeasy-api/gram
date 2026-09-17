#!/usr/bin/env bash

#MISE description="Run mocked local Stripe setup tests (no Stripe mutations)"

set -euo pipefail
node --disable-warning=ExperimentalWarning --experimental-strip-types --test .mise-tasks/stripe/*.test.mts
