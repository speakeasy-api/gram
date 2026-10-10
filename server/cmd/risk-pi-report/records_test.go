package main

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/scanners"
	"github.com/speakeasy-api/gram/server/internal/scanners/promptinjection"
	piopenrouter "github.com/speakeasy-api/gram/server/internal/scanners/promptinjection/openrouter"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	typesafe "github.com/speakeasy-api/gram/server/internal/thirdparty/typesafedecisions"
)

func recordsCase(id, label, wellKnown string) labeledCase {
	return labeledCase{ID: id, Label: label, Text: "text " + id, Source: "s", WellKnown: wellKnown}
}

// main's risk-pi-report hashes cases the same way, so a run of main's
// detector and a run of this change key the same case alike.
func TestCaseHashUsesTheFixtureLine(t *testing.T) {
	t.Parallel()

	corpus := loadReportCorpus(t)
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", defaultCorpusDir, "gram_benigns.jsonl"))
	require.NoError(t, err)
	line := strings.TrimSpace(strings.SplitN(string(raw), "\n", 2)[0])
	var first labeledCase
	require.NoError(t, json.Unmarshal([]byte(line), &first))
	for _, c := range corpus {
		if caseKey(c) == caseKey(first) {
			sum := sha256.Sum256([]byte(line))
			require.Equal(t, fmt.Sprintf("%x", sum)[:caseHashHexLen], caseHash(c))
			return
		}
	}
	t.Fatalf("%s is not in the corpus", caseKey(first))
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
	require.Equal(t, []labeledCase{edited, unfunded, fresh}, casesToRun([]labeledCase{done, edited, unfunded, fresh}, records))
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

func TestOpenRecordsForAppendEndsAPartialLine(t *testing.T) {
	t.Parallel()

	done := `{"key":"s::a","hash":"h1","status":"clear","cost_usd":0,"latency_ms":0}` + "\n"
	partial := `{"key":"s::b","hash":"h2","sta`
	appended := caseRecord{Key: "s::c", Hash: "h3", Status: statusFlagged}
	line, err := json.Marshal(appended)
	require.NoError(t, err)
	tests := []struct {
		name     string
		existing string
		want     string
		wantKeys []string
	}{
		{name: "new file", existing: "", want: string(line) + "\n", wantKeys: []string{"s::c"}},
		{name: "complete last line", existing: done, want: done + string(line) + "\n", wantKeys: []string{"s::a", "s::c"}},
		{name: "partial last line", existing: done + partial, want: done + partial + "\n" + string(line) + "\n", wantKeys: []string{"s::a", "s::c"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), casesFile)
			if tc.existing != "" {
				require.NoError(t, os.WriteFile(path, []byte(tc.existing), 0o600))
			}

			file, err := openRecordsForAppend(path)
			require.NoError(t, err)
			require.NoError(t, appendRecord(file, appended))
			require.NoError(t, file.Close())

			raw, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, tc.want, string(raw))
			records, err := loadRecords(path)
			require.NoError(t, err)
			require.ElementsMatch(t, tc.wantKeys, slices.Collect(maps.Keys(records)))
		})
	}
}

// writeManifestFile writes m as dir's manifest and returns the bytes written.
func writeManifestFile(t *testing.T, dir string, m runManifest) []byte {
	t.Helper()
	raw, err := json.Marshal(m)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, manifestFile), raw, 0o600))
	return raw
}

func detectorForTest(t *testing.T) runDetector {
	t.Helper()
	detector, err := currentDetector()
	require.NoError(t, err)
	return detector
}

var (
	mainCommit    = runCommit{SHA: "1a2b3c4d5e6f", Subject: "chore: main", Uncommitted: false}
	fixtureCommit = runCommit{SHA: "6f5e4d3c2b1a", Subject: "chore: edit a fixture", Uncommitted: false}
)

func TestCurrentDetectorIsTheCascade(t *testing.T) {
	t.Parallel()

	detector := detectorForTest(t)
	require.Equal(t, typesafe.Model, detector.PrefilterModel)
	require.InDelta(t, piopenrouter.PrefilterThreshold, detector.PrefilterThreshold, 1e-9)
	require.Equal(t, piopenrouter.ConfirmationModel, detector.ConfirmationModel)
	require.Len(t, detector.ConfirmationPromptSHA256, 64)
	require.Len(t, detector.PrefilterQuestionsSHA256, 64)
}

func TestClaimRunDirListsEachCommitOnce(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, claimRunDir(dir, mainCommit, time.Now().Add(-time.Hour)))
	now := time.Now()
	require.NoError(t, claimRunDir(dir, fixtureCommit, now))
	require.NoError(t, claimRunDir(dir, mainCommit, now))

	got, err := readManifest(filepath.Join(dir, manifestFile))
	require.NoError(t, err)
	require.Equal(t, detectorForTest(t), got.Detector)
	require.Equal(t, []runCommit{mainCommit, fixtureCommit}, got.Commits)
	require.WithinDuration(t, now, got.Updated, time.Second)
}

