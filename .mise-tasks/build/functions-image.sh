#!/usr/bin/env bash

#MISE description="Build a Gram Functions runner image"
#MISE dir="{{ config_root }}/functions"
#MISE hide=true

#USAGE flag "--level <level>" help="Set log level for builds" default="info"
#USAGE flag "--apko-config <path>" required=#true help="Path to apko config file" 
#USAGE flag "--apko-cache-dir <path>" required=#true help="Path to apko cache directory" default="../local/cache/apko" 
#USAGE flag "--tarball-name <name>" required=#true help="Name of the OCI tarball" default="image.tar" 
#USAGE flag "--arch <arch>" required=#true help="Comma-separated list of target architectures" default="x86_64"
#USAGE flag "--image <image>" required=#true help="Set the OCI image name including tag"
#USAGE flag "--melange-public-key <path>" help="Path to melange signing key"
#USAGE flag "--out <path>" help="Path to output the OCI image tarball" default="./oci"

set -euo pipefail

mkdir -p ./oci

image="${usage_image:?Error: image not provided}"
out="${usage_out:?Error: output directory not provided}"
apko_config="${usage_apko_config:?Error: apko config not provided}"
apko_cache_dir="${usage_apko_cache_dir:?Error: apko cache dir not provided}"
archs="${usage_arch:?Error: arch not provided}"
tarball="$out/${usage_tarball_name:?Error: tarball name not provided}"
log_level="${usage_level:-info}"
date=$(date --utc +"%Y-%m-%dT%H:%M:%SZ")

melange_pub_key=${usage_melange_public_key:-$MELANGE_PUBLIC_KEY}
if [ ! -f "$melange_pub_key" ]; then
  echo "Error: melange public key file not found: $melange_pub_key"
  exit 1
fi

if [ -n "${GITHUB_OUTPUT:-}" ]; then
  echo "image=$image" | tee -a "$GITHUB_OUTPUT"
  echo "tarball=$tarball" | tee -a "$GITHUB_OUTPUT"
fi

rm -rf "$out"
mkdir -p "$out"
apko build \
  --keyring-append "$melange_pub_key" \
  --build-date "$date" \
  --sbom-path "$out" \
  --cache-dir "$apko_cache_dir" \
  --log-level "$log_level" \
  --arch "$archs" \
  "$apko_config" \
  "$image" \
  "$tarball"

# Check for world-writable directories
world_writable=$(find / -type d -perm -0002 ! -perm -1000 -exec ls -ld {} \;)
if [ -n "$world_writable" ]; then
  echo "Error: World-writable directories found:"
  echo "$world_writable"
  exit 1
fi

# Check for setuid/setgid files
setuid_files=$(find / -perm /6000 -type f)
if [ -n "$setuid_files" ]; then
  echo "Error: Found setuid/setgid files:"
  echo "$setuid_files"
  exit 1
fi

# gram user should only own /home/gram
non_gram_dirs=$(find / -type d -user gram)
if [ "$non_gram_dirs" != "/home/gram" ]; then
  echo "Error: Found directories owned by gram user other than /home/gram:"
  echo "$non_gram_dirs"
  exit 1
fi