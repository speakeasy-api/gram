#!/usr/bin/env bash
#MISE dir="{{ config_root }}"
#MISE description="Lint database migrations to ensure they are safe to apply"

#MISE flag "--git-base <base>" help="The git base to use for finding modified migrations"
#USAGE flag "--file... <file>" help="The files to lint. This flag can be provided multiple times. If not provided, all modified migrations will be linted."
#USAGE flag "--no-atlas" help="Skip the 'atlas migrate lint' step. CI uses this so it isn't duplicated by the dedicated atlas-action job step."

set -e

# Emit a GitHub Actions error annotation alongside human output when running in CI.
gh_error() {
  if [ "${GITHUB_ACTIONS:-}" = "true" ]; then
    echo "::error file=$1::$2"
  fi
}

base_ref="origin/main"

if [ -n "$usage_git_base" ]; then
  base_ref="$usage_git_base"
fi

files=()
added_files=()

if [ -n "$usage_file" ]; then
  files=("${usage_file[@]}")
  added_files=("${usage_file[@]}")
else
  while IFS= read -r line; do
    files+=("$line")
  done < <(git diff --relative --diff-filter=d --name-only "$base_ref" -- 'server/migrations/*.sql')
  while IFS= read -r line; do
    added_files+=("$line")
  done < <(git diff --relative --diff-filter=A --name-only "$base_ref" -- 'server/migrations/*.sql')
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
      gh_error "$file" "Migration uses CREATE [UNIQUE] INDEX CONCURRENTLY but does not have '-- atlas:txmode none' as the first line."
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

# Ordering only applies to newly-added migrations: edits and renames don't
# move a file's timestamp, so they can't make the migration sequence out of order.
echo -e "\n🔎 Checking for out-of-order migrations..."

if [ ${#added_files[@]} -eq 0 ]; then
  echo "No newly added migrations, skipping ordering check"
else
  latest_base_name=$(git ls-tree --name-only "$base_ref" -- server/migrations/ | grep '\.sql$' | sort | tail -n 1)
  latest_base_ts="${latest_base_name##*/}"
  latest_base_ts="${latest_base_ts%%_*}"

  out_of_order=false
  for file in "${added_files[@]}"; do
    base_name="${file##*/}"
    ts="${base_name%%_*}"
    if [ -n "$latest_base_ts" ] && { [ "$ts" \< "$latest_base_ts" ] || [ "$ts" = "$latest_base_ts" ]; }; then
      out_of_order=true
      echo "❌ $file (timestamp $ts <= latest on $base_ref: $latest_base_ts)"
      gh_error "$file" "Migration $base_name has timestamp $ts <= latest on $base_ref ($latest_base_ts). Do NOT rename or hand-edit atlas.sum. Delete the migration, rebase $base_ref, then re-run 'mise db:diff <name>' so it is regenerated on top."
    fi
  done

  if [ "$out_of_order" = true ]; then
    echo "
🚨 One or more migrations were added out of order (timestamp <= $latest_base_ts on $base_ref).
🚨 See CLAUDE.md > Database Migrations for the fix.
"
    exit 1
  fi

  echo "✅ All new migrations are ordered correctly"
fi

if [ "${usage_no_atlas:-}" = "true" ]; then
  echo -e "\nSkipping 'atlas migrate lint' (--no-atlas)"
  exit 0
fi

# Run atlas migrate lint
echo -e "\n🔎 Running atlas migrate lint..."
atlas migrate lint \
  --config "file://server/atlas.hcl" \
  --dir "file://server/migrations" \
  --dir-format atlas \
  --git-base "$base_ref" \
  --dev-url "postgres://${DB_USER}:${DB_PASSWORD}@${DB_HOST}:${DB_PORT}/${DB_NAME}?sslmode=disable&search_path=public"
