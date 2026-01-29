#!/usr/bin/env bash
#MISE dir="{{ config_root }}"
#MISE description="Lint database migrations to ensure they are safe to apply"

#MISE flag "--git-base <base>" help="The git base to use for finding modified migrations"
#USAGE flag "--github-token <token>" help="The GitHub token for adding comments to a PR"
#USAGE flag "--github-event-path <path>" help="The path to the GitHub event file"
#USAGE flag "--file... <file>" help="The files to lint. This flag can be provided multiple times. If not provided, all modified migrations will be linted."

set -e

base_ref="origin/main"

if [ -n "$usage_git_base" ]; then
  base_ref="$usage_git_base"
fi

files=()

if [ -n "$usage_file" ]; then
  files=("${usage_file[@]}")
else
  mapfile -t files < <(git diff --relative --diff-filter=d --name-only "$base_ref" -- 'server/migrations/*.sql')
fi

if [ ${#files[@]} -eq 0 ]; then
  echo "No migrations were modified, skipping linting"
  exit 0
fi

squawk_cmd=""

# Check if running in GitHub Actions environment
if [ -n "$usage_github_token" ] && [ -n "$usage_github_event_path" ]; then
  echo "Running in GitHub Actions environment"

  squawk_cmd="upload-to-github"

  SQUAWK_GITHUB_TOKEN=$usage_github_token
  SQUAWK_GITHUB_REPO_OWNER=$(jq --raw-output .repository.owner.login "$usage_github_event_path")
  SQUAWK_GITHUB_REPO_NAME=$(jq --raw-output .repository.name "$usage_github_event_path")
  SQUAWK_GITHUB_PR_NUMBER=$(jq --raw-output .pull_request.number "$usage_github_event_path")

  export SQUAWK_GITHUB_TOKEN
  export SQUAWK_GITHUB_REPO_OWNER
  export SQUAWK_GITHUB_REPO_NAME
  export SQUAWK_GITHUB_PR_NUMBER
fi

printf "Changed files:\n%s\n" "${files[@]}"

printf "%s\n" "${files[@]}" | xargs squawk "$squawk_cmd" \
  --config server/.squawk.toml

# We cannot use squawk's `ban-concurrent-index-creation-in-transaction` rule
# because it does not detect "--atlas:txmode none" directive that disables
# transaction mode for atlas. The following section does concurrent index checks
# outside of squawk.
invalid_indexes=false
echo -e "\n🔎 Checking for concurrent index creation statements without -- atlas:txmode none..."
for file in "${files[@]}"; do
  if grep -i -q "CREATE INDEX CONCURRENTLY" "$file" || grep -i -q "CREATE UNIQUE INDEX CONCURRENTLY" "$file"; then
    # Check if the first line contains --atlas:txmode none
    first_line=$(head -n 1 "$file")
    if [ "$first_line" != "-- atlas:txmode none" ]; then
      invalid_indexes=true
      echo "❌ $file"
    else
      echo "✅ $file"
    fi
  fi
done

if [ "$invalid_indexes" = true ]; then
  echo "
🚨 Migration files contain CREATE [UNIQUE] INDEX CONCURRENTLY statements but do
🚨 not have -- atlas:txmode none as the first line.
🚨
🚨 If you are creating migrations to add/remove indexes then ensure these are
🚨 are isolated to their own files and disable transaction mode.
"
  exit 1
fi
