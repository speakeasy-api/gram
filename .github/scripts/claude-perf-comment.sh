#!/usr/bin/env bash
# Renders an "@claude perf" review (.github/workflows/claude-perf.yml) as its
# pull request comment and prints it on stdout.
#
#   REPORT=<json> MARKER=<html comment> HEAD_SHA=<sha> RUN_URL=<url> claude-perf-comment.sh
#
# REPORT is Claude's structured output, so it is untrusted, and the comment is
# public. The script fails closed, printing nothing: when REPORT is not a
# complete review, and when the rendered comment contains anything shaped like
# a credential, an email address or a UUID.
set -euo pipefail

: "${MARKER:?}" "${HEAD_SHA:?}" "${RUN_URL:?}"
REPORT=${REPORT:-}

if ! jq -e '
  (.verdict | type) == "string" and (.summary | type) == "string"
  and (.findings | type) == "array" and (.baselines | type) == "array"
' <<<"$REPORT" >/dev/null 2>&1; then
  echo "::error::The perf review is missing or incomplete, so nothing was posted." >&2
  exit 1
fi

body=$(jq -r --arg marker "$MARKER" --arg head "$HEAD_SHA" --arg run "$RUN_URL" '
  def cell: tostring | gsub("[|]"; "\\|");
  def num: if . == null then "?" else tostring end;
  $marker + "\n### Performance review: `" + .verdict + "`\n\n" + .summary + "\n"
  + (if (.findings | length) > 0 then
      "\n#### Findings\n" + ([.findings[] |
        "- **" + .severity + "** `" + .file + (if .line then ":" + (.line | tostring) else "" end) + "`: "
        + .title + ". " + .detail + (if .evidence then " _Evidence:_ " + .evidence else "" end)
      ] | join("\n"))
    else "" end)
  + (if (.baselines | length) > 0 then
      "\n\n#### Production baselines (7 days)\n| Resource | Req/day | p50 ms | p95 ms | Query |\n|---|---|---|---|---|\n"
      + ([.baselines[] |
        "| `" + (.resource | cell) + "` | " + (.requests_per_day | num) + " | " + (.p50_ms | num)
        + " | " + (.p95_ms | num) + " | `" + (.query | cell) + "` |"
      ] | join("\n"))
    else "" end)
  + (if .mcp_status then "\n\n_MCP: " + .mcp_status + "_" else "" end)
  + "\n\n<sub>Commit " + $head[0:7] + " · [run](" + $run + ") · Claude Code with Speakeasy MCP</sub>"
' <<<"$REPORT")

# JWTs, GitHub tokens, Anthropic keys, Speakeasy keys, emails and UUIDs.
redact='eyJ[a-z0-9_-]{10,}\.|gh[oprsu]_[a-z0-9]{20,}|github_pat_|sk-ant-|gram_(live|test|local|dj)_|[a-z0-9._%+-]+@[a-z0-9.-]+\.[a-z]{2,}|[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}'
if grep -Eiq "$redact" <<<"$body"; then
  echo "::error::The perf review matched the redaction check, so nothing was posted." >&2
  exit 1
fi

printf '%s\n' "$body"
