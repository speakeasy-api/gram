#!/bin/sh
set -eu

# Preview-environment values for index.html, substituted at container start.
# Runs after 40-admin-server-url.sh; each script substitutes only its own
# variables, so the two passes are independent and order does not matter.
index=/usr/share/nginx/html/index.html

# The gutternote:* values are rendered into HTML attributes, so escape the
# characters that could close the attribute or open a tag. Branch names are
# the reason this matters beyond theory: refs may contain & and <.
escape_html() {
  printf %s "$1" | sed 's/&/\&amp;/g; s/"/\&quot;/g; s/</\&lt;/g; s/>/\&gt;/g'
}

GRAM_BRANCH=$(escape_html "$GRAM_BRANCH")
GRAM_COMMIT_SHA=$(escape_html "$GRAM_COMMIT_SHA")
GRAM_PR_NUMBER=$(escape_html "$GRAM_PR_NUMBER")

# The Gutternote widget tag is composed here rather than templated attribute by
# attribute, so index.html needs a single placeholder and an unconfigured host
# gets no tag at all instead of one with an empty key.
#
# GRAM_GUTTERNOTE_SCRIPT is the one value substituted as raw markup rather than
# escaped — it *is* markup. Everything in it is a literal written here except
# the key, which is escaped because it lands in an attribute.
#
# Both gates fail closed on the image's empty ENV defaults: prod, shared
# staging and local dev emit nothing. The key is a publishable client-side key
# (it is served in this HTML), not a secret.
GRAM_GUTTERNOTE_SCRIPT=""
if [ "$GRAM_IS_PREVIEW" = "true" ] && [ -n "$GRAM_GUTTERNOTE_KEY" ]; then
  GRAM_GUTTERNOTE_SCRIPT="<script src=\"https://cdn.gutternote.com/v1/widget.js\" data-gutternote-key=\"$(escape_html "$GRAM_GUTTERNOTE_KEY")\" defer></script>"
fi

export GRAM_BRANCH GRAM_COMMIT_SHA GRAM_PR_NUMBER GRAM_GUTTERNOTE_SCRIPT

envsubst '$GRAM_BRANCH $GRAM_COMMIT_SHA $GRAM_PR_NUMBER $GRAM_GUTTERNOTE_SCRIPT' <"$index" >/tmp/index-preview.html
cat /tmp/index-preview.html >"$index"
