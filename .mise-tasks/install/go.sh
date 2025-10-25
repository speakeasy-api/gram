#!/usr/bin/env bash
#MISE description="Install Go dependencies"
#MISE hide=true
#MISE dir="{{ config_root }}"

set -e

echo "⏳ Downloading Go dependencies"
go mod download

echo "⏳ Installing Go tools"
go install tool