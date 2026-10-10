package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// testDetector is a cascade manifest's detector for viewer tests.
var testDetector = runDetector{
	PrefilterModel:           "typesafe/jev-1.13",
	PrefilterThreshold:       0.5,
	ConfirmationModel:        "anthropic/claude-sonnet-5.5",
	ConfirmationPromptSHA256: "1f4c2ffbb7cd153e",
	PrefilterQuestionsSHA256: "0219e560da66ed88",
}

// writeRun writes a run named id under runs, measured by commit, updated at
// updated, holding records.
func writeRun(t *testing.T, runs, id string, commit runCommit, updated time.Time, records ...caseRecord) {
	t.Helper()
	dir := filepath.Join(runs, id)
	require.NoError(t, os.MkdirAll(dir, 0o750))
	lines := make([]string, 0, len(records))
	for _, rec := range records {
		raw, err := json.Marshal(rec)
		require.NoError(t, err)
		lines = append(lines, string(raw))
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, casesFile), []byte(strings.Join(lines, "\n")), 0o600))
	manifest, err := json.Marshal(runManifest{Detector: testDetector, Commits: []runCommit{commit}, Updated: updated})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, manifestFile), manifest, 0o600))
}

func viewRecord(c labeledCase, status caseStatus) caseRecord {
	return caseRecord{Key: caseKey(c), Hash: caseHash(c), Status: status, CostUSD: 0.001, LatencyMS: 1000}
}

var (
	baseCommit   = runCommit{SHA: "aaaaaaaaaaaa1111", Subject: "chore: main", Uncommitted: false}
	changeCommit = runCommit{SHA: "cccccccccccc2222", Subject: "fix: better detector", Uncommitted: false}
	olderCommit  = runCommit{SHA: "eeeeeeeeeeee3333", Subject: "chore: an older try", Uncommitted: false}
)

func TestBuildViewDataListsEveryRunAndJoinsCases(t *testing.T) {
	t.Parallel()

	caught, fixed, pending := recordsCase("caught", "malicious", "DAN"), recordsCase("fixed", "benign", ""), recordsCase("pending", "malicious", "")
	runs := t.TempDir()
	now := time.Now()
	writeRun(t, runs, "base-key", baseCommit, now.Add(-2*time.Hour), viewRecord(caught, statusClear), viewRecord(fixed, statusFlagged), viewRecord(pending, statusFlagged))
	writeRun(t, runs, "change-key", changeCommit, now.Add(-3*time.Hour), viewRecord(caught, statusFlagged), viewRecord(fixed, statusClear))
	writeRun(t, runs, "older-key", olderCommit, now, viewRecord(caught, statusFlagged))
	require.NoError(t, os.MkdirAll(filepath.Join(runs, "no-manifest"), 0o750))

	src := viewSource{runsDir: runs, corpus: []labeledCase{caught, fixed, pending}, change: "change-key", base: baseCommit.SHA[:8]}
	data, err := buildViewData(src, time.Unix(0, 0))
	require.NoError(t, err)
	require.Equal(t, "change-key", data.Change)
	require.Equal(t, "base-key", data.Base, "the base run is found by an abbreviated commit")
	ids := make([]string, 0, len(data.Runs))
	for _, r := range data.Runs {
		ids = append(ids, r.ID)
	}
	require.Equal(t, []string{"change-key", "base-key", "older-key"}, ids, "the change and base lead; a run without a manifest is left out")
	require.Equal(t, 2, data.Runs[1].RequiredCaught)
	require.Nil(t, data.Cases[2].Results["change-key"])
	require.Equal(t, statusFlagged, data.Cases[2].Results["base-key"].Status)
	require.Equal(t, caseFlips{NewlyCaught: 1, NewlyMissed: 0, NewFalsePositives: 0, FixedFalsePositives: 1}, compareRuns(data.Cases, data.Base, data.Change))
	require.Equal(t, "Incomplete", data.Runs[0].GateStatus)
}

func TestBuildViewDataWithoutRuns(t *testing.T) {
	t.Parallel()

	src := viewSource{runsDir: filepath.Join(t.TempDir(), "missing"), corpus: []labeledCase{recordsCase("a", "benign", "")}, change: "change-key", base: ""}
	data, err := buildViewData(src, time.Unix(0, 0))
	require.NoError(t, err)
	require.Empty(t, data.Runs)
	require.Empty(t, data.Base)
	require.ErrorContains(t, changeGate(data), "this checkout has no run yet")
}

func TestPickRunsKeepsTheNewestUpToTheLimit(t *testing.T) {
	t.Parallel()

	now := time.Now()
	var runs []loadedRun
	for i := range maxViewRuns + 3 {
		runs = append(runs, loadedRun{id: string(rune('a' + i)), manifest: runManifest{Updated: now.Add(time.Duration(i) * time.Minute)}})
	}
	picked := pickRuns(runs, "b", "a")
	require.Len(t, picked, maxViewRuns)
	require.Equal(t, "b", picked[0].id)
	require.Equal(t, "a", picked[1].id)
	require.Equal(t, string(rune('a'+maxViewRuns+2)), picked[2].id, "the newest other run comes next")
}

