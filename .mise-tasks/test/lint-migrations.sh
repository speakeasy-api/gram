#!/usr/bin/env bash
#MISE dir="{{ config_root }}"
#MISE description="Run unit tests for the lint:migrations static checks"

set -euo pipefail

LINT_SCRIPT="$(pwd)/.mise-tasks/lint/migrations.sh"

if [ ! -f "$LINT_SCRIPT" ]; then
  echo "❌ Cannot find lint script at $LINT_SCRIPT" >&2
  exit 1
fi

pass=0
fail=0
failed_cases=()

assert_case() {
  local name="$1"
  local expected_exit="$2"
  local actual_exit="$3"
  local output="$4"
  local expected_substr="${5:-}"

  if [ "$actual_exit" -ne "$expected_exit" ]; then
    fail=$((fail + 1))
    failed_cases+=("$name (exit $actual_exit, want $expected_exit)")
    echo "❌ $name"
    printf '%s\n' "$output" | sed 's/^/    /'
    return
  fi

  if [ -n "$expected_substr" ] && ! printf '%s' "$output" | grep -qF "$expected_substr"; then
    fail=$((fail + 1))
    failed_cases+=("$name (missing substring: $expected_substr)")
    echo "❌ $name"
    printf '%s\n' "$output" | sed 's/^/    /'
    return
  fi

  pass=$((pass + 1))
  echo "✅ $name"
}

setup_repo() {
  local dir="$1"
  rm -rf "$dir"
  mkdir -p "$dir/server/migrations"

  git -C "$dir" init -q -b main
  git -C "$dir" config user.email "test@example.com"
  git -C "$dir" config user.name "Test"

  printf -- '-- baseline 1\n' > "$dir/server/migrations/20260101000001_one.sql"
  printf -- '-- baseline 2\n' > "$dir/server/migrations/20260201000001_two.sql"
  git -C "$dir" add server/migrations
  git -C "$dir" commit -q -m "baseline migrations"
  git -C "$dir" checkout -q -b feature
}

# Adds files (path -> contents) on the feature branch and commits them.
add_files_and_commit() {
  local dir="$1"
  shift
  local file
  while [ $# -gt 0 ]; do
    file="$1"
    local contents="$2"
    shift 2
    mkdir -p "$dir/$(dirname "$file")"
    printf '%s' "$contents" > "$dir/$file"
  done
  git -C "$dir" add server/migrations
  git -C "$dir" commit -q -m "add migrations"
}

# Run the lint script against a temp repo's feature branch with main as base.
# Mise/usage env vars are simulated; --no-atlas skips the atlas dev DB step.
run_lint() {
  local dir="$1"
  (
    cd "$dir"
    usage_git_base="main" \
      usage_no_atlas="true" \
      bash "$LINT_SCRIPT" 2>&1
  ) || return $?
}

TMP_ROOT="$(mktemp -d)"
trap 'rm -rf "$TMP_ROOT"' EXIT

# ─── Case 1: new migration with timestamp greater than base latest passes ──
DIR="$TMP_ROOT/case-newer"
setup_repo "$DIR"
add_files_and_commit "$DIR" \
  "server/migrations/20260301000001_new.sql" "-- new"$'\n'
set +e
out=$(run_lint "$DIR")
rc=$?
set -e
assert_case "newer timestamp passes" 0 "$rc" "$out" "All new migrations are ordered correctly"

# ─── Case 2: timestamp equal to latest base fails ─────────────────────────
DIR="$TMP_ROOT/case-equal"
setup_repo "$DIR"
add_files_and_commit "$DIR" \
  "server/migrations/20260201000001_dup.sql" "-- dup"$'\n'
set +e
out=$(run_lint "$DIR")
rc=$?
set -e
assert_case "equal timestamp fails" 1 "$rc" "$out" "added out of order"

# ─── Case 3: timestamp older than base latest fails ───────────────────────
DIR="$TMP_ROOT/case-older"
setup_repo "$DIR"
add_files_and_commit "$DIR" \
  "server/migrations/20260115000001_old.sql" "-- old"$'\n'
set +e
out=$(run_lint "$DIR")
rc=$?
set -e
assert_case "older timestamp fails" 1 "$rc" "$out" "added out of order"

# ─── Case 4: error guidance recommends destroy+rediff (not rename/db:hash) ─
DIR="$TMP_ROOT/case-message"
setup_repo "$DIR"
add_files_and_commit "$DIR" \
  "server/migrations/20260115000002_old.sql" "-- old"$'\n'
set +e
out=$(run_lint "$DIR")
rc=$?
set -e
assert_case "error message recommends destroy+rediff" 1 "$rc" "$out" "Delete the offending migration"
if printf '%s' "$out" | grep -qiE 'rename the migration|db:hash'; then
  fail=$((fail + 1))
  failed_cases+=("error message must not mention rename or db:hash")
  echo "❌ error message must not mention rename or db:hash"
  printf '%s\n' "$out" | sed 's/^/    /'
else
  pass=$((pass + 1))
  echo "✅ error message must not mention rename or db:hash"
fi

# ─── Case 5: mixed (one valid, one invalid) fails and names the bad file ──
DIR="$TMP_ROOT/case-mixed"
setup_repo "$DIR"
add_files_and_commit "$DIR" \
  "server/migrations/20260301000001_ok.sql" "-- good"$'\n' \
  "server/migrations/20260115000001_bad.sql" "-- bad"$'\n'
set +e
out=$(run_lint "$DIR")
rc=$?
set -e
assert_case "mixed valid+invalid fails" 1 "$rc" "$out" "20260115000001_bad.sql"

# ─── Case 6: CREATE INDEX CONCURRENTLY without atlas:txmode none fails ────
DIR="$TMP_ROOT/case-concurrent-idx"
setup_repo "$DIR"
add_files_and_commit "$DIR" \
  "server/migrations/20260301000001_idx.sql" "CREATE INDEX CONCURRENTLY idx_foo ON foo(bar);"$'\n'
set +e
out=$(run_lint "$DIR")
rc=$?
set -e
assert_case "concurrent index without txmode none fails" 1 "$rc" "$out" "atlas:txmode none"

# ─── Case 7: CREATE INDEX CONCURRENTLY with atlas:txmode none passes ──────
DIR="$TMP_ROOT/case-concurrent-idx-ok"
setup_repo "$DIR"
add_files_and_commit "$DIR" \
  "server/migrations/20260301000002_idx_ok.sql" $'-- atlas:txmode none\nCREATE INDEX CONCURRENTLY idx_foo ON foo(bar);\n'
set +e
out=$(run_lint "$DIR")
rc=$?
set -e
assert_case "concurrent index with txmode none passes" 0 "$rc" "$out" "All new migrations are ordered correctly"

# ─── Case 8: no migrations changed exits 0 with skip message ──────────────
DIR="$TMP_ROOT/case-nochange"
setup_repo "$DIR"
# No new files; feature branch matches main.
set +e
out=$(run_lint "$DIR")
rc=$?
set -e
assert_case "no migration changes skips" 0 "$rc" "$out" "No migrations were modified"

echo
echo "──────────────────────────────"
echo "Passed: $pass"
echo "Failed: $fail"

if [ "$fail" -gt 0 ]; then
  echo
  echo "Failed cases:"
  for c in "${failed_cases[@]}"; do
    echo "  - $c"
  done
  exit 1
fi
