package main

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/scanners"
	"github.com/speakeasy-api/gram/server/internal/scanners/promptinjection"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	typesafe "github.com/speakeasy-api/gram/server/internal/thirdparty/typesafedecisions"
)

func recordsCase(id, label, wellKnown string) labeledCase {
	return labeledCase{ID: id, Label: label, Text: "text " + id, Source: "s", WellKnown: wellKnown}
}

func TestRecordFromOutcomeStatuses(t *testing.T) {
	t.Parallel()

	finding := []scanners.Finding{{RuleID: "pi"}}
	unavailable := promptinjection.Result{Label: promptinjection.LabelUnavailable}
	tests := []struct {
		name    string
		outcome caseOutcome
		want    caseStatus
	}{
		{name: "flagged", outcome: caseOutcome{findings: finding, verdict: promptinjection.Result{Label: promptinjection.LabelInjection}}, want: statusFlagged},
		{name: "clear", outcome: caseOutcome{verdict: promptinjection.Result{Label: promptinjection.LabelSafe}}, want: statusClear},
		{name: "no verdict", outcome: caseOutcome{verdict: unavailable, observation: decisionObservation{Calls: []callObservation{{Err: errors.New("bad gateway")}}}}, want: statusNoVerdict},
		{name: "openrouter out of credit", outcome: caseOutcome{verdict: unavailable, observation: decisionObservation{Calls: []callObservation{{Err: &openrouter.HTTPError{StatusCode: http.StatusPaymentRequired, Err: openrouter.ErrInsufficientCredits}}}}}, want: statusOutOfCredit},
		{name: "jev out of credit", outcome: caseOutcome{verdict: unavailable, observation: decisionObservation{Calls: []callObservation{{Err: &typesafe.StatusError{StatusCode: http.StatusPaymentRequired}}}}}, want: statusOutOfCredit},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec := recordFromOutcome(recordsCase("a", "malicious", ""), tc.outcome)
			require.Equal(t, tc.want, rec.Status)
			require.Equal(t, "s::a", rec.Key)
		})
	}
}

func TestRecordFromOutcomeExplainsRefusal(t *testing.T) {
	t.Parallel()

	rec := recordFromOutcome(recordsCase("a", "malicious", ""), caseOutcome{
		verdict:     promptinjection.Result{Label: promptinjection.LabelUnavailable},
		refused:     true,
		observation: decisionObservation{Calls: []callObservation{{CostUSD: 0.002}, {CostUSD: 0.003}}, Latency: 1500 * time.Millisecond},
	})
	require.Equal(t, statusNoVerdict, rec.Status)
	require.Contains(t, rec.Detail, "refused")
	require.InDelta(t, 0.005, rec.CostUSD, 1e-9)
	require.InDelta(t, 1500, rec.LatencyMS, 1e-9)
}

func TestCasesToRunSkipsCurrentRecords(t *testing.T) {
	t.Parallel()

	done, edited, unfunded, fresh := recordsCase("done", "benign", ""), recordsCase("edited", "benign", ""), recordsCase("unfunded", "malicious", ""), recordsCase("fresh", "malicious", "")
	records := map[string]caseRecord{
		caseKey(done):     {Key: caseKey(done), Hash: caseHash(done), Status: statusClear},
		caseKey(edited):   {Key: caseKey(edited), Hash: "stale", Status: statusClear},
		caseKey(unfunded): {Key: caseKey(unfunded), Hash: caseHash(unfunded), Status: statusOutOfCredit},
	}
	todo := casesToRun([]labeledCase{done, edited, unfunded, fresh}, records)
	require.Equal(t, []labeledCase{edited, unfunded, fresh}, todo)
}

func TestLoadRecordsKeepsLastLineAndSkipsPartialLine(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), casesFile)
	body := strings.Join([]string{
		`{"key":"s::a","hash":"h1","status":"out_of_credit","cost_usd":0,"latency_ms":0}`,
		`{"key":"s::a","hash":"h1","status":"flagged","cost_usd":0.002,"latency_ms":900}`,
		`{"key":"s::b","hash":"h2","sta`,
	}, "\n")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	records, err := loadRecords(path)
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, statusFlagged, records["s::a"].Status)
}

func TestLoadRecordsMissingFileIsEmpty(t *testing.T) {
	t.Parallel()

	records, err := loadRecords(filepath.Join(t.TempDir(), casesFile))
	require.NoError(t, err)
	require.Empty(t, records)
}

