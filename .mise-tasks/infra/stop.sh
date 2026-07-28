#!/usr/bin/env bash

#MISE description="Stop all compose services"

set -e

source "$(dirname "${BASH_SOURCE[0]}")/../../local/lib/compose.sh"

compose --profile "*" down --remove-orphans
