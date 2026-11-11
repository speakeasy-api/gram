#!/usr/bin/env bash

#MISE description="Test the Magellan explore and filing tasks (no model, browser or Linear calls)"

set -euo pipefail
node --test magellan/*.test.mts
