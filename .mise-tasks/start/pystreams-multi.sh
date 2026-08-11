#!/usr/bin/env bash

#MISE dir="{{ config_root }}/pystreams"
#MISE description="Start up python stream subscribers"
#MISE hide=true

exec uv run multi "$@"