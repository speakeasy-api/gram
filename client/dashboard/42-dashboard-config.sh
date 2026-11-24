#!/bin/sh
set -eu

# Deployment-specific dashboard settings, a JSON object, substituted into the
# gram-dashboard-config meta tag at container start (see
# src/lib/runtime-config.ts). Each entrypoint script substitutes only its own
# variables, so order relative to 40-* and 41-* does not matter.
index=/usr/share/nginx/html/index.html

# The value lands in an HTML attribute, so escape the characters that could
# close the attribute or open a tag. The browser unescapes them back to JSON.
GRAM_DASHBOARD_CONFIG=$(
  printf %s "$GRAM_DASHBOARD_CONFIG" | sed 's/&/\&amp;/g; s/"/\&quot;/g; s/</\&lt;/g; s/>/\&gt;/g'
)
export GRAM_DASHBOARD_CONFIG
envsubst '$GRAM_DASHBOARD_CONFIG' <"$index" >/tmp/index-dashboard-config.html
cat /tmp/index-dashboard-config.html >"$index"
