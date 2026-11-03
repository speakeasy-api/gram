#!/usr/bin/env bash
# Renders an "@claude perf" review (.github/workflows/claude-perf.yml) as its
# pull request comment and prints it on stdout.
#
#   REPORT=<json> MARKER=<html comment> HEAD_SHA=<sha> RUN_URL=<url> \
#     [LATEST_SHA=<sha>] claude-perf-comment.sh
#
# REPORT is Claude's structured output, so it is untrusted, and the comment is
# public. Its text renders as plain text on one line: no Markdown, HTML, links
# or @mentions. The script fails closed: when REPORT is not a complete review,
# or when the report or the rendered comment contains anything shaped like a
# credential, an email address or a UUID, it prints a "no verdict" notice in
# place of the review and exits 1. HEAD_SHA is the reviewed commit; when
# LATEST_SHA, the pull request's head at posting time, differs, the review
# says so.
set -euo pipefail

: "${MARKER:?}" "${HEAD_SHA:?}" "${RUN_URL:?}"
REPORT=${REPORT:-}
LATEST_SHA=${LATEST_SHA:-}

no_verdict() {
  echo "::error::$1" >&2
  printf '%s\n### Performance review: no verdict\n\nThe review of commit %s did not produce a report that can be posted. See the [run](%s).\n' \
    "$MARKER" "${HEAD_SHA:0:7}" "$RUN_URL"
  exit 1
}

jq -e '
  (.verdict | type) == "string" and (.summary | type) == "string"
  and (.findings | type) == "array" and (.baselines | type) == "array"
' <<<"$REPORT" >/dev/null 2>&1 || no_verdict "The perf review is missing or incomplete."

# A value as the comment shows it, on one line. The comment and the redaction
# check below both use it, so they read every value the same way.
line_def='def line: tostring | gsub("[[:space:][:cntrl:]]+"; " ");'

body=$(jq -r --arg marker "$MARKER" --arg head "$HEAD_SHA" --arg latest "$LATEST_SHA" --arg run "$RUN_URL" "$line_def"'
  # Every ASCII punctuation character is backslash-escaped, so no Markdown,
  # HTML, entity or autolink survives, and a zero-width space after @ and #
  # stops mentions and cross-references.
  def text: line | gsub("(?<c>[!-/:-@\\[-`{-~])"; "\\\(.c)") | gsub("(?<c>[@#])"; "\(.c)\u200b");
  # A backtick would end the code span.
  def code: "`" + (line | gsub("`"; "\u0027")) + "`";
  def cell: code | gsub("[|]"; "\\|");
  def num: if type == "number" then tostring else "?" end;
  "\($marker)\n### Performance review: \(.verdict | code)\n\n\(.summary | text)\n"
  + (if .findings != [] then
      "\n#### Findings\n" + ([.findings[] |
        "- **\(.severity | text)** \(if (.line | type) == "number" then "\(.file):\(.line)" else .file end | code): \(.title | text). \(.detail | text)"
        + (if .evidence then " _Evidence:_ \(.evidence | text)" else "" end)
      ] | join("\n"))
    else "" end)
  + (if .baselines != [] then
      "\n\n#### Production baselines (7 days)\n| Resource | Req/day | p50 ms | p95 ms | Query |\n|---|---|---|---|---|\n"
      + ([.baselines[] |
        "| \(.resource | cell) | \(.requests_per_day | num) | \(.p50_ms | num) | \(.p95_ms | num) | \(.query | cell) |"
      ] | join("\n"))
    else "" end)
  + (if .mcp_status then "\n\n_MCP: \(.mcp_status | text)_" else "" end)
  + (if $latest != "" and $latest != $head then
      "\n\n_The pull request is now at \($latest[0:7]), after this review. Comment `@claude perf` to review it._"
    else "" end)
  + "\n\n<sub>Commit \($head[0:7]) · [run](\($run)) · Claude Code with Speakeasy MCP</sub>"
' <<<"$REPORT" 2>/dev/null) || no_verdict "The perf review could not be rendered."

patterns=(
  'eyJ[a-z0-9_-]{10,}\.'                                         # JWTs
  'gh[oprsu]_[a-z0-9]{20,}' 'github_pat_'                        # GitHub tokens
  'sk-ant-'                                                      # Anthropic keys
  'gram_(live|test|local|dj)_'                                   # Speakeasy API keys (auth.APIKeyPrefix) and device join tokens (workloadpolicy)
  'xox[abeprs]-'                                                 # Slack tokens
  '[rs]k_live_'                                                  # Stripe secret and restricted keys
  '(akia|asia)[0-9a-z]{16}'                                      # AWS access key IDs
  'aiza[0-9a-z_-]{35}'                                           # Google API keys
  '-----begin [a-z ]*private key'                                # PEM and PGP private keys
  '://[^/@[:space:]]*:[^/@[:space:]]*@'                          # URLs with a password
  'bearer[[:space:]]+[a-z0-9._~+/=-]{20,}'                       # bearer tokens
  '[a-z0-9._%+-]+@[a-z0-9.-]+\.[a-z]{2,}'                        # emails
  '[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}' # UUIDs
)
# Every value of the report as it reads in the comment, on one line and before
# escaping, which would split an email address or a token; then the comment.
values=$(jq -r "$line_def"' .. | scalars | line' <<<"$REPORT")
grep_status=0
grep -Eiq -f <(printf '%s\n' "${patterns[@]}") <<<"$values"$'\n'"$body" || grep_status=$?
case $grep_status in
  1) ;;
  0) no_verdict "The perf review matched the redaction check." ;;
  *) no_verdict "The redaction check could not run." ;;
esac

printf '%s\n' "$body"
