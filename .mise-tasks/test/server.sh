#!/usr/bin/env bash

#MISE dir="{{ config_root }}/server"
#MISE description="Test the server with optional coverage generation"

set -e

# Generate unique ID for this test run (allows parallel execution)
# Use uuidgen which is available on both Linux and macOS
TEST_RUN_ID="${TEST_RUN_ID:-$(uuidgen | tr '[:upper:]' '[:lower:]' | cut -d'-' -f1)}"

# Container names with unique suffix
PG_CONTAINER="gram-test-pg-${TEST_RUN_ID}"
REDIS_CONTAINER="gram-test-redis-${TEST_RUN_ID}"
CH_CONTAINER="gram-test-clickhouse-${TEST_RUN_ID}"
TEMPORAL_CONTAINER="gram-test-temporal-${TEST_RUN_ID}"

# Network name for inter-container communication
NETWORK_NAME="gram-test-net-${TEST_RUN_ID}"

# Cleanup function
cleanup() {
    local exit_code=$?
    echo "Cleaning up test containers..."
    docker rm -f "$PG_CONTAINER" "$REDIS_CONTAINER" "$CH_CONTAINER" "$TEMPORAL_CONTAINER" 2>/dev/null || true
    docker network rm "$NETWORK_NAME" 2>/dev/null || true
    exit "$exit_code"
}
trap cleanup EXIT INT TERM

echo "Starting test infrastructure (run id: ${TEST_RUN_ID})..."

# Create isolated network
docker network create "$NETWORK_NAME" >/dev/null 2>&1 || true

# Start PostgreSQL with tmpfs for speed (matching testenv)
docker run -d --name "$PG_CONTAINER" \
    --network "$NETWORK_NAME" \
    -e POSTGRES_USER=gotest \
    -e POSTGRES_PASSWORD=gotest \
    -e POSTGRES_DB=gotestdb \
    -e PGDATA=/var/lib/postgresql/data \
    --tmpfs /var/lib/postgresql/data:rw \
    -P \
    pgvector/pgvector:pg17 >/dev/null

# Start Redis (matching testenv: redis:6.2-alpine)
docker run -d --name "$REDIS_CONTAINER" \
    --network "$NETWORK_NAME" \
    -P \
    redis:6.2-alpine >/dev/null

# Start ClickHouse (matching testenv: clickhouse/clickhouse-server:25.8.3)
docker run -d --name "$CH_CONTAINER" \
    --network "$NETWORK_NAME" \
    -e CLICKHOUSE_USER=gram \
    -e CLICKHOUSE_PASSWORD=gram \
    -e CLICKHOUSE_DB=default \
    -P \
    clickhouse/clickhouse-server:25.8.3 >/dev/null

# Start Temporal dev server (matching testenv: temporalio/server)
docker run -d --name "$TEMPORAL_CONTAINER" \
    --network "$NETWORK_NAME" \
    -P \
    --entrypoint temporal \
    temporalio/server:1.27.2 \
    server start-dev --ip 0.0.0.0 --namespace "test_${TEST_RUN_ID}" >/dev/null

echo "Waiting for containers to be ready..."

# Wait for PostgreSQL to be ready
wait_for_postgres() {
    local max_attempts=30
    local attempt=0
    while [ $attempt -lt $max_attempts ]; do
        if docker exec "$PG_CONTAINER" pg_isready -U gotest -d gotestdb >/dev/null 2>&1; then
            return 0
        fi
        attempt=$((attempt + 1))
        sleep 1
    done
    echo "PostgreSQL failed to start"
    return 1
}

# Wait for Redis to be ready
wait_for_redis() {
    local max_attempts=30
    local attempt=0
    while [ $attempt -lt $max_attempts ]; do
        if docker exec "$REDIS_CONTAINER" redis-cli ping >/dev/null 2>&1; then
            return 0
        fi
        attempt=$((attempt + 1))
        sleep 1
    done
    echo "Redis failed to start"
    return 1
}

# Wait for ClickHouse to be ready
wait_for_clickhouse() {
    local max_attempts=30
    local attempt=0
    while [ $attempt -lt $max_attempts ]; do
        if docker exec "$CH_CONTAINER" clickhouse-client --user gram --password gram -q "SELECT 1" >/dev/null 2>&1; then
            return 0
        fi
        attempt=$((attempt + 1))
        sleep 1
    done
    echo "ClickHouse failed to start"
    return 1
}

