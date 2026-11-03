package gram

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
)

// An unusable keyring must not stop a process that only wants device keys.
func TestConnectionDeviceKeys_UnusableKeyringOnlyWarns(t *testing.T) {
	t.Parallel()

	var logged bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logged, nil))

	require.NotNil(t, connectionDeviceKeys(t.Context(), logger, "not json"))
	require.Contains(t, logged.String(), "device_key stays empty")
}

// An unset keyring warns about device_key, not about the retro-exclusion work these processes don't run.
func TestConnectionDeviceKeys_UnsetKeyringWarnsAboutDeviceKey(t *testing.T) {
	t.Parallel()

	var logged bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logged, nil))

	require.NotNil(t, connectionDeviceKeys(t.Context(), logger, ""))
	require.Contains(t, logged.String(), "not configured; device_key stays empty")
	require.NotContains(t, logged.String(), "unusable")
	require.NotContains(t, logged.String(), "error=")
	require.NotContains(t, logged.String(), "exclusion")
}
