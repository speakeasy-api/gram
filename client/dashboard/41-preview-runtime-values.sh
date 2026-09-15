#!/bin/sh
set -eu

# Preview-environment values for index.html, substituted at container start.
# Runs after 40-admin-server-url.sh; each script substitutes only its own
# variables, so the two passes are independent and order does not matter.
#
# Every value below is rendered into an HTML attribute, so escape the
# characters that could close the attribute or open a tag. Branch names are
# the reason this matters beyond theory: refs may contain & and <.
index=/usr/share/nginx/html/index.html

escape_html() {
  printf %s "$1" | sed 's/&/\&amp;/g; s/"/\&quot;/g; s/</\&lt;/g; s/>/\&gt;/g'
}

GRAM_IS_PREVIEW=$(escape_html "$GRAM_IS_PREVIEW")
GRAM_GUTTERNOTE_KEY=$(escape_html "$GRAM_GUTTERNOTE_KEY")
GRAM_BRANCH=$(escape_html "$GRAM_BRANCH")
GRAM_COMMIT_SHA=$(escape_html "$GRAM_COMMIT_SHA")
GRAM_PR_NUMBER=$(escape_html "$GRAM_PR_NUMBER")
export GRAM_IS_PREVIEW GRAM_GUTTERNOTE_KEY GRAM_BRANCH GRAM_COMMIT_SHA \
  GRAM_PR_NUMBER

envsubst '$GRAM_IS_PREVIEW $GRAM_GUTTERNOTE_KEY $GRAM_BRANCH $GRAM_COMMIT_SHA $GRAM_PR_NUMBER' <"$index" >/tmp/index-preview.html
cat /tmp/index-preview.html >"$index"
