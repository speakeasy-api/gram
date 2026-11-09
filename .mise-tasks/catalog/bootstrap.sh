#!/usr/bin/env bash

#MISE description="Bootstrap the local source-managed catalog when explicitly enabled"
#MISE dir="{{ config_root }}"

set -euo pipefail

# Match Go's strconv.ParseBool, used by the server's source-managed flag.
case "${GRAM_CATALOG_SOURCE_MANAGED:-false}" in
    1|t|T|TRUE|true|True) ;;
    0|f|F|FALSE|false|False)
        echo "Source-managed mode disabled: production catalog is not populated. No production access is required."
        exit 0
        ;;
    *)
        echo "Invalid GRAM_CATALOG_SOURCE_MANAGED. Set it to true or false in mise.local.toml." >&2
        exit 1
        ;;
esac

exec bash "$(dirname "$0")/transfer.sh" --action bootstrap
