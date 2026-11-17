package mcp

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/usersessions/assertion/idjag"
	"github.com/speakeasy-api/gram/server/internal/usersessions/jwks"
)

// A failed read of our own key store on the stale-key fallback is recorded as ours, not as the administrator's setup.
func TestIDJAGAssertionHappenedStaleKeyReadFailure(t *testing.T) {
	t.Parallel()

	storeDown := &idjag.Error{Reason: idjag.ReasonKeyUnusable, Err: fmt.Errorf("read stale issuer keys: %w: %w", idjag.ErrStaleKeysUnreadable, oops.MarkStoreConnectionLost(errors.New("key store unreachable")))}
	code, _, unrecorded := idJAGAssertionHappened(storeDown)
	require.Empty(t, unrecorded)
	require.Equal(t, mcpconnectionattempts.CodeAssertionCheckUnavailable, code)
	meaning, ok := mcpconnectionattempts.MeaningOf(code.String())
	require.True(t, ok)
	require.Equal(t, mcpconnectionattempts.WhoSpeakeasy, meaning.WhoFixes)
	// The token response keys on these, so it stays invalid_grant.
	require.False(t, idjag.IsOutage(storeDown))
	require.False(t, idjag.IsStoreFault(storeDown))

	unusable := &idjag.Error{Reason: idjag.ReasonKeyUnusable, Err: jwks.ErrKeySetInvalid}
	code, _, _ = idJAGAssertionHappened(unusable)
	require.Equal(t, mcpconnectionattempts.CodeConfigurationRequired, code, "a key set that cannot be used is still the administrator's")
}
