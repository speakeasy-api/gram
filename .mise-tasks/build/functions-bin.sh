#!/usr/bin/env bash

#MISE description="Build the Gram Functions host program"
#MISE dir="{{ config_root }}/functions"
#MISE depends=["go:tidy"]

#USAGE flag "--level <level>" help="Set log level for builds" default="info"
#USAGE flag "--arch <arch>" required=#true help="Comma-separated list of target architectures" default="x86_64"
#USAGE flag "--melange-private-key <path>" help="Path to melange signing key"

archs="${usage_arch:?Error: arch not provided}"
log_level="${usage_level:-info}"

melange_priv_key=${usage_melange_private_key:-$MELANGE_PRIVATE_KEY}
if [ ! -f "$melange_priv_key" ]; then
  echo "Error: melange private key file not found: $melange_priv_key"
  exit 1
fi

rm -rf ./packages
exec melange build \
  --cache-dir "$(go env GOMODCACHE)" \
  --arch "${archs}" \
  --runner docker \
  --git-repo-url https://github.com/speakeasy-api/gram \
  --signing-key "$melange_priv_key" \
  --log-level "$log_level" \
  ./melange.yaml