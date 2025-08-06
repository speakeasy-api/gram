#!/usr/bin/env bash
#MISE dir="{{ config_root }}/server"
#MISE description="Regenerate atlas.sum"

set -e

exec atlas migrate hash \
  --config file://atlas.hcl
