#!/usr/bin/env bash

#MISE dir="{{ config_root }}"
#MISE description="Split schema and migration changes off the current branch into a separate mig: PR"
#MISE alias="splitmig"

#USAGE flag "-b --base <base>" default="origin/main" help="Base branch the migration PR targets"
#USAGE flag "--worktree-dir <dir>" help="Parent directory for the temporary worktree (defaults to /tmp)"
#USAGE flag "--keep-worktree" help="Keep the worktree after pushing — useful for inspection"
#USAGE flag "--dry-run" help="Stop before commit/push/PR creation"

set -euo pipefail

if ! command -v gh >/dev/null; then
  echo "gh is required" >&2
  exit 1
fi
if ! command -v jq >/dev/null; then
  echo "jq is required" >&2
  exit 1
fi

base="${usage_base:-origin/main}"
worktree_parent="${usage_worktree_dir:-/tmp}"
keep_worktree="${usage_keep_worktree:-false}"
dry_run="${usage_dry_run:-false}"

src_branch=$(git rev-parse --abbrev-ref HEAD)
if [ "$src_branch" = "HEAD" ]; then
  echo "Detached HEAD; check out a branch first." >&2
  exit 1
fi
src_sha=$(git rev-parse HEAD)
repo_root=$(git rev-parse --show-toplevel)

git fetch origin --quiet

# Paths the migration PR carries from the source branch. Generated Go files
# are NOT in this list — we regenerate them from schema.sql in the worktree
# so we don't drag in unrelated changes from queries.sql edits.
mig_paths=(
  "server/database/schema.sql"
  "server/migrations"
)

changed=$(git diff --name-only "$base...HEAD" -- "${mig_paths[@]}")
if [ -z "$changed" ]; then
  echo "No schema or migration changes between $base and HEAD." >&2
  exit 1
fi

echo "Schema/migration files to lift:"
echo "$changed" | sed 's/^/  /'
echo

# Resolve current PR (for title + back-reference). Missing PR is fine — we
# fall back to the branch name.
pr_title=""
pr_url=""
if pr_json=$(gh pr view --json number,title,url 2>/dev/null); then
  pr_title=$(echo "$pr_json" | jq -r '.title')
  pr_url=$(echo "$pr_json" | jq -r '.url')
fi
if [ -z "$pr_title" ]; then
  pr_title="$src_branch"
fi

# Strip a Conventional Commit prefix if present, then prepend "mig: ".
stripped=$(printf "%s" "$pr_title" | sed -E 's/^(feat|fix|chore|docs|refactor|test|build|ci|perf|style|mig)(\([^)]+\))?(!)?:[[:space:]]*//')
new_title="mig: ${stripped}"

# Use awk on a single 32-byte read so we don't hit SIGPIPE under `pipefail`
# the way `tr ... </dev/urandom | head -c N` would.
suffix=$(head -c 32 /dev/urandom | LC_ALL=C tr -dc 'a-z0-9' | cut -c1-6)
new_branch="mig/${src_branch}-${suffix}"
dest="${worktree_parent}/_gram_mig_${suffix}"

echo "Source branch: $src_branch"
echo "Source PR:     ${pr_url:-<none>}"
echo "New branch:    $new_branch"
echo "New title:     $new_title"
echo "Worktree:      $dest"
echo

cleanup() {
  if [ "$keep_worktree" != "true" ] && [ -d "$dest" ]; then
    git -C "$repo_root" worktree remove --force "$dest" 2>/dev/null || true
  fi
}

if [ "$keep_worktree" != "true" ]; then
  trap cleanup EXIT
fi

git worktree add "$dest" "$base"

(
  cd "$dest"
  mise trust >/dev/null

  git checkout -b "$new_branch"

  # Lift schema + migration files from the source branch.
  git checkout "$src_sha" -- "${mig_paths[@]}"

  # Refresh atlas.sum and sqlc-generated Go files so they're consistent with
  # the lifted schema. queries.sql is unchanged from base in this worktree,
  # so any queries.sql.go diff is purely schema-derived.
  mise run db:hash >/dev/null
  mise run gen:sqlc-server >/dev/null

  # The pre-commit hook runs gofix which is a Node/zx task; install root
  # node_modules from the offline cache so the hook can resolve `zx`.
  if ! mise run install:pnpm --offline >/dev/null 2>&1; then
    mise run install:pnpm >/dev/null
  fi

  git add -A

  if git diff --staged --quiet; then
    echo "Nothing to commit after regeneration." >&2
    exit 1
  fi

  if [ "$dry_run" = "true" ]; then
    echo "--dry-run: staged changes ready in $dest, exiting before commit."
    exit 0
  fi

  ref_line=""
  if [ -n "$pr_url" ]; then
    ref_line=$(printf "\n\nSplit out of %s." "$pr_url")
  fi
  git commit -m "${new_title}${ref_line}"

  git push -u origin "$new_branch"

  pr_body=$(cat <<EOF
Schema and migrations split off ${pr_url:-\`$src_branch\`} so the database changes can land independently.
EOF
)

  gh pr create --title "$new_title" --body "$pr_body" --base main
)

echo
echo "Done."
if [ "$keep_worktree" = "true" ]; then
  echo "Worktree kept at: $dest"
fi
