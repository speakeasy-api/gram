#!/usr/bin/env bash
#MISE description="Install Python dependencies"
#MISE hide=true
#MISE dir="{{ config_root }}"

exec uv sync "$@"