# Wait for Temporal to be ready
wait_for_temporal() {
    local max_attempts=60
    local attempt=0
    while [ $attempt -lt $max_attempts ]; do
        if docker exec "$TEMPORAL_CONTAINER" temporal operator cluster health >/dev/null 2>&1; then
            return 0
        fi
        attempt=$((attempt + 1))
        sleep 1
    done
    echo "Temporal failed to start"
    return 1
}

# Wait for all services in parallel
wait_for_postgres &
pg_wait_pid=$!
wait_for_redis &
redis_wait_pid=$!
wait_for_clickhouse &
ch_wait_pid=$!
wait_for_temporal &
temporal_wait_pid=$!

wait $pg_wait_pid || exit 1
wait $redis_wait_pid || exit 1
wait $ch_wait_pid || exit 1
wait $temporal_wait_pid || exit 1

echo "Initializing database schemas..."

# Get mapped ports
PG_PORT=$(docker port "$PG_CONTAINER" 5432 | head -1 | cut -d: -f2)
REDIS_PORT=$(docker port "$REDIS_CONTAINER" 6379 | head -1 | cut -d: -f2)
CH_NATIVE_PORT=$(docker port "$CH_CONTAINER" 9000 | head -1 | cut -d: -f2)
CH_HTTP_PORT=$(docker port "$CH_CONTAINER" 8123 | head -1 | cut -d: -f2)
TEMPORAL_PORT=$(docker port "$TEMPORAL_CONTAINER" 7233 | head -1 | cut -d: -f2)

# Initialize PostgreSQL with schema
docker exec -i "$PG_CONTAINER" psql -U gotest -d gotestdb < database/schema.sql >/dev/null

# Mark template database for cloning (matching testenv behavior)
docker exec "$PG_CONTAINER" psql -U gotest -d gotestdb -c "ALTER DATABASE gotestdb WITH is_template = true;" >/dev/null

# Initialize ClickHouse with schema
docker exec -i "$CH_CONTAINER" clickhouse-client --user gram --password gram < clickhouse/schema.sql >/dev/null

# Export environment variables for tests
export TEST_RUN_ID
export TEST_POSTGRES_HOST="127.0.0.1"
export TEST_POSTGRES_PORT="$PG_PORT"
export TEST_POSTGRES_USER="gotest"
export TEST_POSTGRES_PASSWORD="gotest"
export TEST_POSTGRES_DB="gotestdb"
export TEST_POSTGRES_URL="postgres://gotest:gotest@127.0.0.1:${PG_PORT}/gotestdb?sslmode=disable"

export TEST_REDIS_HOST="127.0.0.1"
export TEST_REDIS_PORT="$REDIS_PORT"
export TEST_REDIS_URL="redis://127.0.0.1:${REDIS_PORT}"

export TEST_CLICKHOUSE_HOST="127.0.0.1"
export TEST_CLICKHOUSE_NATIVE_PORT="$CH_NATIVE_PORT"
export TEST_CLICKHOUSE_HTTP_PORT="$CH_HTTP_PORT"
export TEST_CLICKHOUSE_USER="gram"
export TEST_CLICKHOUSE_PASSWORD="gram"
export TEST_CLICKHOUSE_DB="default"

export TEST_TEMPORAL_HOST="127.0.0.1"
export TEST_TEMPORAL_PORT="$TEMPORAL_PORT"
export TEST_TEMPORAL_ADDRESS="127.0.0.1:${TEMPORAL_PORT}"
export TEST_TEMPORAL_NAMESPACE="test_${TEST_RUN_ID}"

echo "Test infrastructure ready:"
echo "  PostgreSQL: localhost:${PG_PORT}"
echo "  Redis:      localhost:${REDIS_PORT}"
echo "  ClickHouse: localhost:${CH_NATIVE_PORT} (native), localhost:${CH_HTTP_PORT} (http)"
echo "  Temporal:   localhost:${TEMPORAL_PORT}"
echo ""

# Run tests
# PostgreSQL advisory locks serialize template cloning, allowing parallel execution

# Default to ./... if no package pattern is provided
args=("$@")
has_package=false
for arg in "${args[@]}"; do
  if [[ "$arg" == ./* ]] || [[ "$arg" == *... ]] || [[ "$arg" =~ ^[a-zA-Z] && ! "$arg" =~ ^- ]]; then
    has_package=true
    break
  fi
done

if [ "$has_package" = false ]; then
  args=("./..." "${args[@]}")
fi

gotestsum --junitfile junit-report.xml --format-hide-empty-pkg -- "${args[@]}"
test_exit_code=$?

exit "$test_exit_code"
