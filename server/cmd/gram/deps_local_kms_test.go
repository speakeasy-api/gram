package gram

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPreserveLegacyLocalSigningKey_CopiesPrivateKey(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	sourcePath := filepath.Join(root, "cache", "rs256.pem")
	targetPath := filepath.Join(root, "config", "rs256.pem")
	require.NoError(t, os.MkdirAll(filepath.Dir(sourcePath), 0o700))
	require.NoError(t, os.MkdirAll(filepath.Dir(targetPath), 0o700))
	require.NoError(t, os.WriteFile(sourcePath, []byte("cached-key"), 0o600))

	require.NoError(t, preserveLegacyLocalSigningKey(sourcePath, targetPath))
	doc, err := os.ReadFile(targetPath)
	require.NoError(t, err)
	require.Equal(t, []byte("cached-key"), doc)
	info, err := os.Stat(targetPath)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestPreserveLegacyLocalSigningKey_DoesNotReplaceExistingKey(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	sourcePath := filepath.Join(root, "cached.pem")
	targetPath := filepath.Join(root, "configured.pem")
	require.NoError(t, os.WriteFile(sourcePath, []byte("cached-key"), 0o600))
	require.NoError(t, os.WriteFile(targetPath, []byte("configured-key"), 0o600))

	require.NoError(t, preserveLegacyLocalSigningKey(sourcePath, targetPath))
	doc, err := os.ReadFile(targetPath)
	require.NoError(t, err)
	require.Equal(t, []byte("configured-key"), doc)
}

func TestPreserveLegacyLocalSigningKey_RejectsLoosePermissions(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	sourcePath := filepath.Join(root, "cached.pem")
	targetPath := filepath.Join(root, "configured.pem")
	require.NoError(t, os.WriteFile(sourcePath, []byte("cached-key"), 0o644))

	err := preserveLegacyLocalSigningKey(sourcePath, targetPath)
	require.ErrorContains(t, err, "accessible only to its owner")
	_, err = os.Stat(targetPath)
	require.ErrorIs(t, err, os.ErrNotExist)
}
