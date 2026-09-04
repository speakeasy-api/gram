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
cat >.speakeasy/workflow.yaml <<'EOF'
sources:
    Gram-Internal:
        output: .speakeasy/out.openapi.yaml
targets:
    gram-internal:
        output: client/dashboard/src/sdk
EOF
printf 'base\n' >.speakeasy/openapi-hooks.yaml
printf 'base\n' >.speakeasy/out.openapi.yaml
printf 'base\n' >.speakeasy/workflow.lock
printf 'base\n' >client/dashboard/src/sdk/marker
printf 'base\n' >server/gen/marker
git add .
git commit -qm base

git switch -qc feature
cat >.speakeasy/workflow.yaml <<'EOF'
sources:
    Gram-Internal:
        output: .speakeasy/out.openapi.yaml
    Gram-Admin:
        output: .speakeasy/out.admin.openapi.yaml
targets:
    gram-internal:
        output: client/dashboard/src/sdk
    gram-admin:
        output: client/admin/src/sdk
EOF
printf 'feature\n' >.speakeasy/openapi-hooks.yaml
printf 'feature\n' >.speakeasy/out.openapi.yaml
printf 'feature\n' >.speakeasy/out.admin.openapi.yaml
printf 'feature\n' >.speakeasy/workflow.lock
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
    test "$(cat .speakeasy/openapi-hooks.yaml)" = base
    test "$(cat .speakeasy/out.openapi.yaml)" = base
    test "$(cat .speakeasy/out.admin.openapi.yaml)" = feature
    test "$(cat .speakeasy/workflow.lock)" = base
    test "$(cat client/admin/src/sdk/marker)" = feature
    mkdir -p client/dashboard/src/sdk
    touch client/dashboard/src/sdk/sdk-regenerated
    admin_output=$(awk '/^    gram-admin:/{ target = 1; next } target && /output:/{ print $2; exit }' .speakeasy/workflow.yaml)
    test "$admin_output" = client/admin/src/sdk
    mkdir -p "$admin_output"
    touch "$admin_output/sdk-regenerated"
    ;;
  *) echo "unexpected mise invocation: $*" >&2; exit 1 ;;
esac
EOF
chmod +x "$tmpdir/bin/mise"
PATH="$tmpdir/bin:$PATH" bash ./resolve.sh

for marker in .speakeasy/openapi-hooks.yaml .speakeasy/out.openapi.yaml .speakeasy/workflow.lock client/dashboard/src/sdk/marker server/gen/marker; do
  test "$(cat "$marker")" = base
done
grep -q '^    gram-admin:' .speakeasy/workflow.yaml
test -e server/gen/goa-regenerated
test -e client/dashboard/src/sdk/sdk-regenerated
test -e client/admin/src/sdk/sdk-regenerated
test "$(cat client/admin/src/sdk/marker)" = feature
