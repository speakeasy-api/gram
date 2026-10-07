package mcp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

// A hosted member may receive only the single unqualified token of its own
// derived remote_session_issuer: sibling, member-qualified and ambiguous
// tokens degrade to no token, never an error.
func TestHostedMemberTokens(t *testing.T) {
	t.Parallel()

	issuerID := uuid.New()
	member := metaMember{slug: "m", toolsetID: uuid.NullUUID{UUID: uuid.New(), Valid: true}, remoteSessionIssuerID: uuid.NullUUID{UUID: issuerID, Valid: true}}
	entry := func(token, resource string, issuer uuid.UUID) remotesessions.UpstreamToken {
		return remotesessions.UpstreamToken{Token: token, Resource: resource, RemoteSessionIssuerID: issuer}
	}
	tokens := func(entries ...remotesessions.UpstreamToken) remotesessions.ClientTokens {
		m := make(remotesessions.ClientTokens, len(entries))
		for _, e := range entries {
			m[uuid.New()] = e
		}
		return m
	}

	require.Nil(t, hostedMemberTokens(nil, member), "no tokens stays no tokens")
	require.Nil(t, hostedMemberTokens(tokens(entry("sibling", "", uuid.New())), member), "a sibling's token never reaches a hosted member")
	require.Nil(t, hostedMemberTokens(tokens(entry("a", "https://member-a.example.com/mcp", issuerID)), member), "a member-qualified token never reaches a hosted member")
	require.Nil(t, hostedMemberTokens(tokens(entry("a", "", uuid.New())), metaMember{slug: "m"}), "a member with no derived issuer gets no token")
	require.Nil(t, hostedMemberTokens(tokens(entry("a", "", issuerID), entry("b", "", issuerID)), member), "several clients of the member's issuer name no single credential")
	partial := entry("sibling", "", issuerID)
	partial.IssuerBoundClients = 2
	require.Nil(t, hostedMemberTokens(tokens(partial), member), "the one connected client of two bound to the member's issuer may be a sibling's")

	got := hostedMemberTokens(tokens(entry("sibling", "", uuid.New()), entry("own", "", issuerID)), member)
	require.Len(t, got, 1, "the member's own unqualified token is the hosted credential")
	require.Equal(t, "own", got[issuerID].Token)
}
