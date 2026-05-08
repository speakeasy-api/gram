#!/usr/bin/env bash
#
# Render the prompt-injection accuracy comment body. Emits Markdown to stdout
# starting with a marker comment so the workflow can find and update the
# sticky comment idempotently.
#
# Comment is intentionally compact: status line, regressions vs main (when any),
# and a one-line overall summary. Full per-source / per-rule breakdown is in
# the workflow run's `risk-accuracy-metrics` artifact.
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
fi
echo

# Compute regression bullets when a baseline is available.
OVERALL_BULLETS=""
RULE_BULLETS=""
SOURCE_BULLETS=""
REG_TOTAL=0

if [ "$HAS_MAIN" -eq 1 ]; then
  OVERALL_BULLETS=$(jq -r --slurpfile main "$MAIN_FILE" '
    def fmt: . * 10000 | round / 10000;
    def fmt_d($d):
      if $d > 0   then "+" + ($d | tostring)
      elif $d < 0 then ($d | tostring)
      else "0" end;
    .summary.overall as $o
    | $main[0].summary.overall as $m
    | ["precision","recall","f1","accuracy","fp_rate"]
    | map(. as $k
        | (($o[$k]  // 0) | fmt) as $pr
        | ((($o[$k] // 0) - ($m[$k] // 0)) | fmt) as $d
        | select($d != 0)
        | "- **\($k):** \($pr) (Δ \(fmt_d($d)))")
    | .[]
  ' "$PR_FILE")

  RULE_BULLETS=$(jq -r --slurpfile main "$MAIN_FILE" '
    def fmt_d($d):
      if $d > 0   then "+" + ($d | tostring)
      elif $d < 0 then ($d | tostring)
      else "0" end;
    ((.summary.by_rule // []) | map({(.rule_id): {tp: .tp_count, fp: .fp_count}}) | add // {}) as $pr
    | (($main[0].summary.by_rule // []) | map({(.rule_id): {tp: .tp_count, fp: .fp_count}}) | add // {}) as $mn
    | (($pr | keys) + ($mn | keys) | unique)
    | map(. as $k
        | { rule: $k,
            ptp: ($pr[$k].tp // 0), pfp: ($pr[$k].fp // 0),
            mtp: ($mn[$k].tp // 0), mfp: ($mn[$k].fp // 0) }
        | . + { dtp: (.ptp - .mtp), dfp: (.pfp - .mfp) }
        | select(.dtp != 0 or .dfp != 0)
        | "- `\(.rule)`: TP \(.ptp) (Δ \(fmt_d(.dtp))), FP \(.pfp) (Δ \(fmt_d(.dfp)))")
    | .[]
  ' "$PR_FILE")

  SOURCE_BULLETS=$(jq -r --slurpfile main "$MAIN_FILE" '
    def fmt: . * 10000 | round / 10000;
    def fmt_d($d):
      if $d > 0   then "+" + ($d | tostring)
      elif $d < 0 then ($d | tostring)
      else "0" end;
    (($main[0].summary.by_source // []) | map({(.source): .metrics}) | add // {}) as $mn
    | (.summary.by_source // [])[]
    | . as $s
    | (($s.metrics.recall  // 0) | fmt) as $pr_r
    | (($s.metrics.fp_rate // 0) | fmt) as $pr_f
    | ((($s.metrics.recall  // 0) - ($mn[$s.source].recall  // 0)) | fmt) as $dr
    | ((($s.metrics.fp_rate // 0) - ($mn[$s.source].fp_rate // 0)) | fmt) as $df
    | select($dr != 0 or $df != 0)
    | "- **\($s.source):** recall \($pr_r) (Δ \(fmt_d($dr))), fp_rate \($pr_f) (Δ \(fmt_d($df)))"
  ' "$PR_FILE")

  OVERALL_N=$(printf '%s\n' "$OVERALL_BULLETS" | grep -c '^- ' || true)
  RULE_N=$(printf '%s\n' "$RULE_BULLETS"   | grep -c '^- ' || true)
  SOURCE_N=$(printf '%s\n' "$SOURCE_BULLETS" | grep -c '^- ' || true)
  REG_TOTAL=$((OVERALL_N + RULE_N + SOURCE_N))
fi

if [ "$HAS_MAIN" -eq 0 ]; then
  echo "_No baseline yet — main has no recorded run for this artifact. Tracking starts on the next merge._"
elif [ "$REG_TOTAL" -eq 0 ]; then
  echo "✅ No regressions vs main."
else
  if [ "$REG_TOTAL" -eq 1 ]; then
    echo "⚠️ 1 regression vs main."
  else
    echo "⚠️ $REG_TOTAL regressions vs main."
  fi
fi
echo

if [ "$HAS_MAIN" -eq 1 ] && [ "$REG_TOTAL" -gt 0 ]; then
  if [ "$OVERALL_N" -gt 0 ]; then
    echo '**Overall**'
    echo
    printf '%s\n' "$OVERALL_BULLETS"
    echo
  fi
  if [ "$RULE_N" -gt 0 ]; then
    echo '**By rule**'
    echo
    printf '%s\n' "$RULE_BULLETS"
    echo
  fi
  if [ "$SOURCE_N" -gt 0 ]; then
    echo '**By source**'
    echo
    printf '%s\n' "$SOURCE_BULLETS"
    echo
  fi
fi

# Always show a one-line current snapshot of overall metrics.
HEADLINE=$(jq -r '
  def fmt: . * 10000 | round / 10000;
  .summary.overall
  | "**Overall:** precision `\(.precision|fmt)` · recall `\(.recall|fmt)` · f1 `\(.f1|fmt)` · accuracy `\(.accuracy|fmt)` · fp_rate `\(.fp_rate|fmt)`"
' "$PR_FILE")
echo "$HEADLINE"
echo

echo '<sub>Generated by <code>.github/scripts/risk-metrics-comment.sh</code>. Hard floor on FP-rate: <code>server/internal/background/activities/risk_analysis/testdata/prompt_injection/floors.json</code>. Full per-source / per-rule breakdown: <code>risk-accuracy-metrics</code> artifact on the workflow run.</sub>'
