#!/usr/bin/env bash
#
# Render the prompt-injection accuracy comment body. Emits Markdown to stdout
# starting with a marker comment so the workflow can find and update the
# sticky comment idempotently.
#
# usage: risk-metrics-comment.sh <pr.json> [main.json]
#   pr.json   — required; the current PR run's metrics envelope.
#   main.json — optional; main's last successful run, for delta cells.

set -euo pipefail

PR_FILE="${1:-}"
MAIN_FILE="${2:-}"

if [ -z "$PR_FILE" ] || [ ! -f "$PR_FILE" ]; then
  echo "usage: $0 <pr.json> [main.json]" >&2
  exit 2
fi

HAS_MAIN=0
if [ -n "$MAIN_FILE" ] && [ -f "$MAIN_FILE" ]; then
  HAS_MAIN=1
fi

# Marker so the workflow can locate this comment among a PR's comments.
echo '<!-- risk-accuracy-metrics -->'
echo
echo '### Prompt-injection accuracy'
echo

PR_TOTAL=$(jq '.summary.total' "$PR_FILE")
PR_TP=$(jq '.summary.counts.tp' "$PR_FILE")
PR_FP=$(jq '.summary.counts.fp' "$PR_FILE")
PR_TN=$(jq '.summary.counts.tn' "$PR_FILE")
PR_FN=$(jq '.summary.counts.fn' "$PR_FILE")
PR_MALICIOUS=$((PR_TP + PR_FN))
PR_BENIGN=$((PR_FP + PR_TN))
PR_SHA=$(jq -r '.git_sha' "$PR_FILE")
PR_TS=$(jq -r '.timestamp' "$PR_FILE")
PR_SHA_SHORT="${PR_SHA:0:8}"

if [ "$HAS_MAIN" -eq 1 ]; then
  MAIN_TOTAL=$(jq '.summary.total' "$MAIN_FILE")
  MAIN_SHA=$(jq -r '.git_sha' "$MAIN_FILE")
  MAIN_SHA_SHORT="${MAIN_SHA:0:8}"
  CORPUS_DELTA=$((PR_TOTAL - MAIN_TOTAL))
  if [ "$CORPUS_DELTA" -ne 0 ]; then
    DELTA_STR=$(printf '%+d' "$CORPUS_DELTA")
    echo "Corpus: $PR_TOTAL cases ($PR_MALICIOUS malicious / $PR_BENIGN benign) — **$DELTA_STR vs main**"
  else
    echo "Corpus: $PR_TOTAL cases ($PR_MALICIOUS malicious / $PR_BENIGN benign)"
  fi
  echo "Baseline: \`$MAIN_SHA_SHORT\` (main) · This PR: \`$PR_SHA_SHORT\` · $PR_TS"
else
  echo "Corpus: $PR_TOTAL cases ($PR_MALICIOUS malicious / $PR_BENIGN benign)"
  echo "This PR: \`$PR_SHA_SHORT\` · $PR_TS"
  echo
  echo "_No baseline yet — main has no recorded run for this artifact. Tracking starts on the next merge._"
fi
echo

echo '#### Overall'
echo
echo '| metric | value | Δ vs main |'
echo '| --- | --- | --- |'

emit_overall() {
  local metric="$1"
  local field="$2"
  local pr_val
  pr_val=$(jq -r ".summary.overall.${field} | . * 10000 | round / 10000" "$PR_FILE")
  if [ "$HAS_MAIN" -eq 1 ]; then
    local main_val delta_str
    main_val=$(jq -r ".summary.overall.${field} | . * 10000 | round / 10000" "$MAIN_FILE")
    delta_str=$(awk -v a="$pr_val" -v b="$main_val" 'BEGIN {
      d = a - b
      if (d > 0)       printf "+%.4f", d
      else if (d < 0)  printf "%.4f", d
      else             printf "0"
    }')
    echo "| $metric | $pr_val | $delta_str |"
  else
    echo "| $metric | $pr_val | — |"
  fi
}

emit_overall precision precision
emit_overall recall recall
emit_overall f1 f1
emit_overall accuracy accuracy
emit_overall fp_rate fp_rate
echo

echo '#### By source'
echo

if [ "$HAS_MAIN" -eq 1 ]; then
  echo '| source | TP | FP | TN | FN | recall | fp_rate | Δ recall | Δ fp_rate |'
  echo '| --- | --- | --- | --- | --- | --- | --- | --- | --- |'
  jq -r --slurpfile main "$MAIN_FILE" '
    def fmt: . * 10000 | round / 10000;
    def fmt_delta($d):
      if $d == null then "—"
      elif $d > 0   then "+" + ($d | fmt | tostring)
      elif $d < 0   then ($d | fmt | tostring)
      else "0" end;
    def main_metric($source; $field):
      ($main[0].summary.by_source[]? | select(.source == $source) | .metrics[$field]) // null;
    .summary.by_source[]
    | . as $s
    | (main_metric($s.source; "recall")) as $mr
    | (main_metric($s.source; "fp_rate")) as $mf
    | "| \($s.source) | \($s.counts.tp) | \($s.counts.fp) | \($s.counts.tn) | \($s.counts.fn) | \($s.metrics.recall|fmt) | \($s.metrics.fp_rate|fmt) | \(if $mr == null then "—" else fmt_delta($s.metrics.recall - $mr) end) | \(if $mf == null then "—" else fmt_delta($s.metrics.fp_rate - $mf) end) |"
  ' "$PR_FILE"
else
  echo '| source | TP | FP | TN | FN | recall | fp_rate |'
  echo '| --- | --- | --- | --- | --- | --- | --- |'
  jq -r '
    def fmt: . * 10000 | round / 10000;
    .summary.by_source[]
    | "| \(.source) | \(.counts.tp) | \(.counts.fp) | \(.counts.tn) | \(.counts.fn) | \(.metrics.recall|fmt) | \(.metrics.fp_rate|fmt) |"
  ' "$PR_FILE"
fi
echo

echo '#### By rule'
echo

if [ "$HAS_MAIN" -eq 1 ]; then
  echo '| rule | TP | Δ TP | FP | Δ FP |'
  echo '| --- | --- | --- | --- | --- |'
  jq -r --slurpfile main "$MAIN_FILE" '
    def fmt_delta($d):
      if $d > 0   then "+" + ($d | tostring)
      elif $d < 0 then ($d | tostring)
      else "0" end;
    def main_rule($rule_id; $field):
      ($main[0].summary.by_rule[]? | select(.rule_id == $rule_id) | .[$field]) // 0;
    .summary.by_rule[]
    | . as $r
    | (main_rule($r.rule_id; "tp_count")) as $mtp
    | (main_rule($r.rule_id; "fp_count")) as $mfp
    | "| \($r.rule_id) | \($r.tp_count) | \(fmt_delta($r.tp_count - $mtp)) | \($r.fp_count) | \(fmt_delta($r.fp_count - $mfp)) |"
  ' "$PR_FILE"
else
  echo '| rule | TP | FP |'
  echo '| --- | --- | --- |'
  jq -r '
    .summary.by_rule[]
    | "| \(.rule_id) | \(.tp_count) | \(.fp_count) |"
  ' "$PR_FILE"
fi
echo

echo '<sub>Generated by <code>.github/scripts/risk-metrics-comment.sh</code>. The hard floor on FP-rate lives in <code>server/internal/background/activities/risk_analysis/testdata/prompt_injection/floors.json</code>.</sub>'
