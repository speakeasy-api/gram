package mcp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// appendRemoteSessionTokenInputs must fail closed when more than one
// remote-session token resolves: without per-tool routing (AIS-152) it cannot
// tell which tool needs which issuer's token, and injecting all with empty
// securityKeys could forward the wrong bearer upstream.

func TestAppendRemoteSessionTokenInputs_EmptyMapAddsNothing(t *testing.T) {
	t.Parallel()

	got, err := appendRemoteSessionTokenInputs(nil, nil)
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestAppendRemoteSessionTokenInputs_SingleTokenTaggedWithIssuer(t *testing.T) {
	t.Parallel()

	issuerID := uuid.New()
	got, err := appendRemoteSessionTokenInputs(nil, map[uuid.UUID]string{issuerID: "upstream-token"})
	require.NoError(t, err)
	require.Equal(t, []oauthTokenInputs{{
		securityKeys:          nil,
		remoteSessionIssuerID: uuid.NullUUID{UUID: issuerID, Valid: true},
		Token:                 "upstream-token",
	}}, got)
}

func TestAppendRemoteSessionTokenInputs_MultipleTokensFailsClosed(t *testing.T) {
	t.Parallel()

	got, err := appendRemoteSessionTokenInputs(nil, map[uuid.UUID]string{
		uuid.New(): "token-a",
		uuid.New(): "token-b",
	})
	require.Error(t, err)
	require.Nil(t, got)
}
