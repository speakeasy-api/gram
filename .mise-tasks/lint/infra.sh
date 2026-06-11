#!/usr/bin/env bash

#MISE description="Run linters against infra changes"
#MISE dir="{{ config_root }}"

#USAGE flag "--against <against>" default=".git" help="The source, module, or image to check against. Must be one of format [binpb,dir,git,json,mod,protofile,tar,txtpb,yaml,zip]"

set -eo pipefail

against="${usage_against:?--against is required}"

buf lint
buf breaking --against "${against}"

uv run --directory infra --extra dev pyrefly check --summarize-errors --min-severity warn
uv run --directory infra --extra dev ty check