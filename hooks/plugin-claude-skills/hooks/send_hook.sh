#!/usr/bin/env bash
set -u

server_url="${GRAM_HOOKS_SERVER_URL:-https://app.getgram.ai}"
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
shared_producer="$script_dir/../../shared-producer/send-hook.mts"
node_dir="$script_dir/../runtime/node"
node_bin="$node_dir/bin/node"
node_version="v22.18.0"
node_platform="linux-x64"
node_archive="node-${node_version}-${node_platform}.tar.xz"
node_url="https://nodejs.org/dist/${node_version}/${node_archive}"

find_node() {
  if [ -x "$node_bin" ]; then
    echo "$node_bin"
    return 0
  fi

  if command -v node >/dev/null 2>&1; then
    command -v node
    return 0
  fi

  return 1
}

install_node() {
  tmpdir="$(mktemp -d 2>/dev/null || mktemp -d -t gram-node)"
  archive_path="$tmpdir/$node_archive"
  staged_dir="$tmpdir/node-staged"
  cleanup() {
    rm -rf "$tmpdir"
  }
  trap cleanup RETURN

  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$node_url" -o "$archive_path" || return 1
  elif command -v wget >/dev/null 2>&1; then
    wget -qO "$archive_path" "$node_url" || return 1
  else
    return 1
  fi

  if ! command -v tar >/dev/null 2>&1; then
    return 1
  fi

  tar -xJf "$archive_path" -C "$tmpdir" || return 1
  extracted_dir="$tmpdir/node-${node_version}-${node_platform}"
  [ -d "$extracted_dir" ] || return 1

  mkdir -p "$staged_dir"
  cp -R "$extracted_dir"/* "$staged_dir"/ || return 1
  mkdir -p "$(dirname "$node_dir")"

  if [ -x "$node_bin" ]; then
    echo "$node_bin"
    return 0
  fi

  mv "$staged_dir" "$node_dir" 2>/dev/null || true

  if [ -x "$node_bin" ]; then
    echo "$node_bin"
    return 0
  fi

  return 1
}

run_legacy_fallback() {
  curl -s -X POST \
    -H "Content-Type: application/json" \
    -d @- \
    --max-time 30 \
    "${server_url}/rpc/hooks.claude" 2>/dev/null || echo '{}'
}

payload="$(cat)"

node_path="$(find_node 2>/dev/null || true)"
if [ -z "$node_path" ]; then
  node_path="$(install_node 2>/dev/null || true)"
fi

if [ -n "$node_path" ] && [ -f "$shared_producer" ]; then
  printf '%s' "$payload" | "$node_path" "$shared_producer" --agent=claude
  exit 0
fi

printf '%s' "$payload" | run_legacy_fallback
