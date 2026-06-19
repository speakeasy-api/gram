#!/usr/bin/env bash

#MISE description="Run linters against infra changes"
#MISE dir="{{ config_root }}"

#USAGE flag "--against <against>" default=".git" help="The source, module, or image to check against. Must be one of format [binpb,dir,git,json,mod,protofile,tar,txtpb,yaml,zip]"

set -eo pipefail

against="${usage_against:?--against is required}"

buf lint
buf breaking --against "${against}"

# ruff is provided by mise (see mise.toml), not the uv environment, so call it
# directly. It picks up the lint selection from infra/pyproject.toml and skips
# the buf-generated gen_py tree via that config's extend-exclude.
ruff check infra