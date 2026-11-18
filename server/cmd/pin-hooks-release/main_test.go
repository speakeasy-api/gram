package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestScanRelayProviders covers the AST scan: a standalone and a grouped
// Provider const are both found, a dir with no provider constants is an
// error (an empty list must never be pinned silently), and an unparseable
// file is an error.
func TestScanRelayProviders(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "pi.go"), []byte(`package relay

import "github.com/speakeasy-api/agenthooks"

const ProviderPi agenthooks.Provider = "pi"
`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "grouped.go"), []byte(`package relay

import "github.com/speakeasy-api/agenthooks"

const (
	ProviderVibe agenthooks.Provider = "vibe"
	other        = "not-a-provider"
)
`), 0o600))
	// Not a provider declaration: wrong package, wrong type, wrong name.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "other.go"), []byte(`package relay

const (
	ProviderX string = "x"
	Foo agenthooks.Provider = "no-prefix"
)
`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "skip_test.go"), []byte(`package relay

const ProviderTest agenthooks.Provider = "test"
`), 0o600))

	got, err := scanRelayProviders(dir)
	require.NoError(t, err)
	require.Equal(t, []string{"pi", "vibe"}, got)

	empty := t.TempDir()
	_, err = scanRelayProviders(empty)
	require.ErrorContains(t, err, "refusing to pin an empty provider list")

	broken := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(broken, "bad.go"), []byte(`package relay

const Provider = `), 0o600))
	_, err = scanRelayProviders(broken)
	require.ErrorContains(t, err, "parse")
}
