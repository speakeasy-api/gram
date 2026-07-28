#!/usr/bin/env bash

#MISE description="Install the Playwright browser and system dependencies"

set -e

exec playwright-cli install-browser chromium --with-deps "$@"
