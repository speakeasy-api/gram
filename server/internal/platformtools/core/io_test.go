package core

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// A tool can be dispatched with no request body; DecodeInput must treat a nil
// payload as empty input rather than panicking on io.ReadAll(nil).
func TestDecodeInputHandlesNilPayload(t *testing.T) {
	t.Parallel()

	var dst struct {
		A string `json:"a"`
	}
	require.NoError(t, DecodeInput(nil, &dst))
	require.Empty(t, dst.A)
}
