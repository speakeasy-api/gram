#!/usr/bin/env bash

#MISE description="Call the Dagger Go pipeline (arguments are Dagger API functions and flags)"
#MISE dir="{{ config_root }}"

set -euo pipefail

exec dagger api call go-pipeline "$@"
