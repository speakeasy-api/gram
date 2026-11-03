package gram

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConnectionDeviceKeyOption_WarnsOnlyWhenUnset(t *testing.T) {
	t.Parallel()

	var logged bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logged, nil))

	require.NotNil(t, connectionDeviceKeyOption(t.Context(), logger, "secret"))
	require.Empty(t, logged.String())

	require.NotNil(t, connectionDeviceKeyOption(t.Context(), logger, ""))
	require.Contains(t, logged.String(), "connection device key secret not configured")
}
