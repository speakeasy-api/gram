#!/usr/bin/env bash

#MISE description="Run lint scripts across workspace packages using aube"

set -e

exec aube run -r --if-present lint
