#!/usr/bin/env bash

# Extract connection details from running cloud-sql-proxy
PROXY_INFO=$(ps aux | grep cloud-sql-proxy | grep -v grep | head -1)

if [ -z "$PROXY_INFO" ]; then
    echo "ERROR: No cloud-sql-proxy process found. Please run 'mise gcp:db dev' first."
    exit 1
fi

# Extract port from the proxy command
PORT=$(echo "$PROXY_INFO" | grep -o 'port=[0-9]*' | cut -d= -f2)

if [ -z "$PORT" ]; then
    echo "ERROR: Could not extract port from proxy process"
    exit 1
fi

# Get the authenticated user
USER=$(gcloud auth list --format="value(ACCOUNT)" --limit 1)

echo "Found cloud-sql-proxy running on port $PORT"
echo "Using database user: $USER"

# Set the DATABASE_URL and run the backfill
export DATABASE_URL="postgresql://$USER@127.0.0.1:$PORT/gram?sslmode=disable"

echo "Running script..."
go run backfillToolsetVersions.go