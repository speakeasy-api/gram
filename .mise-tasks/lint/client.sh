#!/usr/bin/env bash

#MISE description="Run linting on all client projects using pnpm"
#MISE sources=["client/**/*.ts", "client/**/*.tsx", "client/**/*.js", "client/**/*.jsx"]
#MISE outputs=["client/**/*.ts", "client/**/*.tsx", "client/**/*.js", "client/**/*.jsx"]

set -e

exec pnpm lint