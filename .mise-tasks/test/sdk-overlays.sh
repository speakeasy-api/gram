#!/usr/bin/env bash

#MISE description="Prove Admin operations cannot affect the Dashboard SDK input"

set -euo pipefail

raw_spec="server/gen/http/openapi3.yaml"
common_overlay="overlays/goa-common.yaml"
dashboard_overlay="overlays/dashboard-sdk.yaml"
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
