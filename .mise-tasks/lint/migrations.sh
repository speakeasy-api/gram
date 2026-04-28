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
  while IFS= read -r line; do
    files+=("$line")
  done < <(git diff --relative --diff-filter=d --name-only "$base_ref" -- 'server/migrations/*.sql')
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

# Check for out-of-order migrations
echo -e "\n🔎 Checking for out-of-order migrations..."

# Build a list of changed file basenames for filtering
changed_basenames=()
for file in "${files[@]}"; do
  changed_basenames+=("$(basename "$file")")
done

# Get the highest migration timestamp on the base ref, excluding changed files
latest_base_ts=""
while IFS= read -r migration; do
  base_name="$(basename "$migration")"
  # Skip files that are in our changed set
  skip=false
  for changed in "${changed_basenames[@]}"; do
    if [ "$base_name" = "$changed" ]; then
      skip=true
      break
    fi
  done
  if [ "$skip" = true ]; then
    continue
  fi
  ts="${base_name%%_*}"
  if [ -z "$latest_base_ts" ] || [ "$ts" \> "$latest_base_ts" ]; then
    latest_base_ts="$ts"
  fi
done < <(git ls-tree --name-only "$base_ref" -- server/migrations/ | grep '\.sql$')

if [ -n "$latest_base_ts" ]; then
  out_of_order=false
  for file in "${files[@]}"; do
    base_name="$(basename "$file")"
    ts="${base_name%%_*}"
    if [ "$ts" \< "$latest_base_ts" ] || [ "$ts" = "$latest_base_ts" ]; then
      out_of_order=true
      echo "❌ $file (timestamp $ts <= latest on $base_ref: $latest_base_ts)"
    fi
  done

  if [ "$out_of_order" = true ]; then
    echo "
🚨 One or more migration files were added out of order.
🚨
🚨 The latest migration on $base_ref has timestamp $latest_base_ts, but
🚨 a new migration has a timestamp that is less than or equal to it.
🚨
🚨 Rename the migration file with a timestamp after $latest_base_ts,
🚨 then run 'mise db:hash' to regenerate atlas.sum.
"
    exit 1
  fi

  echo "✅ All new migrations are ordered correctly"
fi

# Run atlas migrate lint
echo -e "\n🔎 Running atlas migrate lint..."
atlas migrate lint \
  --config "file://server/atlas.hcl" \
  --dir "file://server/migrations" \
  --dir-format atlas \
  --git-base "$base_ref" \
  --dev-url "postgres://${DB_USER}:${DB_PASSWORD}@${DB_HOST}:${DB_PORT}/${DB_NAME}?sslmode=disable&search_path=public"
