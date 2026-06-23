#!/usr/bin/env bash
# Build the tunnel binaries (gateway, agent) as static CGO-free linux images and
# load them into the kind cluster. Matches the kind node arch. (sample-mcp is
# optional — the MCP server is deployed elsewhere; re-add it in TARGETS to build.)
set -euo pipefail

CLUSTER="${KIND_CLUSTER:-local-mess}"
LOCAL_K8S_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$LOCAL_K8S_DIR/../.." && pwd)"

ARCH="$(uname -m)"
case "$ARCH" in
  arm64|aarch64) GOARCH=arm64; PLATFORM=linux/arm64 ;;
  x86_64|amd64)  GOARCH=amd64; PLATFORM=linux/amd64 ;;
  *) echo "unsupported arch: $ARCH" >&2; exit 1 ;;
esac

cd "$REPO_ROOT"

# name:path pairs (space-separated; macOS bash 3.2 has no associative arrays).
# sample-mcp removed: the MCP server is deployed elsewhere. To build it for local
# testing, append: sample-mcp:./tunnel/cmd/sample-mcp
TARGETS="tunnel-gateway:./tunnel/cmd/tunnel-gateway tunnel-agent:./tunnel/cmd/tunnel-agent"

STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT
mkdir -p "$STAGE/bin"
cp "$LOCAL_K8S_DIR/Dockerfile" "$STAGE/Dockerfile"

for entry in $TARGETS; do
  name="${entry%%:*}"
  path="${entry#*:}"
  echo "==> Building $name (linux/$GOARCH)"
  CGO_ENABLED=0 GOOS=linux GOARCH="$GOARCH" go build -o "$STAGE/bin/$name" "$path"
  img="gram-$name:local"
  echo "==> Image $img ($PLATFORM)"
  docker build --platform "$PLATFORM" --build-arg "BIN=$name" -t "$img" "$STAGE"
  echo "==> Loading $img into kind '$CLUSTER'"
  kind load docker-image "$img" --name "$CLUSTER"
done

echo "Done. Loaded into kind-$CLUSTER (imagePullPolicy: Never):"
echo "  - gram-tunnel-gateway:local"
echo "  - gram-tunnel-agent:local"
