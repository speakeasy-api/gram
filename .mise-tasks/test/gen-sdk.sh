#!/usr/bin/env bash

#MISE description="Test SDK checks use the Speakeasy workflow"

set -euo pipefail

tmpdir=$(mktemp -d)
trap 'rm -rf "$tmpdir"' EXIT
mkdir -p "$tmpdir/repo/.speakeasy" "$tmpdir/bin"
cp .mise-tasks/gen/sdk.sh "$tmpdir/repo/sdk.sh"

cd "$tmpdir/repo"
cat >.speakeasy/workflow.yaml <<'EOF'
sources:
  Gram-Internal:
    inputs:
      - location: internal.yaml
    overlays:
      - location: internal-overlay.yaml
    transformations:
      - removeUnused: true
    output: internal-output.yaml
  Gram-Admin:
    inputs:
      - location: admin.yaml
    overlays:
      - location: admin-overlay.yaml
    transformations:
      - removeUnused: true
    output: admin-output.yaml
EOF
printf 'internal\n' >internal.yaml
printf 'admin\n' >admin.yaml
printf 'overlay\n' >internal-overlay.yaml
printf 'overlay\n' >admin-overlay.yaml
printf 'internal\n' >internal-output.yaml
printf 'admin\n' >admin-output.yaml

cat >"$tmpdir/bin/speakeasy" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"$SPEAKEASY_LOG"

if [[ "$1" == "run" ]]; then
  grep -q 'removeUnused: true' .speakeasy/workflow.yaml
  while [[ $# -gt 0 ]]; do
    if [[ "$1" == "--source" ]]; then
      source=$2
      break
    fi
    shift
  done
  case "$source" in
    Gram-Internal) cp internal.yaml internal-output.yaml ;;
    Gram-Admin) cp admin.yaml admin-output.yaml ;;
    *) exit 1 ;;
  esac
  exit
fi

echo "unexpected speakeasy invocation: $*" >&2
exit 1
EOF
chmod +x "$tmpdir/bin/speakeasy"

export SPEAKEASY_LOG="$tmpdir/speakeasy.log"
PATH="$tmpdir/bin:$PATH" usage_check=true bash ./sdk.sh
grep -q '^run .*--source Gram-Internal' "$SPEAKEASY_LOG"
grep -q '^run .*--source Gram-Admin' "$SPEAKEASY_LOG"
test "$(cat internal-output.yaml)" = internal
test "$(cat admin-output.yaml)" = admin

printf 'changed\n' >internal.yaml
if PATH="$tmpdir/bin:$PATH" usage_check=true bash ./sdk.sh >/dev/null 2>&1; then
  echo "Expected stale workflow output to fail the check" >&2
  exit 1
fi
test "$(cat internal-output.yaml)" = internal
