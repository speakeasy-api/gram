package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadCorpusPreservesTrajectoryTwinSemantics(t *testing.T) {
	t.Parallel()

	corpus, err := loadCorpus(filepath.Join("..", "..", "..", defaultCorpusDir))
	require.NoError(t, err)

	byText := make(map[string][]labeledCase)
	twins := 0
	for _, row := range corpus {
		if row.Source != "trajectory_twins" {
			continue
		}
		twins++
		byText[row.Text] = append(byText[row.Text], row)
	}
	require.Equal(t, 74, twins)
	require.Len(t, byText["cat ~/.config/example/credentials"], 2)
}

func TestLoadCorpusLoadsEveryFixtureFileInOrder(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	files := map[string]string{
		"zz_new.jsonl":           `{"id": "z", "label": "benign", "text": "shared", "source": "zz_new"}`,
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
	// deepset loads first and keeps the shared text; a file outside the
	// known order still loads, after the known files, in name order.
	require.Equal(t, []string{"deepset::d", "trajectory_twins::t1", "trajectory_twins::t2", "aa_new::a"}, keys)
}

func TestLoadCorpusRejectsADirectoryWithoutFixtures(t *testing.T) {
	t.Parallel()

	_, err := loadCorpus(t.TempDir())
	require.ErrorContains(t, err, "no cases in")
}
