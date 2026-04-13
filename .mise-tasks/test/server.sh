#!/usr/bin/env bash
#MISE dir="{{ config_root }}/server"
#MISE description="Test the server with optional coverage generation. It takes the same arguments as 'go test'."

declare -a EXIT_CALLBACKS=()

run_exit_callbacks() {
  for callback in "${EXIT_CALLBACKS[@]}"; do
    eval "$callback"
  done
}

trap run_exit_callbacks EXIT

register_exit_callback() {
  EXIT_CALLBACKS+=("$1")
}

# Check if flags are provided
cover=false
open_html=false
args=()

for arg in "$@"; do
  case $arg in
    --cover)
      cover=true
      shift ;;
    --html)
      open_html=true
      shift ;;
    *)
      args+=("$arg") ;;
  esac
done

if [ ${#args[@]} -eq 0 ]; then
  args=("-tags=inv.debug" "./...")
fi

if [ "$cover" = true ]; then
  args=("-coverprofile=cover.out" "-covermode=atomic" "${args[@]}")
fi


if [ "${TEST_WITH_INFRA:-}" = "1" ]; then
  echo "Starting test infrastructure in Docker containers..."

  random_hex=$(openssl rand -hex 4)

  pg_container_name="test-pgvector-${random_hex}"
  pg_container_id=$(docker run -d -q --rm \
    -p 5432 \
    --tmpfs /var/lib/postgresql/data:rw \
    --env POSTGRES_PASSWORD=gotest \
    --env POSTGRES_USER=gotest \
    --env POSTGRES_DB=gotestdb \
    --env PGDATA=/var/lib/postgresql/data \
    --volume "$(pwd)/database/schema.sql:/docker-entrypoint-initdb.d/00-init.sql:ro" \
    --volume "$(pwd)/database/testing.sql:/docker-entrypoint-initdb.d/01-testing.sql:ro" \
    --name "${pg_container_name}" \
    pgvector/pgvector:pg17)
  pg_port=$(docker port "${pg_container_name}" 5432/tcp | cut -d: -f2)
  pg_conn_string="postgres://gotest:gotest@127.0.0.1:${pg_port}/gotestdb?sslmode=disable"
  register_exit_callback "docker stop ${pg_container_name} > /dev/null 2>&1 || true"
  export TESTENV_POSTGRES_URI="${pg_conn_string}"
  echo "Started PostgreSQL container '${pg_container_name}' with connection string: ${pg_conn_string}"

  ch_container_name="test-clickhouse-${random_hex}"
  ch_container_id=$(docker run -d -q --rm \
    -p 9000 \
    --env CLICKHOUSE_USER=gotest \
    --env CLICKHOUSE_PASSWORD=gotest \
    --tmpfs /var/lib/clickhouse:rw \
    --tmpfs /var/log/clickhouse:rw \
    --volume "$(pwd)/clickhouse/schema.sql:/docker-entrypoint-initdb.d/00-init.sql:ro" \
    --name "${ch_container_name}" \
    clickhouse/clickhouse-server:25.8.3)
  ch_port=$(docker port "${ch_container_name}" 9000/tcp | cut -d: -f2)
  ch_conn_string="clickhouse://gotest:gotest@127.0.0.1:${ch_port}/default"
  register_exit_callback "docker stop ${ch_container_name} > /dev/null 2>&1 || true"
  export TESTENV_CLICKHOUSE_URI="${ch_conn_string}"
  until docker exec "${ch_container_id}" clickhouse-client --user gotest --password gotest --query "EXISTS TABLE default.telemetry_logs" 2>/dev/null | grep -q 1; do
    echo "Waiting for ClickHouse to be ready..."
    sleep 1
  done
  echo "Started ClickHouse container '${ch_container_name}' with connection string: ${ch_conn_string}"

  redis_container_name="test-redis-${random_hex}"
  redis_container_id=$(docker run -d -q --rm \
    -p 6379 \
    --tmpfs /data:rw \
    --name "${redis_container_name}" \
    redis:6.2-alpine)
  redis_port=$(docker port "${redis_container_name}" 6379/tcp | cut -d: -f2)
  redis_conn_string="redis://127.0.0.1:${redis_port}"
  register_exit_callback "docker stop ${redis_container_name} > /dev/null 2>&1 || true"
  export TESTENV_REDIS_URI="${redis_conn_string}"
  until docker exec "${redis_container_id}" redis-cli ping 2>/dev/null | grep -q PONG; do
    echo "Waiting for Redis to be ready..."
    sleep 1
  done
  echo "Started Redis container '${redis_container_name}' with connection string: ${redis_conn_string}"

  temporal_container_name="test-temporal-${random_hex}"
  temporal_container_id=$(docker run -d -q --rm \
    -p 7233 \
    --name "${temporal_container_name}" \
    --entrypoint "temporal" \
    temporalio/server:1.27.2 \
    server start-dev --headless --ip 0.0.0.0 --namespace default --db-filename /home/temporal/dev.db)
  temporal_port=$(docker port "${temporal_container_id}" 7233/tcp | cut -d: -f2)
  temporal_conn_string="temporal://127.0.0.1:${temporal_port}/default"
  register_exit_callback "docker stop ${temporal_container_name} > /dev/null 2>&1 || true"
  export TESTENV_TEMPORAL_URI="${temporal_conn_string}"
  until docker exec "${temporal_container_id}" tctl namespace list 2>/dev/null | grep -q "default"; do
    echo "Waiting for Temporal to be ready..."
    sleep 1
  done
  echo "Started Temporal container '${temporal_container_name}' with connection string: ${temporal_conn_string}"
fi

gotestsum --junitfile junit-report.xml --format-hide-empty-pkg -- "${args[@]}"
test_exit_code=$?

if [ "$cover" = true ] && [ -f "cover.out" ]; then
  grep -v "/gen/" cover.out > coverage_filtered.out
  mv coverage_filtered.out cover.out

  go tool cover -html=cover.out -o cover.html
  echo "Coverage report generated: cover.html"

  if [ "$open_html" = true ]; then
    if command -v open >/dev/null 2>&1; then
      open cover.html
    elif command -v xdg-open >/dev/null 2>&1; then
      xdg-open cover.html
    else
      echo "Could not open browser automatically. Please open cover.html manually."
    fi
  fi
fi

exit $test_exit_code
