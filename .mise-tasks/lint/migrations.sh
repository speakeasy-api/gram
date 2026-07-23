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

# Migration directories whose files must stay in strictly increasing timestamp
# order relative to the base ref. Each is checked independently:
#
#   server/migrations                      Postgres, Atlas format (<ts>_<name>.sql)
#   server/clickhouse/migrations           ClickHouse, Atlas format (<ts>_<name>.sql)
#   server/clickhouse/local/golang_migrate ClickHouse, golang-migrate format
#                                          (<ts>_<name>.up.sql / .down.sql pairs)
#
# server/clickhouse/local/backfill holds hand-named data-backfill scripts, not
# migrations, so it is deliberately absent.
PG_DIR="server/migrations"
CH_ATLAS_DIR="server/clickhouse/migrations"
CH_GOLANG_DIR="server/clickhouse/local/golang_migrate"

# Print the --file entries that live under a directory, one per line.
files_under() {
  local dir="$1" f
  for f in "${usage_file[@]}"; do
    case "$f" in
      "$dir"/*) echo "$f" ;;
    esac
  done
}

# Collect the migration files added under a directory relative to the base ref.
#
# --no-renames is essential: git's default rename detection pairs a migration a
# stale branch adds with a similar main-only migration the branch is missing and
# reports it as a rename, not an addition, so it silently escapes the ordering
# check (the exact INC-418 failure mode). Disabling rename detection makes every
# new migration surface as an addition.
#
# Args: <dir> <glob>. Prints one file path per line. Honors --file when given.
collect_added() {
  local dir="$1" glob="$2"
  if [ -n "$usage_file" ]; then
    files_under "$dir"
  else
    git diff --no-renames --relative --diff-filter=A --name-only "$base_ref" -- "$dir/$glob"
  fi
}

# Fail if any newly-added migration in a directory has a timestamp at or before
# the latest timestamp already on the base ref. Sets ORDERING_FAILED=1 rather
# than exiting so every directory is reported in one run.
#
# Args: <dir> <added-glob> <name-filter-regex> <label> <diff-cmd>
check_ordering() {
  local dir="$1" glob="$2" filter="$3" label="$4" diff_cmd="$5"

  local added=()
  while IFS= read -r line; do
    [ -n "$line" ] && added+=("$line")
  done < <(collect_added "$dir" "$glob")

  if [ ${#added[@]} -eq 0 ]; then
    echo "No newly added $label migrations, skipping ordering check"
    return 0
  fi

  local latest_base_name latest_base_ts
  latest_base_name=$(git ls-tree --name-only "$base_ref" -- "$dir/" | grep "$filter" | sort | tail -n 1)
  latest_base_ts="${latest_base_name##*/}"
  latest_base_ts="${latest_base_ts%%_*}"

  local bad=() file base_name ts
  for file in "${added[@]}"; do
    base_name="${file##*/}"
    ts="${base_name%%_*}"
    if [ -n "$latest_base_ts" ] && { [ "$ts" \< "$latest_base_ts" ] || [ "$ts" = "$latest_base_ts" ]; }; then
      bad+=("$file")
      echo "❌ $file (timestamp $ts <= latest on $base_ref: $latest_base_ts)"
      gh_error "$file" "Migration $base_name has timestamp $ts <= latest on $base_ref ($latest_base_ts). Do NOT rename the file or hand-edit atlas.sum. Delete this file, rebase $base_ref, then re-run '$diff_cmd <name>' so it is regenerated on top with a fresh timestamp."
    else
      echo "✅ $file"
    fi
  done

  if [ ${#bad[@]} -eq 0 ]; then
    return 0
  fi

  ORDERING_FAILED=1
  echo "
🚨 The following $label migration(s) were added out of order (timestamp <= $latest_base_ts on $base_ref):
🚨"
  for file in "${bad[@]}"; do
    echo "🚨   - $file"
  done
  echo "🚨
🚨 Do NOT rename the file(s) or hand-edit atlas.sum. Migration files and
🚨 atlas.sum must only be produced by the Atlas CLI.
🚨
🚨 To fix:
🚨   1. Delete the out-of-order file(s) listed above on this branch."
  if [ "$dir" = "$CH_ATLAS_DIR" ] || [ "$dir" = "$CH_GOLANG_DIR" ]; then
    echo "🚨      For ClickHouse, delete the matching file in BOTH flavor dirs
🚨      ($CH_ATLAS_DIR and $CH_GOLANG_DIR)."
  fi
  echo "🚨   2. Rebase or merge $base_ref into your branch.
🚨   3. Re-run '$diff_cmd <name>' so the migration is regenerated on top
🚨      with a fresh timestamp."
  if [ "$dir" = "$CH_ATLAS_DIR" ] || [ "$dir" = "$CH_GOLANG_DIR" ]; then
    echo "🚨      'mise clickhouse:diff' regenerates both ClickHouse flavors together,
🚨      keeping the Atlas and golang-migrate dirs in sync."
  fi
  echo ""
}

# Postgres files modified in this change, used by the concurrent-index check and
# to gate the local 'atlas migrate lint' step.
collect_pg_modified() {
  if [ -n "$usage_file" ]; then
    files_under "$PG_DIR"
  else
    git diff --no-renames --relative --diff-filter=d --name-only "$base_ref" -- "$PG_DIR/*.sql"
  fi
}

pg_files=()
while IFS= read -r line; do
  [ -n "$line" ] && pg_files+=("$line")
done < <(collect_pg_modified)

# Check for concurrent index creation statements without -- atlas:txmode none.
# Postgres-only: ClickHouse has no CREATE INDEX CONCURRENTLY.
if [ ${#pg_files[@]} -gt 0 ]; then
  invalid_indexes=false
  echo -e "\n🔎 Checking for concurrent index creation statements without -- atlas:txmode none..."
  for file in "${pg_files[@]}"; do
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
fi

# Ordering only applies to newly-added migrations: edits and renames don't move a
# file's timestamp, so they can't make the migration sequence out of order.
echo -e "\n🔎 Checking for out-of-order migrations..."
ORDERING_FAILED=0
check_ordering "$PG_DIR" '*.sql' '\.sql$' "Postgres" "mise db:diff"
check_ordering "$CH_ATLAS_DIR" '*.sql' '\.sql$' "ClickHouse (Atlas)" "mise clickhouse:diff"
check_ordering "$CH_GOLANG_DIR" '*.up.sql' '\.up\.sql$' "ClickHouse (golang-migrate)" "mise clickhouse:diff"

if [ "$ORDERING_FAILED" -eq 1 ]; then
  exit 1
fi

echo "✅ All new migrations are ordered correctly"

if [ "${usage_no_atlas:-}" = "true" ]; then
  echo -e "\nSkipping 'atlas migrate lint' (--no-atlas)"
  exit 0
fi

if [ ${#pg_files[@]} -eq 0 ]; then
  echo -e "\nNo Postgres migrations were modified, skipping 'atlas migrate lint'"
  exit 0
fi

# Run atlas migrate lint (Postgres). ClickHouse Atlas linting requires a
# docker://clickhouse dev database and is handled by the dedicated atlas-action
# CI step, so it is intentionally not run here.
echo -e "\n🔎 Running atlas migrate lint..."
atlas migrate lint \
  --config "file://server/atlas.hcl" \
  --dir "file://server/migrations" \
  --dir-format atlas \
  --git-base "$base_ref" \
  --dev-url "postgres://${DB_USER}:${DB_PASSWORD}@${DB_HOST}:${DB_PORT}/${DB_NAME}?sslmode=disable&search_path=public"
