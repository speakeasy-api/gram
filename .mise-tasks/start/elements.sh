#!/usr/bin/env bash
#MISE description="Start up the Gram Dashboard dev server"

#USAGE flag "--ssl-key <file>" env="GRAM_SSL_KEY_FILE" help="Path to the SSL key file for HTTPS"
#USAGE flag "--ssl-cert <file>" env="GRAM_SSL_CERT_FILE" help="Path to the SSL certificate file for HTTPS"

set -e

ssl_key=${usage_ssl_key:-}
ssl_cert=${usage_ssl_cert:-}

args=()
if [ -n "$ssl_key" ] && [ -n "$ssl_cert" ]; then
    args+=("--https")
    args+=("--ssl-key" "$ssl_key")
    args+=("--ssl-cert" "$ssl_cert")
fi

args+=("--port" "$ELEMENTS_STORYBOOK_PORT")

exec pnpm --filter ./elements storybook "${args[@]}"
