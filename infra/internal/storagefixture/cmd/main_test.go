package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCommitLocalFile_SyncFailureRemovesFinalLink(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	temporary, final := filepath.Join(dir, "temporary"), filepath.Join(dir, "object.parquet")
	require.NoError(t, os.WriteFile(temporary, []byte("complete object"), 0o600))

	failure := errors.New("directory sync failed")
	var visibleBeforeSync bool
	err := commitLocalFile(temporary, final, func() error {
		_, statErr := os.Stat(final)
		visibleBeforeSync = statErr == nil
		return failure
	})
	require.ErrorIs(t, err, failure)
	require.True(t, visibleBeforeSync)

	_, err = os.Stat(final)
	require.ErrorIs(t, err, os.ErrNotExist)

	data, err := os.ReadFile(temporary)
	require.NoError(t, err)
	require.Equal(t, "complete object", string(data))
}

func TestCommitLocalFile_PreservesExistingObject(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	temporary, final := filepath.Join(dir, "temporary"), filepath.Join(dir, "object.parquet")
	require.NoError(t, os.WriteFile(temporary, []byte("new object"), 0o600))
	require.NoError(t, os.WriteFile(final, []byte("existing object"), 0o600))

	var synced bool
	err := commitLocalFile(temporary, final, func() error { synced = true; return nil })
	require.ErrorIs(t, err, os.ErrExist)
	require.False(t, synced)

	data, err := os.ReadFile(final)
	require.NoError(t, err)
	require.Equal(t, "existing object", string(data))
}
