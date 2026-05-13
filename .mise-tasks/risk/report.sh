#!/usr/bin/env bash
#MISE description="Run the prompt-injection accuracy suite and print a tidy summary of the resulting metrics"

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
METRICS_FILE="$REPO_ROOT/server/risk_accuracy_metrics.json"

test_exit=0
mise run test:server -run TestDetectPromptInjection_AccuracyBaseline ./internal/background/activities/risk_analysis/... || test_exit=$?

if [ ! -f "$METRICS_FILE" ]; then
  echo "metrics file not found at $METRICS_FILE; the test may have crashed before writing the artifact" >&2
  exit "$test_exit"
fi

echo
echo "═══ Prompt-Injection Accuracy Summary ═══"

jq -r '
  def fmt: . * 10000 | round / 10000;
  "ref:       \(.ref)",
  "git_sha:   \(.git_sha)",
  "timestamp: \(.timestamp)",
  "",
  "total:     \(.summary.total)  (TP=\(.summary.counts.tp) FP=\(.summary.counts.fp) TN=\(.summary.counts.tn) FN=\(.summary.counts.fn))",
  "precision: \(.summary.overall.precision|fmt)",
  "recall:    \(.summary.overall.recall|fmt)",
  "f1:        \(.summary.overall.f1|fmt)",
  "accuracy:  \(.summary.overall.accuracy|fmt)",
  "fp_rate:   \(.summary.overall.fp_rate|fmt)",
  "",
  "By source:",
  ""
' "$METRICS_FILE"

(
  printf 'source\ttp\tfp\ttn\tfn\trecall\tfp_rate\n'
  jq -r '
    def fmt: . * 10000 | round / 10000;
    .summary.by_source[]
    | [.source, .counts.tp, .counts.fp, .counts.tn, .counts.fn, (.metrics.recall|fmt), (.metrics.fp_rate|fmt)]
    | @tsv
  ' "$METRICS_FILE"
) | column -t -s "$(printf '\t')"

echo
echo "By rule:"
echo
(
  printf 'rule_id\ttp\tfp\n'
  jq -r '
    .summary.by_rule[]
    | [.rule_id, .tp_count, .fp_count]
    | @tsv
  ' "$METRICS_FILE"
) | column -t -s "$(printf '\t')"

echo
exit "$test_exit"
