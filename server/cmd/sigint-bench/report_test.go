package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReportDoesNotFollowDestinationSymlinks(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target, report := filepath.Join(dir, "target"), filepath.Join(dir, "report.json")
	require.NoError(t, os.WriteFile(target, []byte("keep"), 0600))
	require.NoError(t, os.Symlink(target, report))
	require.NoError(t, writeReportData(t.Context(), report, []byte("first")))
	require.NoError(t, writeReportData(t.Context(), report, []byte("second")))
	data, err := os.ReadFile(target)
	require.NoError(t, err)
	require.Equal(t, "keep", string(data))
	data, err = os.ReadFile(report)
	require.NoError(t, err)
	require.Equal(t, "second", string(data))
}
