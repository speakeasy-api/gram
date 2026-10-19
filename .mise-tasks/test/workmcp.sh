#!/usr/bin/env bash

#MISE description="Test worktree MCP setup and scoped cleanup without changing personal config"

set -euo pipefail
node --test .mise-tasks/git/workmcp.test.mts
