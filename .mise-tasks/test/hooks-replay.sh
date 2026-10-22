#!/usr/bin/env bash

#MISE description="Test the hooks:replay transcript parser against its synthetic fixture"

set -euo pipefail
node --test .mise-tasks/hooks/replay/*.test.mts
