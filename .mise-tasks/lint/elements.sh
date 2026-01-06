#!/usr/bin/env bash

#MISE description="Run linting on the elements package"

set -e

cd elements
exec pnpm lint
