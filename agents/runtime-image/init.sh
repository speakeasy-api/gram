#!/usr/bin/env bash

set -euo pipefail

# Provision the assistant workdir from the prebuilt ext4 template. The rootfs
# is per-machine so we can mount the template directly — any mutations stay
# isolated to this machine and the loop mount enforces the hard size cap baked
# into the template at image build time.
workdir_image=/usr/share/gram/workdir-template.ext4
workdir_mount=/var/lib/gram-assistant/work
mkdir -p "$workdir_mount"
mount -o loop "$workdir_image" "$workdir_mount"

/usr/local/bin/lightpanda-supervise &

runner_port="${GRAM_ASSISTANT_RUNTIME_GUEST_PORT:-8081}"
runner_addr="${GRAM_RUNNER_ADDR:-0.0.0.0:${runner_port}}"

exec /usr/local/bin/gram-assistant-runner serve --addr "$runner_addr"
