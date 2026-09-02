#!/usr/bin/env bash

#MISE description="Test generated artifact conflict resolution with a base missing a target"

set -euo pipefail

tmpdir=$(mktemp -d)
trap 'rm -rf "$tmpdir"' EXIT
repo="$tmpdir/repo"
mkdir -p "$repo" "$tmpdir/bin"
cp .mise-tasks/gen/resolve.sh "$repo/resolve.sh"

cd "$repo"
git init -q -b main
git config user.email test@example.invalid
git config user.name "Resolve Test"
mkdir -p .speakeasy client/dashboard/src/sdk server/gen
printf 'base\n' >.speakeasy/marker
printf 'base\n' >client/dashboard/src/sdk/marker
printf 'base\n' >server/gen/marker
git add .
git commit -qm base

git switch -qc feature
printf 'feature\n' >.speakeasy/marker
printf 'feature\n' >client/dashboard/src/sdk/marker
printf 'feature\n' >server/gen/marker
mkdir -p client/admin/src/sdk
printf 'feature\n' >client/admin/src/sdk/marker
git add .
git commit -qm feature

cat >"$tmpdir/bin/mise" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
case "$*" in
  "run gen:goa-server") touch server/gen/goa-regenerated ;;
  "run gen:sdk")
    test "$(cat client/admin/src/sdk/marker)" = feature
    mkdir -p client/dashboard/src/sdk client/admin/src/sdk
    touch client/dashboard/src/sdk/sdk-regenerated client/admin/src/sdk/sdk-regenerated
    ;;
  *) echo "unexpected mise invocation: $*" >&2; exit 1 ;;
esac
EOF
chmod +x "$tmpdir/bin/mise"
PATH="$tmpdir/bin:$PATH" bash ./resolve.sh

for marker in .speakeasy/marker client/dashboard/src/sdk/marker server/gen/marker; do
  test "$(cat "$marker")" = base
done
test -e server/gen/goa-regenerated
test -e client/dashboard/src/sdk/sdk-regenerated
test -e client/admin/src/sdk/sdk-regenerated
test "$(cat client/admin/src/sdk/marker)" = feature
