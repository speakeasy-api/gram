#!/usr/bin/env bash

#MISE description="Test worktree PR lookup, ranking, and caching without GitHub requests"

set -euo pipefail
node --test .mise-tasks/git/worktree-prs.test.mts
