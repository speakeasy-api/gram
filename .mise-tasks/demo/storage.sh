#!/usr/bin/env bash
#MISE description="Write a generated Parquet fixture through a local Pub/Sub storage runner"
#MISE dir="{{ config_root }}"

set -euo pipefail
go run ./infra/internal/storagefixture/cmd "$@"
