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

func writeRun(t *testing.T, label string, records ...caseRecord) string {
	t.Helper()
	dir := t.TempDir()
	lines := make([]string, 0, len(records))
	for _, rec := range records {
		raw, err := json.Marshal(rec)
		require.NoError(t, err)
		lines = append(lines, string(raw))
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, casesFile), []byte(strings.Join(lines, "\n")), 0o600))
	manifest, err := json.Marshal(runManifest{Label: label, Ref: label + " @ abc", PrefilterModel: "typesafe/jev-1.13", PrefilterThreshold: 0.5, ConfirmationModel: "anthropic/claude-sonnet-5.5", ConfirmationPromptSHA256: "1f4c2ffbb7cd153e", PrefilterQuestionsSHA256: "0219e560da66ed88"})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, manifestFile), manifest, 0o600))
	return dir
}

func viewRecord(c labeledCase, status caseStatus) caseRecord {
	return caseRecord{Key: caseKey(c), Hash: caseHash(c), Status: status, CostUSD: 0.001, LatencyMS: 1000}
}

func TestBuildViewDataJoinsBothRuns(t *testing.T) {
	t.Parallel()

	caught, fixed, pending := recordsCase("caught", "malicious", "DAN"), recordsCase("fixed", "benign", ""), recordsCase("pending", "malicious", "")
	base := writeRun(t, "main", viewRecord(caught, statusClear), viewRecord(fixed, statusFlagged), viewRecord(pending, statusFlagged))
	change := writeRun(t, "this change", viewRecord(caught, statusFlagged), viewRecord(fixed, statusClear))
	opts := options{runDir: change, baseRunDir: base, maxFalsePositives: 0, minRecall: 0.8}

	data, err := buildViewData(opts, []labeledCase{caught, fixed, pending}, time.Unix(0, 0))
	require.NoError(t, err)
	require.Len(t, data.Sides, 2)
	require.Equal(t, "main", data.Sides[0].Manifest.Label)
	require.Equal(t, 2, data.Sides[0].RequiredCaught)
	require.Nil(t, data.Cases[2].Change)
	require.Equal(t, statusFlagged, data.Cases[2].Base.Status)
	require.Equal(t, caseFlips{NewlyCaught: 1, NewlyMissed: 0, NewFalsePositives: 0, FixedFalsePositives: 1}, compareSides(data.Cases))
	require.Equal(t, "Incomplete", gateOutcome(data.Sides[1]))
}

func TestSummaryMarkdownListsBothRunsAndFlips(t *testing.T) {
	t.Parallel()

	attack := recordsCase("attack", "malicious", "DAN")
	base := writeRun(t, "main", viewRecord(attack, statusClear))
	change := writeRun(t, "this change", viewRecord(attack, statusFlagged))
	data, err := buildViewData(options{runDir: change, baseRunDir: base, maxFalsePositives: 0, minRecall: 0.8}, []labeledCase{attack}, time.Unix(0, 0))
	require.NoError(t, err)

	md := summaryMarkdown(data)
	require.Contains(t, md, "| main (main @ abc) | 0 | 0 of 1 | 0 of 1 (0.0%)")
	require.Contains(t, md, "| this change (this change @ abc) | 0 | 1 of 1 | 1 of 1 (100.0%)")
	require.Contains(t, md, "| Pass | Meets both |")
	require.Contains(t, md, "Compared with main: 1 newly caught · 0 newly missed")
	require.Contains(t, md, "- this change: typesafe/jev-1.13 ≥ 0.50 → anthropic/claude-sonnet-5.5 · confirmation prompt `1f4c2ffbb7` · questions `0219e560da`")
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

func TestViewerGateOutcomes(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		minRecall float64
		maxFP     int
		label     string
		status    caseStatus
		want      string
	}{
		{name: "recall requires attacks", minRecall: 0.8, maxFP: -1, label: "benign", status: statusClear, want: "Fail"},
		{name: "recall with FP limit requires attacks", minRecall: 0.8, maxFP: 0, label: "benign", status: statusClear, want: "Fail"},
		{name: "zero recall is disabled", maxFP: -1, label: "benign", status: statusClear, want: "Not set"},
		{name: "FP only passes without attacks", maxFP: 0, label: "benign", status: statusClear, want: "Pass"},
		{name: "FP only rejects flagged benign", maxFP: 0, label: "benign", status: statusFlagged, want: "Fail"},
		{name: "recall passes", minRecall: 0.8, maxFP: -1, label: "malicious", status: statusFlagged, want: "Pass"},
		{name: "recall fails", minRecall: 0.8, maxFP: -1, label: "malicious", status: statusClear, want: "Fail"},
		{name: "out of credit is incomplete", minRecall: 0.8, maxFP: 0, label: "malicious", status: statusOutOfCredit, want: "Incomplete"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := recordsCase("case", tc.label, "")
			dir := writeRun(t, "change", viewRecord(c, tc.status))
			data, err := buildViewData(options{runDir: dir, minRecall: tc.minRecall, maxFalsePositives: tc.maxFP}, []labeledCase{c}, time.Unix(0, 0))
			require.NoError(t, err)
			require.Equal(t, tc.want, data.Sides[0].GateStatus)
			require.Contains(t, summaryMarkdown(data), "| "+tc.want+" |")
			page, err := renderViewer(&data, false)
			require.NoError(t, err)
			require.Contains(t, string(page), `"gate_status":"`+tc.want+`"`)
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
				require.Error(t, serveViewer(t.Context(), options{serve: tc.address}, nil))
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, address)
		})
	}
}

func TestHistoricalRunPreservesRefusalFallback(t *testing.T) {
	t.Parallel()

	attack := recordsCase("attack", "malicious", "DAN")
	dir := writeRun(t, "historical", viewRecord(attack, statusFlagged))
	raw := []byte(`{"label":"historical","prefilter_model":"typesafe/jev-1.13","prefilter_threshold":0.5,"confirmation_model":"anthropic/claude-sonnet-5.5","refusal_fallback_model":"anthropic/claude-opus-4.8"}`)
	require.NoError(t, os.WriteFile(filepath.Join(dir, manifestFile), raw, 0o600))
	data, err := buildViewData(options{runDir: dir, minRecall: 0.8}, []labeledCase{attack}, time.Unix(0, 0))
	require.NoError(t, err)
	require.Equal(t, "anthropic/claude-opus-4.8", data.Sides[0].Manifest.RefusalFallbackModel)
	require.Contains(t, summaryMarkdown(data), "anthropic/claude-sonnet-5.5 (refusal fallback: anthropic/claude-opus-4.8)")
	page, err := renderViewer(&data, false)
	require.NoError(t, err)
	require.Contains(t, string(page), `"refusal_fallback_model":"anthropic/claude-opus-4.8"`)
}

func TestCurrentManifestOmitsRefusalFallback(t *testing.T) {
	t.Parallel()

	manifest, err := currentManifest(options{})
	require.NoError(t, err)
	require.Empty(t, manifest.RefusalFallbackModel)
	raw, err := json.Marshal(manifest)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "refusal_fallback_model")
}
