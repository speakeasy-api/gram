#!/usr/bin/env bash

#MISE description="Build the Gram Functions host program"
#MISE dir="{{ config_root }}/functions"
#MISE depends=["go:tidy"]

#USAGE flag "--level <level>" help="Set log level for builds" default="info"
#USAGE flag "--arch <arch>" required=#true help="Comma-separated list of target architectures" default="x86_64"
#USAGE flag "--melange-private-key <path>" help="Path to melange signing key"
#USAGE flag "--apk-cache-dir <path>" required=#true help="Path to apko cache directory" default="../local/cache/apk"
#USAGE flag "--dev" help="Mark as development build" default="false"

set -euo pipefail

archs="${usage_arch:?Error: arch not provided}"
apk_cache_dir="${usage_apk_cache_dir:?Error: apk cache dir not provided}"
log_level="${usage_level:-info}"

melange_priv_key=${usage_melange_private_key:-$MELANGE_PRIVATE_KEY}
if [ ! -f "$melange_priv_key" ]; then
  echo "Error: melange private key file not found: $melange_priv_key"
  exit 1
fi

vars_file=$(mktemp)
trap 'rm -f "$vars_file"' EXIT

suffix=""
if [ "${usage_dev:-false}" = "true" ]; then
  suffix="-dev"
fi

sha="$(git rev-parse --short HEAD)"
{
  echo "date: $(date -u +"%Y-%m-%dT%H:%M:%SZ")"
  echo "commit: $sha"
  echo "version: $sha$suffix"
} >> "$vars_file"

# Create a minimal module layout for melange build context because
# `.melangeignore` does not really work. They are using `github.com/zealic/xignore`
# under the hood which does not seem to support negation patterns correctly.
rm -rf .tmp-melange/functions
mkdir -p .tmp-melange/functions .tmp-melange/server
trap 'rm -rf .tmp-melange' EXIT
cp -r ../go.mod ../go.sum .tmp-melange/
cp -r ../server/gen .tmp-melange/server/
cp -r ./{internal,cmd,buildinfo} .tmp-melange/functions/

rm -rf ./packages
melange build \
  --source-dir .tmp-melange \
  --apk-cache-dir "$apk_cache_dir" \
  --cache-dir "$(go env GOMODCACHE)" \
  --arch "${archs}" \
  --runner docker \
  --git-repo-url https://github.com/speakeasy-api/gram \
  --signing-key "$melange_priv_key" \
  --log-level "$log_level" \
  --vars-file "$vars_file" \
  ./melange.yaml