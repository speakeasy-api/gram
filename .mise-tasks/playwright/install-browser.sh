#!/usr/bin/env bash

#MISE description="Install Google Chrome (the Playwright browser) and system dependencies"

set -e

exec playwright-cli install-browser chrome --with-deps "$@"
