#!/usr/bin/env bash

#MISE description="Start up the Speakeasy Admin dashboard dev server"
#MISE hide=true

set -e

# This server keeps about 130 MB of live heap.
# shellcheck source=.mise-tasks/start/_vite-memory.sh
source "${BASH_SOURCE[0]%/*}/_vite-memory.sh"
vite_memory_limits 640

exec aube run --filter ./client/admin dev
