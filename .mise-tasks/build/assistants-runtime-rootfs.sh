#!/usr/bin/env bash

#MISE description="Build the guest rootfs image for local assistant Firecracker runtimes"
#MISE dir="{{ config_root }}"

#USAGE flag "--arch <arch>" help="Target architecture (amd64 or arm64). Defaults to the current architecture."
#USAGE flag "--rootfs-size-mib <mib>" help="Size of the generated ext4 rootfs image in MiB." default="2048"

set -euo pipefail

arch="${usage_arch:-$(uname -m)}"
arch="${arch/aarch64/arm64}"
arch="${arch/x86_64/amd64}"

case "$arch" in
  amd64) fc_arch="x86_64" ; docker_arch="amd64" ;;
  arm64) fc_arch="aarch64" ; docker_arch="arm64" ;;
  *) echo "unsupported arch: $arch" >&2; exit 1 ;;
esac

size_mib="${usage_rootfs_size_mib:-2048}"
image="gram-assistant-runtime-rootfs:${docker_arch}-dev"
out_dir="./agents/runtime-artifacts/${fc_arch}"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

mkdir -p "$out_dir" "${tmp_dir}/rootfs"

echo "Building runtime image ${image}"
docker build --platform "linux/${docker_arch}" -f ./agents/runtime-image/Dockerfile -t "${image}" .

cid="$(docker create --platform "linux/${docker_arch}" "${image}")"
trap 'docker rm -f "${cid}" >/dev/null 2>&1 || true; rm -rf "$tmp_dir"' EXIT

echo "Exporting root filesystem"
docker export "${cid}" | tar -xf - -C "${tmp_dir}/rootfs"
docker rm -f "${cid}" >/dev/null

root_ca="${NODE_EXTRA_CA_CERTS:-}"
if [ -n "$root_ca" ] && [ -f "$root_ca" ]; then
  install -d "${tmp_dir}/rootfs/usr/local/share/ca-certificates"
  install -m 0644 "$root_ca" "${tmp_dir}/rootfs/usr/local/share/ca-certificates/gram-local-dev.crt"
fi

echo "Packing ext4 rootfs"
docker run --rm \
  --platform "linux/${docker_arch}" \
  -v "${tmp_dir}/rootfs:/rootfs" \
  -v "${out_dir}:/out" \
  debian:bookworm-slim \
  bash -lc "
    set -euo pipefail
    apt-get update >/dev/null
    apt-get install -y --no-install-recommends e2fsprogs >/dev/null
    if [ -f /rootfs/usr/local/share/ca-certificates/gram-local-dev.crt ]; then
      chroot /rootfs update-ca-certificates >/dev/null
    fi
    rm -f /out/assistant-rootfs.ext4
    truncate -s ${size_mib}M /out/assistant-rootfs.ext4
    mkfs.ext4 -F -d /rootfs /out/assistant-rootfs.ext4 >/dev/null
  "

echo "Rootfs written to ${out_dir}/assistant-rootfs.ext4"
