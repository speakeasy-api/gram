#!/usr/bin/env bash
# Build the gram server + migrations images and load them into the kind cluster.
# The server/Dockerfile copies a prebuilt ./bin/gram (distroless, no Go toolchain),
# so we compile the static binary first, matching the kind node arch.
set -euo pipefail

CLUSTER="${KIND_CLUSTER:-local-mess}"
IMAGE="gram-server:local"
MIG_IMAGE="gram-migrations:local"
LOCAL_K8S_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$LOCAL_K8S_DIR/.." && pwd)"

cd "$REPO_ROOT/server"

# Match the kind node arch (arm64 on Apple Silicon, amd64 on Intel/most Linux).
ARCH="$(uname -m)"
case "$ARCH" in
  arm64|aarch64) GOARCH=arm64; PLATFORM=linux/arm64 ;;
  x86_64|amd64)  GOARCH=amd64; PLATFORM=linux/amd64 ;;
  *) echo "unsupported arch: $ARCH" >&2; exit 1 ;;
esac

echo "==> Building linux/$GOARCH gram binary"
mkdir -p bin
CGO_ENABLED=0 GOOS=linux GOARCH="$GOARCH" go build -o bin/gram ./

echo "==> Building image $IMAGE ($PLATFORM)"
docker build --platform "$PLATFORM" -t "$IMAGE" -f Dockerfile .

echo "==> Loading $IMAGE into kind cluster '$CLUSTER'"
kind load docker-image "$IMAGE" --name "$CLUSTER"

echo "==> Building migrations image $MIG_IMAGE ($PLATFORM)"
# server/.dockerignore whitelists only bin+migrations, so stage the files atlas
# and golang-migrate need into a clean temp context (no .dockerignore).
STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT
cp "$REPO_ROOT/server/atlas.hcl" "$STAGE/atlas.hcl"
cp -R "$REPO_ROOT/server/migrations" "$STAGE/migrations"
cp -R "$REPO_ROOT/server/clickhouse/local/golang_migrate" "$STAGE/golang_migrate"
cp "$LOCAL_K8S_DIR/migrations.Dockerfile" "$STAGE/Dockerfile"
docker build --platform "$PLATFORM" -t "$MIG_IMAGE" "$STAGE"

echo "==> Loading $MIG_IMAGE into kind cluster '$CLUSTER'"
kind load docker-image "$MIG_IMAGE" --name "$CLUSTER"

# ClickHouse with TLS certs/config (server client always uses TLS, port 9440).
CH_IMAGE="gram-clickhouse:local"
echo "==> Building clickhouse image $CH_IMAGE ($PLATFORM)"
docker build --platform "$PLATFORM" -t "$CH_IMAGE" "$REPO_ROOT/local/clickhouse"
echo "==> Loading $CH_IMAGE into kind cluster '$CLUSTER'"
kind load docker-image "$CH_IMAGE" --name "$CLUSTER"

echo "Done. Images loaded into kind-$CLUSTER (imagePullPolicy: Never):"
echo "  - $IMAGE"
echo "  - $MIG_IMAGE"
echo "  - $CH_IMAGE"
