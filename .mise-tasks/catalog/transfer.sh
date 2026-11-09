#!/usr/bin/env bash

#MISE description="Run the local catalog transfer CLI with source configuration checks"
#MISE dir="{{ config_root }}"
#MISE hide=true

set -euo pipefail

operation="refresh"
previous=""
for argument in "$@"; do
    if [ "$previous" = "--operation" ]; then
        operation="$argument"
    elif [[ "$argument" == --operation=* ]]; then
        operation="${argument#--operation=}"
    fi
    previous="$argument"
done
if [ "$operation" = "reset" ]; then
    recovery="Check mise run catalog:reset --action status, then run mise run catalog:reset --action preview for a new plan. Review it and supply its exact plan ID and confirmation before applying. No apply retry was attempted."
else
    recovery="Retry mise run catalog:bootstrap, or mise run catalog:refresh for a new preview; apply only the reviewed exact plan ID. No seed/reset is needed."
fi

# Match Go's strconv.ParseBool, used by the server's source-managed flag.
case "${GRAM_CATALOG_SOURCE_MANAGED:-false}" in
    1|t|T|TRUE|true|True) ;;
    0|f|F|FALSE|false|False)
        echo "Production catalog is not populated. Set GRAM_CATALOG_SOURCE_MANAGED=true in mise.local.toml to enable source-managed mode." >&2
        exit 1
        ;;
    *)
        echo "Invalid GRAM_CATALOG_SOURCE_MANAGED. Set it to true or false in mise.local.toml." >&2
        exit 1
        ;;
esac

for key in GRAM_CATALOG_SOURCE_URL GRAM_CATALOG_SOURCE_AUDIENCE GRAM_CATALOG_SOURCE_READER; do
    if [ -z "${!key:-}" ]; then
        echo "Missing $key. Configure it in mise.local.toml [env]. $recovery" >&2
        exit 1
    fi
done

# The CLI handles local mock OIDC; source ADC stays in Admin. Never pass cookies or token files.
cd server
if ! go run . catalog-transfer "$@"; then
    echo "Catalog transfer failed. Check source URL/audience/reader in mise.local.toml and local server/dev-idp readiness." >&2
    echo "For ADC authentication, run gcloud auth application-default login. For production permission errors, request access through C1 (ConductorOne)." >&2
    echo "$recovery" >&2
    exit 1
fi
