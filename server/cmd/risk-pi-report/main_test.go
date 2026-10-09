package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// loadReportCorpus loads the committed corpus, the cases the evaluation
// report scored.
func loadReportCorpus(t *testing.T) []labeledCase {
	t.Helper()
	corpus, err := loadCorpus(filepath.Join("..", "..", "..", defaultCorpusDir))
	require.NoError(t, err)
	return corpus
}

func casesFrom(corpus []labeledCase, source string) []labeledCase {
	var out []labeledCase
	for _, c := range corpus {
		if c.Source == source {
			out = append(out, c)
		}
	}
	return out
}

func TestLoadCorpusPreservesTrajectoryTwinSemantics(t *testing.T) {
	t.Parallel()

	twins := casesFrom(loadReportCorpus(t), "trajectory_twins")
	require.Len(t, twins, 74)
	byText := make(map[string][]labeledCase)
	for _, row := range twins {
		byText[row.Text] = append(byText[row.Text], row)
	}
	require.Len(t, byText["cat ~/.config/example/credentials"], 2)
}

func TestLoadCorpusPreservesAgentDojoTaskOccurrences(t *testing.T) {
	t.Parallel()

	cases := casesFrom(loadReportCorpus(t), "agentdojo")
	require.Len(t, cases, 246)
	malicious := 0
	ids := make(map[string]bool, len(cases))
	for _, row := range cases {
		require.False(t, ids[row.ID], "case IDs remain unique")
		ids[row.ID] = true
		malicious += boolInt(row.Label == "malicious")
	}
	require.Equal(t, 204, malicious)
	require.Equal(t, 42, len(cases)-malicious)
}

func TestLoadedDeepsetKeepsItsRelabelProvenance(t *testing.T) {
	t.Parallel()

	for _, c := range casesFrom(loadReportCorpus(t), "deepset") {
		if c.ID != "deepset.train.0032" {
			continue
		}
		require.Equal(t, "malicious", c.OriginalLabel)
		require.Equal(t, "persona_roleplay", c.RelabelReason)
		require.Equal(t, map[string]string{"original_label": "malicious", "relabel_reason": "persona_roleplay"}, caseContext(c))
		return
	}
	t.Fatal("deepset.train.0032 is not in the corpus")
}

func TestLoadCorpusLoadsEveryFixtureFileInOrder(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	files := map[string]string{
		"zz_new.jsonl":           `{"id": "z", "label": "benign", "text": "only in zz", "source": "zz_new"}`,
		"mm_new.jsonl":           `{"id": "m", "label": "benign", "text": "shared", "source": "mm_new"}`,
		"aa_new.jsonl":           `{"id": "a", "label": "benign", "text": "only in aa", "source": "aa_new"}`,
		"deepset.jsonl":          `{"id": "d", "label": "malicious", "text": "shared", "source": "deepset"}`,
		"trajectory_twins.jsonl": `{"id": "t1", "label": "benign", "text": "twin", "source": "trajectory_twins"}` + "\n" + `{"id": "t2", "label": "malicious", "text": "twin", "source": "trajectory_twins"}`,
		"notes.md":               "not a fixture",
	}
	for name, body := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body+"\n"), 0o600))
	}

	corpus, err := loadCorpus(dir)
	require.NoError(t, err)
	keys := make([]string, 0, len(corpus))
	for _, c := range corpus {
		keys = append(keys, caseKey(c))
	}
	// deepset loads first and keeps the shared text; files outside the known
	// order still load, after the known files, in name order.
	require.Equal(t, []string{"deepset::d", "trajectory_twins::t1", "trajectory_twins::t2", "aa_new::a", "zz_new::z"}, keys)
}

func TestLoadCorpusRejectsADirectoryWithoutFixtures(t *testing.T) {
	t.Parallel()

	_, err := loadCorpus(t.TempDir())
	require.ErrorContains(t, err, "no cases in")
}

func TestCascadeSmokeCasesStayOutOfTheScoredCorpus(t *testing.T) {
	t.Parallel()

	require.Empty(t, casesFrom(loadReportCorpus(t), "cascade_context"))
	smoke, err := loadCorpus(filepath.Join("..", "..", "..", fixturesPath, "cascade_smoke"))
	require.NoError(t, err)
	require.Len(t, smoke, 4)
}

func TestFirstEnvSkipsMisePlaceholder(t *testing.T) {
	t.Setenv("RISK_PI_REPORT_TEST_DEV_KEY", unsetEnvPlaceholder)
	t.Setenv("RISK_PI_REPORT_TEST_API_KEY", "ci-key")
	require.Equal(t, "ci-key", firstEnv("RISK_PI_REPORT_TEST_DEV_KEY", "RISK_PI_REPORT_TEST_API_KEY"))
}
