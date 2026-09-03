#!/bin/sh
set -eu

index=/usr/share/nginx/html/index.html
GRAM_ADMIN_SERVER_URL=$(
  printf %s "$GRAM_ADMIN_SERVER_URL" | sed 's/&/\&amp;/g; s/"/\&quot;/g; s/</\&lt;/g; s/>/\&gt;/g'
)
export GRAM_ADMIN_SERVER_URL
envsubst '$GRAM_ADMIN_SERVER_URL' <"$index" >/tmp/index.html
cat /tmp/index.html >"$index"
