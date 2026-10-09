package main

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/scanners"
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
	for _, name := range requiredCorpusFiles {
		if name != "gram_benigns.jsonl" {
			require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(`{"id": "`+name+`", "label": "benign", "text": "`+name+`", "source": "x"}`+"\n"), 0o600))
		}
	}
	corpus, err := loadCorpus(dir, "")
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

	finding := []scanners.Finding{{RuleID: "pi"}}
	failed := decisionObservation{Calls: []callObservation{{Err: errors.New("parse judge response")}}}
	unfunded := decisionObservation{Calls: []callObservation{{Err: &openrouter.HTTPError{StatusCode: http.StatusPaymentRequired, Err: openrouter.ErrInsufficientCredits}}}}
	tests := []struct {
		name    string
		outcome caseOutcome
		want    caseStatus
	}{
		{name: "flagged", outcome: caseOutcome{findings: finding, verdict: piopenrouter.Stabilized{IsInjection: true}}, want: statusFlagged},
		{name: "clear", outcome: caseOutcome{}, want: statusClear},
		{name: "no verdict", outcome: caseOutcome{observation: failed}, want: statusNoVerdict},
		{name: "out of credit", outcome: caseOutcome{observation: unfunded}, want: statusOutOfCredit},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec := recordFromOutcome(recordsCase("a", "malicious"), "google/gemini", tc.outcome)
			require.Equal(t, tc.want, rec.Status)
			require.Equal(t, "s::a", rec.Key)
			require.Equal(t, "google/gemini", rec.Model)
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
	opts := options{runDir: dir, label: "main", ref: "origin/main @ abc", judgeModel: piopenrouter.Model}

	require.NoError(t, runRecords(t.Context(), opts, []labeledCase{benign, attack}))
	raw, err := os.ReadFile(filepath.Join(dir, manifestFile))
	require.NoError(t, err)
	var manifest runManifest
	require.NoError(t, json.Unmarshal(raw, &manifest))
	require.Equal(t, "main", manifest.Label)
	require.Equal(t, piopenrouter.Model, manifest.ConfirmationModel)
	require.Empty(t, manifest.PrefilterModel)
	require.WithinDuration(t, time.Now(), manifest.Updated, time.Minute)
}

func TestRunRecordsNeedsAKeyForNewCases(t *testing.T) {
	t.Setenv("OPENROUTER_DEV_KEY", "")
	t.Setenv("OPENROUTER_API_KEY", "")

	err := runRecords(t.Context(), options{runDir: t.TempDir(), label: "main"}, []labeledCase{recordsCase("new", "benign")})
	require.ErrorContains(t, err, "set OPENROUTER_DEV_KEY")
}

func TestExcludeSourcesDropsMatchingCases(t *testing.T) {
	t.Parallel()

	keep, drop := labeledCase{ID: "a", Source: "deepset"}, labeledCase{ID: "b", Source: "cascade_context"}
	require.Equal(t, []labeledCase{keep}, excludeSources([]labeledCase{keep, drop}, "cascade_context"))
	require.Equal(t, []labeledCase{keep, drop}, excludeSources([]labeledCase{keep, drop}, ""))
}

func TestFirstEnvSkipsTheMisePlaceholder(t *testing.T) {
	t.Setenv("OPENROUTER_DEV_KEY", unsetEnvPlaceholder)
	t.Setenv("OPENROUTER_API_KEY", "sk-test")
	require.Equal(t, "sk-test", firstEnv("OPENROUTER_DEV_KEY", "OPENROUTER_API_KEY"))
}
