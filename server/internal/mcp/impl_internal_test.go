package mcp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

// appendRemoteSessionTokenInputs must fail closed when more than one
// remote-session token resolves: nothing maps a tool's security scheme to a
// remote_session_issuer yet (AGE-3285), so it cannot tell which tool needs
// which issuer's token, and injecting all with empty securityKeys could
// forward the wrong bearer upstream.

func TestAppendRemoteSessionTokenInputs_EmptyMapAddsNothing(t *testing.T) {
	t.Parallel()

	got, err := appendRemoteSessionTokenInputs(nil, nil)
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestAppendRemoteSessionTokenInputs_SingleTokenTaggedWithIssuer(t *testing.T) {
	t.Parallel()

	issuerID := uuid.New()
	got, err := appendRemoteSessionTokenInputs(nil, map[uuid.UUID]remotesessions.UpstreamToken{
		issuerID: {Token: "upstream-token", Resource: "https://a.example.com/mcp", RemoteSessionClientID: uuid.New()},
	})
	require.NoError(t, err)
	require.Equal(t, []oauthTokenInputs{{
		securityKeys:          nil,
		remoteSessionIssuerID: uuid.NullUUID{UUID: issuerID, Valid: true},
		Token:                 "upstream-token",
	}}, got)
}

func TestAppendRemoteSessionTokenInputs_MultipleTokensFailsClosed(t *testing.T) {
	t.Parallel()

	got, err := appendRemoteSessionTokenInputs(nil, map[uuid.UUID]remotesessions.UpstreamToken{
		uuid.New(): {Token: "token-a", Resource: "", RemoteSessionClientID: uuid.New()},
		uuid.New(): {Token: "token-b", Resource: "", RemoteSessionClientID: uuid.New()},
	})
	require.Error(t, err)
	require.Nil(t, got)
}
