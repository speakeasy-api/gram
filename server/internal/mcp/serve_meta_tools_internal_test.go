package mcp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

// A hosted member may receive only a lone unqualified token: qualified
// tokens belong to the member they were consented for, and several tokens
// are unroutable — both degrade to no token, never an error.
func TestHostedMemberTokens(t *testing.T) {
	t.Parallel()

	entry := func(token, resource string) remotesessions.UpstreamToken {
		return remotesessions.UpstreamToken{Token: token, Resource: resource}
	}
	tokens := func(entries ...remotesessions.UpstreamToken) map[uuid.UUID]remotesessions.UpstreamToken {
		m := make(map[uuid.UUID]remotesessions.UpstreamToken, len(entries))
		for _, e := range entries {
			m[uuid.New()] = e
		}
		return m
	}

	require.Nil(t, hostedMemberTokens(nil), "no tokens stays no tokens")
	require.Nil(t, hostedMemberTokens(tokens(entry("a", ""), entry("b", ""))), "several tokens are unroutable")
	require.Nil(t, hostedMemberTokens(tokens(entry("a", "https://member-a.example.com/mcp"))), "a member-qualified token never reaches a hosted member")
	got := hostedMemberTokens(tokens(entry("a", "")))
	require.Len(t, got, 1, "a lone unqualified token is the hosted credential")
	for _, e := range got {
		require.Equal(t, "a", e.Token)
	}
}
