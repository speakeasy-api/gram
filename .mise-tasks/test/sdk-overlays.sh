#!/usr/bin/env bash

#MISE description="Prove SDK overlays isolate and name Admin operations"

set -euo pipefail

raw_spec="server/gen/http/openapi3.yaml"
common_overlay="overlays/goa-common.yaml"
dashboard_overlay="overlays/dashboard-sdk.yaml"
admin_overlay="overlays/admin-sdk.yaml"
tmpdir=$(mktemp -d)
trap 'rm -rf "$tmpdir"' EXIT

cp "$raw_spec" "$tmpdir/baseline.yaml"
cp "$raw_spec" "$tmpdir/probe.yaml"

match_count=$(yq '[.paths.*.* | select(.operationId == "adminGetSession")] | length' "$tmpdir/probe.yaml")
if [ "$match_count" -ne 1 ]; then
  echo "Expected exactly one adminGetSession operation, found $match_count" >&2
  exit 1
fi

yq -i '(.paths.*.* | select(.operationId == "adminGetSession")).x-admin-isolation-probe = true' "$tmpdir/probe.yaml"
probe_count=$(yq '[.paths.*.* | select(.x-admin-isolation-probe == true)] | length' "$tmpdir/probe.yaml")
if [ "$probe_count" -ne 1 ]; then
  echo "Expected exactly one Admin isolation probe, found $probe_count" >&2
  exit 1
fi

for input in baseline probe; do
  speakeasy overlay apply \
    --schema "$tmpdir/$input.yaml" \
    --overlay "$common_overlay" \
    --out "$tmpdir/$input.common.yaml" >/dev/null 2>&1
  speakeasy overlay apply \
    --schema "$tmpdir/$input.common.yaml" \
    --overlay "$dashboard_overlay" \
    --out "$tmpdir/$input.overlay.yaml" >/dev/null 2>&1
  speakeasy openapi transform remove-unused \
    --schema "$tmpdir/$input.overlay.yaml" \
    --out "$tmpdir/$input.dashboard.yaml" >/dev/null 2>&1
done

diff -u "$tmpdir/baseline.dashboard.yaml" "$tmpdir/probe.dashboard.yaml"

output="$tmpdir/probe.dashboard.yaml"
if grep -Eq '^[[:space:]]*/admin/' "$output"; then
  echo "Dashboard input contains an Admin path" >&2
  exit 1
fi
if grep -Eq '^[[:space:]]*- name: admin$' "$output"; then
  echo "Dashboard input contains the standalone Admin tag" >&2
  exit 1
fi
if grep -q 'AdminOrganization' "$output"; then
  echo "Dashboard input contains an Admin schema" >&2
  exit 1
fi
if grep -q 'admin_auth_header_Authorization' "$output"; then
  echo "Dashboard input contains Admin auth" >&2
  exit 1
fi

speakeasy overlay apply \
  --schema "$tmpdir/baseline.common.yaml" \
  --overlay "$admin_overlay" \
  --out "$tmpdir/admin.yaml" >/dev/null 2>&1

operation_rows=$(
  yq -o=json "$tmpdir/admin.yaml" |
    jq -r '.paths[][] | select(.operationId and (."x-speakeasy-ignore" != true)) | [.operationId, (."x-speakeasy-name-override" // "")] | @tsv'
)
if [ -z "$operation_rows" ]; then
  echo "Expected active Admin operations, found none" >&2
  exit 1
fi

while IFS=$'\t' read -r operation_id sdk_name; do
  if [[ $operation_id != admin* ]]; then
    echo "Expected stable admin-prefixed operation ID, found $operation_id" >&2
    exit 1
  fi
  local_name=${operation_id#admin}
  first_letter=$(printf '%s' "${local_name:0:1}" | tr '[:upper:]' '[:lower:]')
  expected_name="$first_letter${local_name:1}"
  if [ "$sdk_name" != "$expected_name" ]; then
    echo "Expected $operation_id to have SDK name $expected_name, found ${sdk_name:-none}" >&2
    exit 1
  fi
done <<<"$operation_rows"
