#!/usr/bin/env bash

#MISE description="Test the flow-checks and Magellan agent tasks and workflow invariants (no model, browser or Linear calls)"

set -euo pipefail
node --test .mise-tasks/flows/*.test.mts .mise-tasks/magellan/*.test.mts
