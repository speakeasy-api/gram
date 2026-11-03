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
