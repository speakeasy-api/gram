#!/usr/bin/env bash

#MISE description="Build local Firecracker artifacts for assistant runtimes"
#MISE dir="{{ config_root }}"

#USAGE flag "--arch <arch>" help="Target architecture (amd64 or arm64). Defaults to the current architecture."
#USAGE flag "--version <version>" help="Firecracker version to download." default="1.14.1"
#USAGE flag "--rootfs-size-mib <mib>" help="Size of the generated ext4 rootfs image in MiB." default="2048"

set -euo pipefail

arch_args=()
if [ -n "${usage_arch:-}" ]; then
  arch_args+=(--arch "${usage_arch}")
fi

echo "Preparing Firecracker dependencies"
if [ "${#arch_args[@]}" -gt 0 ]; then
  mise run build:assistants-firecracker-deps "${arch_args[@]}" --version "${usage_version:-1.14.1}"
else
  mise run build:assistants-firecracker-deps --version "${usage_version:-1.14.1}"
fi

echo "Building assistant runtime rootfs"
if [ "${#arch_args[@]}" -gt 0 ]; then
  mise run build:assistants-runtime-rootfs "${arch_args[@]}" --rootfs-size-mib "${usage_rootfs_size_mib:-2048}"
else
  mise run build:assistants-runtime-rootfs --rootfs-size-mib "${usage_rootfs_size_mib:-2048}"
fi
