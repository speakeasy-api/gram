package mcp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

// A hosted member may receive only the unqualified token keyed by its own
// derived remote_session_issuer: sibling and member-qualified tokens degrade
// to no token, never an error.
func TestHostedMemberTokens(t *testing.T) {
	t.Parallel()

	issuerID := uuid.New()
	member := metaMember{slug: "m", toolsetID: uuid.NullUUID{UUID: uuid.New(), Valid: true}, remoteSessionIssuerID: uuid.NullUUID{UUID: issuerID, Valid: true}}
	entry := func(token, resource string) remotesessions.UpstreamToken {
		return remotesessions.UpstreamToken{Token: token, Resource: resource}
	}
	keyed := func(id uuid.UUID, e remotesessions.UpstreamToken) map[uuid.UUID]remotesessions.UpstreamToken {
		return map[uuid.UUID]remotesessions.UpstreamToken{id: e}
	}

	require.Nil(t, hostedMemberTokens(nil, member), "no tokens stays no tokens")
	require.Nil(t, hostedMemberTokens(keyed(uuid.New(), entry("sibling", "")), member), "a sibling's token never reaches a hosted member")
	require.Nil(t, hostedMemberTokens(keyed(issuerID, entry("a", "https://member-a.example.com/mcp")), member), "a member-qualified token never reaches a hosted member")
	require.Nil(t, hostedMemberTokens(keyed(uuid.New(), entry("a", "")), metaMember{slug: "m"}), "a member with no derived issuer gets no token")

	m := keyed(uuid.New(), entry("sibling", ""))
	m[issuerID] = entry("own", "")
	got := hostedMemberTokens(m, member)
	require.Len(t, got, 1, "the member's own unqualified token is the hosted credential")
	require.Equal(t, "own", got[issuerID].Token)
}
