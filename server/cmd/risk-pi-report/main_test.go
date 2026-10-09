package main

import (
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