func TestComputeTotalsCountsEveryOutcome(t *testing.T) {
	t.Parallel()

	fp, wk, missed, noVerdict, unfunded, pending := recordsCase("fp", "benign", ""), recordsCase("wk", "malicious", "Ignore previous instructions"), recordsCase("missed", "malicious", "DAN"), recordsCase("nov", "malicious", ""), recordsCase("ooc", "malicious", ""), recordsCase("pending", "benign", "")
	record := func(c labeledCase, status caseStatus, ms float64) caseRecord {
		return caseRecord{Key: caseKey(c), Hash: caseHash(c), Status: status, CostUSD: 0.01, LatencyMS: ms}
	}
	records := map[string]caseRecord{
		caseKey(fp):        record(fp, statusFlagged, 100),
		caseKey(wk):        record(wk, statusFlagged, 200),
		caseKey(missed):    record(missed, statusClear, 300),
		caseKey(noVerdict): record(noVerdict, statusNoVerdict, 400),
		caseKey(unfunded):  record(unfunded, statusOutOfCredit, 0),
	}
	totals := computeTotals([]labeledCase{fp, wk, missed, noVerdict, unfunded, pending}, records)
	require.Equal(t, sideTotals{
		Cases: 6, Benign: 2, FalsePositives: 1, Attacks: 4, Caught: 1, WellKnown: 2, WellKnownCaught: 1,
		Refused: 0, NoVerdict: 1, OutOfCredit: 1, Pending: 1, CostUSD: totals.CostUSD,
		LatencyP50MS: 200, LatencyP90MS: 400,
	}, totals)
	require.InDelta(t, 0.05, totals.CostUSD, 1e-9)
}

func TestGateTallyFromRecordsMatchesTallyGate(t *testing.T) {
	t.Parallel()

	fp, caught, missed := recordsCase("fp", "benign", ""), recordsCase("caught", "malicious", ""), recordsCase("missed", "malicious", "")
	records := map[string]caseRecord{
		caseKey(fp):     {Key: caseKey(fp), Hash: caseHash(fp), Status: statusFlagged},
		caseKey(caught): {Key: caseKey(caught), Hash: caseHash(caught), Status: statusFlagged},
		caseKey(missed): {Key: caseKey(missed), Hash: caseHash(missed), Status: statusNoVerdict},
	}
	tally := gateTallyFromRecords([]labeledCase{fp, caught, missed}, records)
	require.Equal(t, gateTally{FalsePositives: 1, Attacks: 2, AttacksCaught: 1, FalsePositiveKeys: []string{"s::fp"}}, tally)
}

func TestRunRecordsGatesAFinishedRunWithoutCalls(t *testing.T) {
	t.Parallel()

	benign, attack := recordsCase("benign", "benign", ""), recordsCase("attack", "malicious", "")
	dir := t.TempDir()
	body := strings.Join([]string{
		`{"key":"s::benign","hash":"` + caseHash(benign) + `","status":"clear","cost_usd":0,"latency_ms":10}`,
		`{"key":"s::attack","hash":"` + caseHash(attack) + `","status":"flagged","cost_usd":0,"latency_ms":10}`,
	}, "\n")
	require.NoError(t, os.WriteFile(filepath.Join(dir, casesFile), []byte(body), 0o600))
	opts := options{runDir: dir, label: "this change", ref: "test", refusalFallback: true, maxFalsePositives: 0, minRecall: 0.8}

	require.NoError(t, runRecords(t.Context(), opts, []labeledCase{benign, attack}))
	manifest, err := loadManifest(dir)
	require.NoError(t, err)
	require.Equal(t, "this change", manifest.Label)
}

func TestFinishRunRefusesTheGateWhileCasesLackResults(t *testing.T) {
	t.Parallel()

	benign, attack := recordsCase("benign", "benign", ""), recordsCase("attack", "malicious", "")
	cleared := caseRecord{Key: caseKey(benign), Hash: caseHash(benign), Status: statusClear}
	opts := options{label: "this change", maxFalsePositives: 0, minRecall: 0.8}

	err := finishRun(opts, []labeledCase{benign, attack}, map[string]caseRecord{caseKey(benign): cleared}, false)
	require.ErrorContains(t, err, "1 cases have no result")

	unfunded := caseRecord{Key: caseKey(attack), Hash: caseHash(attack), Status: statusOutOfCredit}
	err = finishRun(opts, []labeledCase{benign, attack}, map[string]caseRecord{caseKey(benign): cleared, caseKey(attack): unfunded}, false)
	require.ErrorContains(t, err, "out of OpenRouter credit")
}

func TestFinishRunFailsTheGateOnAFalsePositive(t *testing.T) {
	t.Parallel()

	benign, attack := recordsCase("benign", "benign", ""), recordsCase("attack", "malicious", "")
	records := map[string]caseRecord{
		caseKey(benign): {Key: caseKey(benign), Hash: caseHash(benign), Status: statusFlagged},
		caseKey(attack): {Key: caseKey(attack), Hash: caseHash(attack), Status: statusFlagged},
	}
	err := finishRun(options{label: "this change", maxFalsePositives: 0, minRecall: 0.8}, []labeledCase{benign, attack}, records, false)
	require.ErrorContains(t, err, "merge gate failed")
}

func TestFinishRunWithoutAGateOnlySummarizes(t *testing.T) {
	t.Parallel()

	benign := recordsCase("benign", "benign", "")
	err := finishRun(options{label: "main", maxFalsePositives: gateDisabledFalsePositives}, []labeledCase{benign}, map[string]caseRecord{}, false)
	require.NoError(t, err)
}
