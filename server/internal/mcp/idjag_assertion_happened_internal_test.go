package mcp

import (
	"context"
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
	code, _, unrecorded := idJAGAssertionHappened(t.Context(), storeDown)
	require.Empty(t, unrecorded)
	require.Equal(t, mcpconnectionattempts.CodeAssertionCheckUnavailable, code)
	meaning, ok := mcpconnectionattempts.MeaningOf(code.String())
	require.True(t, ok)
	require.Equal(t, mcpconnectionattempts.WhoSpeakeasy, meaning.WhoFixes)
	// The token response keys on these, so it stays invalid_grant.
	require.False(t, idjag.IsOutage(storeDown))
	require.False(t, idjag.IsStoreFault(storeDown))

	unusable := &idjag.Error{Reason: idjag.ReasonKeyUnusable, Err: jwks.ErrKeySetInvalid}
	code, _, _ = idJAGAssertionHappened(t.Context(), unusable)
	require.Equal(t, mcpconnectionattempts.CodeConfigurationRequired, code, "a key set that cannot be used is still the administrator's")
}

// A key set fetch that ended in a cancellation writes no row only when this request ended; a live request that shared another caller's fetch gets a temporary problem of ours.
func TestIDJAGAssertionHappenedCancelledKeyFetch(t *testing.T) {
	t.Parallel()

	cancelledFetch := &idjag.Error{Reason: idjag.ReasonKeyUnusable, Err: fmt.Errorf("fetch key set: request key set: %w", context.Canceled)}

	ended, cancel := context.WithCancel(t.Context())
	cancel()
	code, _, unrecorded := idJAGAssertionHappened(ended, cancelledFetch)
	require.Equal(t, mcpconnectionattempts.Code{}, code)
	require.Equal(t, "token_assertion_key_fetch_cancelled", unrecorded)

	code, _, unrecorded = idJAGAssertionHappened(t.Context(), cancelledFetch)
	require.Empty(t, unrecorded)
	require.Equal(t, mcpconnectionattempts.CodeTransientFailure, code)
}
