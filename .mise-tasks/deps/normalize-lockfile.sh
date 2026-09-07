#!/usr/bin/env bash

#MISE description="Clean unrelated pnpm lockfile churn after a dependency update using aube"
#MISE dir="{{ config_root }}"

# pnpm can re-resolve unrelated peer snapshots during dependency updates. Start
# from the branch's base lockfile so aube resolves only the manifest changes.

set -euo pipefail

if [ -n "$(git status --porcelain)" ]; then
  echo "Error: working tree must be clean before normalizing the lockfile." >&2
  exit 1
fi

base_ref=origin/main
if ! git rev-parse --verify --quiet "$base_ref^{commit}" >/dev/null; then
  echo "Error: $base_ref is unavailable. Fetch it before running this task." >&2
  exit 1
fi

base=$(git merge-base HEAD "$base_ref")
if ! git cat-file -e "$base:pnpm-lock.yaml" 2>/dev/null; then
  echo "Error: pnpm-lock.yaml does not exist at merge base $base." >&2
  exit 1
fi

echo "Restoring pnpm-lock.yaml from merge base $base..."
git restore --source="$base" --worktree -- pnpm-lock.yaml

keep_normalized=0
restore_tree() {
  if [ "$keep_normalized" -eq 0 ]; then
    git restore --worktree -- .
    git clean -fd --quiet
  fi
}
trap restore_tree EXIT

echo "Re-resolving manifest changes with aube..."
aube install --lockfile-only --fix-lockfile

unexpected=$(
  {
    git diff --name-only -- . ':!pnpm-lock.yaml'
    git diff --cached --name-only
    git ls-files --others --exclude-standard
  } | sort -u
)
if [ -n "$unexpected" ]; then
  echo "Error: files other than pnpm-lock.yaml changed:" >&2
  echo "$unexpected" >&2
  exit 1
fi

git diff --check -- pnpm-lock.yaml
keep_normalized=1

if git diff --quiet -- pnpm-lock.yaml; then
  echo "Lockfile is already normalized; no changes to review."
  exit 0
fi

echo
echo "Normalized lockfile diff:"
git diff --stat -- pnpm-lock.yaml
echo
echo "Review with: git diff -- pnpm-lock.yaml"
echo "The task did not stage, commit, or push the change."