func TestClaimRunDirRefusesAnotherDetector(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		change func(*runDetector)
	}{
		{name: "confirmation model", change: func(d *runDetector) { d.ConfirmationModel = "other/model" }},
		{name: "confirmation prompt", change: func(d *runDetector) { d.ConfirmationPromptSHA256 = "0123456789ab" }},
		{name: "prefilter model", change: func(d *runDetector) { d.PrefilterModel = "other/prefilter" }},
		{name: "prefilter threshold", change: func(d *runDetector) { d.PrefilterThreshold = 0.9 }},
		{name: "prefilter questions", change: func(d *runDetector) { d.PrefilterQuestionsSHA256 = "0123456789ab" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			other := detectorForTest(t)
			tc.change(&other)
			before := writeManifestFile(t, dir, runManifest{Detector: other, Commits: []runCommit{mainCommit}, Updated: time.Now()})

			require.ErrorContains(t, claimRunDir(dir, mainCommit, time.Now()), "holds another detector's results")
			after, err := os.ReadFile(filepath.Join(dir, manifestFile))
			require.NoError(t, err)
			require.Equal(t, before, after, "a refused run dir keeps its manifest")
		})
	}
}

func TestRunRecordsRefusesAnotherDetectorsRecords(t *testing.T) {
	t.Parallel()

	attack := recordsCase("attack", "malicious", "")
	dir := t.TempDir()
	file, err := openRecordsForAppend(filepath.Join(dir, casesFile))
	require.NoError(t, err)
	require.NoError(t, appendRecord(file, caseRecord{Key: caseKey(attack), Hash: caseHash(attack), Status: statusFlagged}))
	require.NoError(t, file.Close())
	other := detectorForTest(t)
	other.ConfirmationModel = "other/model"
	writeManifestFile(t, dir, runManifest{Detector: other, Commits: nil, Updated: time.Now()})

	err = runRecords(t.Context(), options{runDir: dir, commit: mainCommit}, []labeledCase{attack})
	require.ErrorContains(t, err, "run this tool from the checkout it was built from")
}

func TestRunRecordsRefusesDuplicateCaseKeys(t *testing.T) {
	t.Parallel()

	first, repeat := recordsCase("a", "benign", ""), recordsCase("a", "malicious", "")
	err := runRecords(t.Context(), options{runDir: t.TempDir(), commit: mainCommit}, []labeledCase{first, repeat})
	require.ErrorContains(t, err, `two cases share the key "s::a"`)
}

func TestLockRunDirMakesASecondRunWait(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	unlock, err := lockRunDir(dir)
	require.NoError(t, err)

	type lockResult struct {
		unlock func()
		err    error
	}
	locked := make(chan lockResult, 1)
	go func() {
		second, err := lockRunDir(dir)
		locked <- lockResult{unlock: second, err: err}
	}()
	select {
	case got := <-locked:
		t.Fatalf("a second run got past the lock while the first held it (err: %v)", got.err)
	case <-time.After(200 * time.Millisecond):
	}
	unlock()
	select {
	case got := <-locked:
		require.NoError(t, got.err)
		got.unlock()
	case <-time.After(5 * time.Second):
		t.Fatal("the second run never locked the run dir after the first released it")
	}
}

func TestRunRecordsReusesAFinishedRunWithoutCalls(t *testing.T) {
	t.Setenv("OPENROUTER_DEV_KEY", "")
	t.Setenv("OPENROUTER_API_KEY", "")

	benign, attack := recordsCase("benign", "benign", ""), recordsCase("attack", "malicious", "")
	dir := t.TempDir()
	lines := make([]string, 0, 2)
	for _, rec := range []caseRecord{
		{Key: caseKey(benign), Hash: caseHash(benign), Status: statusClear, LatencyMS: 10},
		{Key: caseKey(attack), Hash: caseHash(attack), Status: statusFlagged, LatencyMS: 10},
	} {
		raw, err := json.Marshal(rec)
		require.NoError(t, err)
		lines = append(lines, string(raw))
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, casesFile), []byte(strings.Join(lines, "\n")), 0o600))

	require.NoError(t, runRecords(t.Context(), options{runDir: dir, commit: mainCommit}, []labeledCase{benign, attack}))
	manifest, err := readManifest(filepath.Join(dir, manifestFile))
	require.NoError(t, err)
	require.Equal(t, []runCommit{mainCommit}, manifest.Commits)
	require.Equal(t, detectorForTest(t), manifest.Detector)
	require.WithinDuration(t, time.Now(), manifest.Updated, time.Minute)
}

func TestRunRecordsNeedsAKeyForNewCases(t *testing.T) {
	t.Setenv("OPENROUTER_DEV_KEY", "")
	t.Setenv("OPENROUTER_API_KEY", "")

	err := runRecords(t.Context(), options{runDir: t.TempDir(), commit: mainCommit}, []labeledCase{recordsCase("new", "benign", "")})
	require.ErrorContains(t, err, "set OPENROUTER_DEV_KEY")
}