func TestSummaryMarkdownListsTheComparedRunsAndFlips(t *testing.T) {
	t.Parallel()

	attack := recordsCase("attack", "malicious", "DAN")
	runs := t.TempDir()
	writeRun(t, runs, "base-key", baseCommit, time.Now(), viewRecord(attack, statusClear))
	writeRun(t, runs, "change-key", changeCommit, time.Now(), viewRecord(attack, statusFlagged))
	writeRun(t, runs, "older-key", olderCommit, time.Now(), viewRecord(attack, statusFlagged))
	data, err := buildViewData(viewSource{runsDir: runs, corpus: []labeledCase{attack}, change: "change-key", base: baseCommit.SHA}, time.Unix(0, 0))
	require.NoError(t, err)

	md := summaryMarkdown(data)
	require.Contains(t, md, "| main (aaaaaaaaaa chore: main) | 0 | 0 of 1 | 0 of 1 (0.0%)")
	require.Contains(t, md, "| this change (cccccccccc fix: better detector) | 0 | 1 of 1 | 1 of 1 (100.0%)")
	require.NotContains(t, md, "an older try", "the PR summary shows only the compared runs")
	require.Contains(t, md, "| Pass | Meets both |")
	require.Contains(t, md, "Compared with main: 1 newly caught · 0 newly missed")
	require.Contains(t, md, "- this change: typesafe/jev-1.13 ≥ 0.50 → anthropic/claude-sonnet-5.5 · confirmation prompt `1f4c2ffbb7` · questions `0219e560da`")
	require.NoError(t, changeGate(data))
}

func TestSummaryMarkdownWhenMainSharesTheCode(t *testing.T) {
	t.Parallel()

	attack := recordsCase("attack", "malicious", "")
	runs := t.TempDir()
	writeRun(t, runs, "shared-key", baseCommit, time.Now(), viewRecord(attack, statusFlagged))
	data, err := buildViewData(viewSource{runsDir: runs, corpus: []labeledCase{attack}, change: "shared-key", base: baseCommit.SHA}, time.Unix(0, 0))
	require.NoError(t, err)

	md := summaryMarkdown(data)
	require.Equal(t, 1, strings.Count(md, "| this change ("))
	require.Contains(t, md, "Main has the same code as this change")
	require.NotContains(t, md, "Compared with main")
}

func TestChangeGate(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		label   string
		status  caseStatus
		want    string
		wantErr string
	}{
		{name: "passes", label: "malicious", status: statusFlagged, want: "Pass"},
		{name: "misses an attack", label: "malicious", status: statusClear, want: "Fail", wantErr: "caught 0 of 1 attacks"},
		{name: "flags a benign case", label: "benign", status: statusFlagged, want: "Fail", wantErr: "1 false positives"},
		{name: "out of credit", label: "malicious", status: statusOutOfCredit, want: "Incomplete", wantErr: "1 cases have no result"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := recordsCase("case", tc.label, "")
			runs := t.TempDir()
			writeRun(t, runs, "change-key", changeCommit, time.Now(), viewRecord(c, tc.status))
			data, err := buildViewData(viewSource{runsDir: runs, corpus: []labeledCase{c}, change: "change-key", base: ""}, time.Unix(0, 0))
			require.NoError(t, err)
			require.Equal(t, tc.want, data.Runs[0].GateStatus)
			require.Contains(t, summaryMarkdown(data), "| "+tc.want+" |")
			err = changeGate(data)
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}

func TestViewerGoalsWaitForCompleteRuns(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		totals sideTotals
		want   string
	}{
		{name: "pending", totals: sideTotals{Pending: 1, WellKnown: 1, WellKnownCaught: 1}, want: "Incomplete"},
		{name: "out of credit", totals: sideTotals{OutOfCredit: 1, WellKnown: 1, WellKnownCaught: 1}, want: "Incomplete"},
		{name: "both", totals: sideTotals{WellKnown: 1, WellKnownCaught: 1}, want: "Meets both"},
		{name: "first only", totals: sideTotals{WellKnown: 1}, want: "Goal 1 only"},
		{name: "second only", totals: sideTotals{FalsePositives: 1, WellKnown: 1, WellKnownCaught: 1}, want: "Goal 2 only"},
		{name: "neither", totals: sideTotals{FalsePositives: 1, WellKnown: 1}, want: "Fails both"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, goalsOutcome(tc.totals))
		})
	}
}

func TestRenderViewerKeepsTheDataInsideItsScript(t *testing.T) {
	t.Parallel()

	data := viewData{Generated: time.Unix(0, 0), Cases: []viewCase{{Key: "s::x", Text: "</script><script>alert(1)</script>"}}}
	page, err := renderViewer(&data, false)
	require.NoError(t, err)
	html := string(page)
	require.Equal(t, 1, strings.Count(html, "</script>"), "case text must not close the page's script")
	require.NotContains(t, html, "/*VIEW_DATA*/")
	require.Contains(t, html, "const LIVE = false;")
}

func TestRenderViewerLiveModePollsForData(t *testing.T) {
	t.Parallel()

	page, err := renderViewer(nil, true)
	require.NoError(t, err)
	require.Contains(t, string(page), "const LIVE = true;")
	require.Contains(t, string(page), "let DATA = null;")
}

func TestViewerListenAddress(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		address string
		want    string
	}{
		{address: "127.0.0.1:8765", want: "127.0.0.1:8765"},
		{address: "127.0.0.2:0", want: "127.0.0.2:0"},
		{address: "[::1]:8765", want: "[::1]:8765"},
		{address: "[::ffff:127.0.0.1]:8765", want: "[::ffff:127.0.0.1]:8765"},
		{address: "localhost:8765", want: "127.0.0.1:8765"},
		{address: ":8765"},
		{address: "0.0.0.0:8765"},
		{address: "[::]:8765"},
		{address: "192.0.2.1:8765"},
		{address: "example.com:8765"},
		{address: "127.0.0.1"},
	} {
		t.Run(tc.address, func(t *testing.T) {
			t.Parallel()
			address, err := viewerListenAddress(tc.address)
			if tc.want == "" {
				require.Error(t, err)
				require.Error(t, serveViewer(t.Context(), tc.address, false, viewSource{}))
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, address)
		})
	}
}
