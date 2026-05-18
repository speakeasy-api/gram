package cataloggen_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/outbox/cataloggen"
)

func TestGenerateEmptyDir(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	out, err := cataloggen.Generate(dir)
	require.NoError(t, err)
	require.Contains(t, string(out), "var All = []outbox.EventRegistration{}")
}

func TestGenerateYAMLEmpty(t *testing.T) {
	t.Parallel()

	out, err := cataloggen.GenerateYAML(nil)
	require.NoError(t, err)
	require.Contains(t, string(out), "webhooks: {}")
}

func TestCheckRoundTrip(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	goOut, err := cataloggen.Generate(dir)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "catalog_gen.go"), goOut, 0o600))

	yamlOut, err := cataloggen.GenerateYAML(nil)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "catalog_gen.yaml"), yamlOut, 0o600))

	require.NoError(t, cataloggen.Check(dir))
	require.NoError(t, cataloggen.CheckYAML(dir, nil))
}
