#!/usr/bin/env bash

#MISE description="Check for file changes after a build"

set -e

git_status_output=$(git status --porcelain)

if [[ -n "$git_status_output" ]]; then
  >&2 echo "$git_status_output"
  >&2 git diff | cat
  >&2 echo "🚨 FAIL: Build process resulted in file changes."
  exit 1
else
  echo "✅ OK: No file changes detected after build."
fi

exit 0
