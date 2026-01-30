#!/usr/bin/env bash
#MISE dir="{{ config_root }}"
#MISE description="Lint database migrations to ensure they are safe to apply"

#MISE flag "--git-base <base>" help="The git base to use for finding modified migrations"
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

printf "Changed files:\n%s\n" "${files[@]}"

# Check for concurrent index creation statements without -- atlas:txmode none
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
