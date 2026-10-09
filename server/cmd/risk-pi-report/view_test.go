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
