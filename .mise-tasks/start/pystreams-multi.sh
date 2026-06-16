#!/usr/bin/env bash

#MISE dir="{{ config_root }}/pystreams"
#MISE description="Start up python stream subscribers"

exec uv run --no-sync multi "$@"