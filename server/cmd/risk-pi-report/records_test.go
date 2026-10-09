package main

import (
	"context"
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
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	piopenrouter "github.com/speakeasy-api/gram/server/internal/scanners/promptinjection/openrouter"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

func recordsCase(id, label string) labeledCase {
	return labeledCase{ID: id, Label: label, Text: "text " + id, Source: "s"}
}

func TestCaseHashUsesTheFixtureLine(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	line := `{"id": "gram_benigns.api.001", "label": "benign", "text": "List my deployments", "source": "gram_benigns", "field_this_version_ignores": true}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gram_benigns.jsonl"), []byte(line+"\n"), 0o600))
	corpus, err := loadCorpus(dir)
	require.NoError(t, err)
	var loaded labeledCase
	for _, c := range corpus {
		if c.ID == "gram_benigns.api.001" {
			loaded = c
		}
	}
	sum := sha256.Sum256([]byte(line))
	require.Equal(t, fmt.Sprintf("%x", sum)[:caseHashHexLen], caseHash(loaded), "a field this version ignores still counts, so every version hashes the line alike")
}

func TestRecordFromOutcomeStatuses(t *testing.T) {
	t.Parallel()

	attack := piopenrouter.Verdict{DirectiveKind: piopenrouter.DirectiveInstructionOverride, Target: piopenrouter.TargetGuardedAgent, Operational: true, Rationale: "overrides the system prompt"}
	benign := piopenrouter.Verdict{DirectiveKind: piopenrouter.DirectiveNone, Target: piopenrouter.TargetNone, Operational: false, Rationale: "asks to list deployments"}
	unfunded := &openrouter.HTTPError{StatusCode: http.StatusPaymentRequired, Err: openrouter.ErrInsufficientCredits}
	tests := []struct {
		name       string
		outcome    caseOutcome
		want       caseStatus
		wantDetail string
	}{
		{name: "flagged", outcome: caseOutcome{verdict: attack, costUSD: 0.002, latency: 900 * time.Millisecond}, want: statusFlagged, wantDetail: "overrides the system prompt"},
		{name: "clear", outcome: caseOutcome{verdict: benign, costUSD: 0.002, latency: 900 * time.Millisecond}, want: statusClear, wantDetail: "asks to list deployments"},
		{name: "no verdict", outcome: caseOutcome{verdict: emptyTypedVerdict, costUSD: 0.002, latency: 900 * time.Millisecond, err: errors.New("parse judge response")}, want: statusNoVerdict, wantDetail: "parse judge response"},
		{name: "out of credit", outcome: caseOutcome{verdict: emptyTypedVerdict, costUSD: 0.002, latency: 900 * time.Millisecond, err: unfunded}, want: statusOutOfCredit, wantDetail: "OpenRouter returned 402: out of credit"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec := recordFromOutcome(recordsCase("a", "malicious"), tc.outcome)
			require.Equal(t, tc.want, rec.Status)
			require.Equal(t, tc.wantDetail, rec.Detail)
			require.Equal(t, "s::a", rec.Key)
			require.Equal(t, piopenrouter.Model, rec.Model)
			require.InDelta(t, 0.002, rec.CostUSD, 1e-9)
			require.InDelta(t, 900.0, rec.LatencyMS, 1e-9)
		})
	}
}

func TestCasesToRunSkipsCurrentRecords(t *testing.T) {
	t.Parallel()

	done, edited, unfunded, fresh := recordsCase("done", "benign"), recordsCase("edited", "benign"), recordsCase("unfunded", "malicious"), recordsCase("fresh", "malicious")
	records := map[string]caseRecord{
		caseKey(done):     {Key: caseKey(done), Hash: caseHash(done), Status: statusClear},
		caseKey(edited):   {Key: caseKey(edited), Hash: "stale", Status: statusClear},
		caseKey(unfunded): {Key: caseKey(unfunded), Hash: caseHash(unfunded), Status: statusOutOfCredit},
	}
	require.Equal(t, []labeledCase{edited, unfunded, fresh}, casesToRun([]labeledCase{done, edited, unfunded, fresh}, records))
}

func TestTallyRunCountsEveryFinishedCase(t *testing.T) {
	t.Parallel()

	caught, falsePositive, missed, failed, unfunded, fresh := recordsCase("caught", "malicious"), recordsCase("fp", "benign"), recordsCase("missed", "malicious"), recordsCase("failed", "malicious"), recordsCase("unfunded", "benign"), recordsCase("fresh", "benign")
	records := map[string]caseRecord{
		caseKey(caught):        {Key: caseKey(caught), Hash: caseHash(caught), Status: statusFlagged, CostUSD: 0.5},
		caseKey(falsePositive): {Key: caseKey(falsePositive), Hash: caseHash(falsePositive), Status: statusFlagged, CostUSD: 0.25},
		caseKey(missed):        {Key: caseKey(missed), Hash: caseHash(missed), Status: statusClear},
		caseKey(failed):        {Key: caseKey(failed), Hash: caseHash(failed), Status: statusNoVerdict},
		caseKey(unfunded):      {Key: caseKey(unfunded), Hash: caseHash(unfunded), Status: statusOutOfCredit},
	}
	got := tallyRun([]labeledCase{caught, falsePositive, missed, failed, unfunded, fresh}, records)
	require.Equal(t, runTally{cases: 6, done: 4, falsePositives: 1, attacks: 3, caught: 1, noVerdict: 1, outOfCredit: 1, costUSD: 0.75}, got)
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

var (
	mainCommit    = runCommit{SHA: "1a2b3c4d5e6f", Subject: "chore: main", Uncommitted: false}
	fixtureCommit = runCommit{SHA: "6f5e4d3c2b1a", Subject: "chore: edit a fixture", Uncommitted: false}
)

func TestClaimRunDirListsEachCommitOnce(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, claimRunDir(dir, mainCommit, time.Now().Add(-time.Hour)))
	now := time.Now()
	require.NoError(t, claimRunDir(dir, fixtureCommit, now))
	require.NoError(t, claimRunDir(dir, mainCommit, now))

	got, err := readManifest(filepath.Join(dir, manifestFile))
	require.NoError(t, err)
	require.Equal(t, currentDetector(), got.Detector)
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
		{name: "prefilter threshold", change: func(d *runDetector) { d.PrefilterThreshold = 0.5 }},
		{name: "prefilter questions", change: func(d *runDetector) { d.PrefilterQuestionsSHA256 = "0123456789ab" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			other := currentDetector()
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

	attack := recordsCase("attack", "malicious")
	dir := t.TempDir()
	file, err := openRecordsForAppend(filepath.Join(dir, casesFile))
	require.NoError(t, err)
	require.NoError(t, appendRecord(file, caseRecord{Key: caseKey(attack), Hash: caseHash(attack), Status: statusFlagged}))
	require.NoError(t, file.Close())
	other := currentDetector()
	other.ConfirmationModel = "other/model"
	writeManifestFile(t, dir, runManifest{Detector: other, Commits: nil, Updated: time.Now()})

	err = runRecords(t.Context(), options{runDir: dir, commit: mainCommit}, []labeledCase{attack})
	require.ErrorContains(t, err, "run this tool from the checkout it was built from")
}

func TestRunRecordsRefusesDuplicateCaseKeys(t *testing.T) {
	t.Parallel()

	first, repeat := recordsCase("a", "benign"), recordsCase("a", "malicious")
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

// blockingClient holds each completion until release closes, counting calls.
type blockingClient struct {
	openrouter.CompletionClient
	calls   atomic.Int32
	started chan struct{}
	release chan struct{}
}

func (c *blockingClient) GetCompletion(context.Context, openrouter.CompletionRequest) (*openrouter.CompletionResponse, error) {
	c.calls.Add(1)
	c.started <- struct{}{}
	<-c.release
	return nil, errors.New("blocking client released")
}

func TestScanJudgeStartsNoCaseAfterACancelWhileWaitingForASlot(t *testing.T) {
	t.Parallel()

	corpus := make([]labeledCase, judgeConcurrency+1)
	for i := range corpus {
		corpus[i] = recordsCase(fmt.Sprint(i), "malicious")
	}
	client := &blockingClient{started: make(chan struct{}, len(corpus)), release: make(chan struct{})}
	ctx, cancel := context.WithCancel(t.Context())
	finished := make(chan int32, 1)
	go func() {
		var cases atomic.Int32
		scanJudge(ctx, client, corpus, func(int, caseOutcome) { cases.Add(1) })
		finished <- cases.Load()
	}()

	// The first cases hold every slot, so the loop waits for one when the run
	// is cancelled.
	for range judgeConcurrency {
		<-client.started
	}
	cancel()
	close(client.release)
	require.Equal(t, int32(judgeConcurrency), <-finished)
	require.Equal(t, int32(judgeConcurrency), client.calls.Load())
}

func TestRunRecordsReusesAFinishedRunWithoutCalls(t *testing.T) {
	t.Setenv("OPENROUTER_DEV_KEY", "")
	t.Setenv("OPENROUTER_API_KEY", "")

	benign, attack := recordsCase("benign", "benign"), recordsCase("attack", "malicious")
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
	require.Equal(t, piopenrouter.Model, manifest.Detector.ConfirmationModel)
	require.Empty(t, manifest.Detector.PrefilterModel)
	require.WithinDuration(t, time.Now(), manifest.Updated, time.Minute)
}

func TestRunRecordsNeedsAKeyForNewCases(t *testing.T) {
	t.Setenv("OPENROUTER_DEV_KEY", "")
	t.Setenv("OPENROUTER_API_KEY", "")

	err := runRecords(t.Context(), options{runDir: t.TempDir(), commit: mainCommit}, []labeledCase{recordsCase("new", "benign")})
	require.ErrorContains(t, err, "set OPENROUTER_DEV_KEY")
}

func TestFirstEnvSkipsTheMisePlaceholder(t *testing.T) {
	t.Setenv("OPENROUTER_DEV_KEY", unsetEnvPlaceholder)
	t.Setenv("OPENROUTER_API_KEY", "sk-test")
	require.Equal(t, "sk-test", firstEnv("OPENROUTER_DEV_KEY", "OPENROUTER_API_KEY"))
}
