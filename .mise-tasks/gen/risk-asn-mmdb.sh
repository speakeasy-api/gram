#!/usr/bin/env bash

#MISE description="Refresh the DB-IP ASN mmdb embedded by the risk_analysis package"

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
DATA_DIR="$ROOT/server/internal/background/activities/risk_analysis/data"
MMDB="$DATA_DIR/dbip-asn.mmdb"
SHA="$DATA_DIR/dbip-asn.mmdb.sha256"
URL="https://raw.githubusercontent.com/sapics/ip-location-db/main/dbip-asn-mmdb/dbip-asn.mmdb"

tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT

echo "Downloading $URL"
curl -fsSL -o "$tmp" "$URL"

# Floor on size keeps an empty/HTML error response from silently
# overwriting the embedded data with garbage. Upstream is ~9 MB; a
# truncated download is the only realistic failure mode.
size=$(wc -c < "$tmp")
if [ "$size" -lt 1000000 ]; then
  echo "downloaded file is suspiciously small ($size bytes); aborting" >&2
  exit 1
fi

new_sha=$(shasum -a 256 "$tmp" | awk '{print $1}')
old_sha=""
if [ -f "$SHA" ]; then
  old_sha=$(cat "$SHA")
fi
if [ "$new_sha" = "$old_sha" ]; then
  echo "Already up to date (sha256 $new_sha)"
  exit 0
fi

mv "$tmp" "$MMDB"
echo "$new_sha" > "$SHA"
echo "Updated $MMDB (sha256 $new_sha)"

# Run the package tests so a corrupt or wildly-changed snapshot fails
# fast. The suite asserts well-known cloud IPs (Cloudflare AS13335,
# Google LLC AS15169, GitHub AS36459, Fastly AS54113) still classify
# as infra, which is the integrity check we care about.
echo "Verifying with package tests"
( cd "$ROOT" && go test ./server/internal/background/activities/risk_analysis/... )
