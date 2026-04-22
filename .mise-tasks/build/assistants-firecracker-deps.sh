#!/usr/bin/env bash

#MISE description="Download local Firecracker + kernel dependencies for assistant runtimes"
#MISE dir="{{ config_root }}"

#USAGE flag "--arch <arch>" help="Target architecture (amd64 or arm64). Defaults to the current architecture."
#USAGE flag "--version <version>" help="Firecracker version to download." default="1.14.1"
#USAGE flag "--kernel-version <v>" help="Guest kernel image version from firecracker-ci s3 bucket." default="6.1.102"
#USAGE flag "--kernel-bucket-dir <d>" help="firecracker-ci bucket directory (changes per kernel series)." default="v1.10"

set -euo pipefail

arch="${usage_arch:-$(uname -m)}"
arch="${arch/aarch64/arm64}"
arch="${arch/x86_64/amd64}"

case "$arch" in
  amd64) fc_arch="x86_64" ;;
  arm64) fc_arch="aarch64" ;;
  *) echo "unsupported arch: $arch" >&2; exit 1 ;;
esac

version="${usage_version:-1.14.1}"
kernel_version="${usage_kernel_version:-6.1.102}"
kernel_bucket_dir="${usage_kernel_bucket_dir:-v1.10}"
out_dir="./agents/runtime-artifacts/${fc_arch}"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

mkdir -p "$out_dir"

fc_url="https://github.com/firecracker-microvm/firecracker/releases/download/v${version}/firecracker-v${version}-${fc_arch}.tgz"
# The old quickstart kernel is Linux 4.14 from 2020 — no virtio-rng driver and
# no `random.trust_cpu` support, so guest userspace blocks on getrandom() for
# a minute at startup. Use firecracker-ci's maintained 6.x images instead.
kernel_url="https://s3.amazonaws.com/spec.ccfc.min/firecracker-ci/${kernel_bucket_dir}/${fc_arch}/vmlinux-${kernel_version}"

echo "Downloading Firecracker ${version} (${fc_arch})"
curl -fsSL "$fc_url" -o "${tmp_dir}/firecracker.tgz"
tar -xzf "${tmp_dir}/firecracker.tgz" -C "${tmp_dir}"

fc_bin="$(find "${tmp_dir}" -type f -name 'firecracker*' ! -name '*.tgz' | head -n 1)"
if [ -z "${fc_bin}" ]; then
  echo "failed to locate extracted firecracker binary" >&2
  exit 1
fi
install -m 0755 "${fc_bin}" "${out_dir}/firecracker"

echo "Downloading guest kernel (${fc_arch})"
curl -fsSL "$kernel_url" -o "${out_dir}/vmlinux.bin"

echo "Artifacts written to ${out_dir}"
