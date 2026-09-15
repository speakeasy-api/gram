#!/bin/sh
set -eu

index=/usr/share/nginx/html/index.html

# Every value here is rendered into an HTML attribute in index.html, so
# escape the characters that could close the attribute or open a tag. Branch
# names are the reason this matters beyond theory: refs may contain & and <.
escape_html() {
  printf %s "$1" | sed 's/&/\&amp;/g; s/"/\&quot;/g; s/</\&lt;/g; s/>/\&gt;/g'
}

GRAM_ADMIN_SERVER_URL=$(escape_html "$GRAM_ADMIN_SERVER_URL")
GRAM_BRANCH=$(escape_html "$GRAM_BRANCH")
GRAM_COMMIT_SHA=$(escape_html "$GRAM_COMMIT_SHA")
GRAM_PR_NUMBER=$(escape_html "$GRAM_PR_NUMBER")
GRAM_IS_PREVIEW=$(escape_html "$GRAM_IS_PREVIEW")
GRAM_GUTTERNOTE_KEY=$(escape_html "$GRAM_GUTTERNOTE_KEY")
export GRAM_ADMIN_SERVER_URL GRAM_BRANCH GRAM_COMMIT_SHA GRAM_PR_NUMBER \
  GRAM_IS_PREVIEW GRAM_GUTTERNOTE_KEY

envsubst '$GRAM_ADMIN_SERVER_URL $GRAM_BRANCH $GRAM_COMMIT_SHA $GRAM_PR_NUMBER $GRAM_IS_PREVIEW $GRAM_GUTTERNOTE_KEY' <"$index" >/tmp/index.html
cat /tmp/index.html >"$index"
