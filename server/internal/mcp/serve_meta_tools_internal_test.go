package mcp

import (
	"errors"
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

	hosted := func(tokens map[uuid.UUID]remotesessions.UpstreamToken, member metaMember) map[uuid.UUID]remotesessions.UpstreamToken {
		got, err := hostedMemberTokens(tokens, member)
		require.NoError(t, err)
		return got
	}

	require.Nil(t, hosted(nil, member), "no tokens stays no tokens")
	require.Nil(t, hosted(keyed(uuid.New(), entry("sibling", "")), member), "a sibling's token never reaches a hosted member")
	require.Nil(t, hosted(keyed(issuerID, entry("a", "https://member-a.example.com/mcp")), member), "a member-qualified token never reaches a hosted member")
	require.Nil(t, hosted(keyed(uuid.New(), entry("a", "")), metaMember{slug: "m"}), "a member with no derived issuer gets no token")

	m := keyed(uuid.New(), entry("sibling", ""))
	m[issuerID] = entry("own", "")
	got := hosted(m, member)
	require.Len(t, got, 1, "the member's own unqualified token is the hosted credential")
	require.Equal(t, "own", got[issuerID].Token)
}

func TestHostedMemberTokens_SelfCredential(t *testing.T) {
	t.Parallel()

	issuerID := uuid.New()
	member := metaMember{slug: "m", toolsetID: uuid.NullUUID{UUID: uuid.New(), Valid: true}, remoteSessionIssuerID: uuid.NullUUID{UUID: issuerID, Valid: true}}

	got, err := hostedMemberTokens(map[uuid.UUID]remotesessions.UpstreamToken{
		issuerID: {Token: "self-token", CredentialOwner: remotesessions.CredentialOwnerSelf},
	}, member)
	require.NoError(t, err)
	require.Equal(t, "self-token", got[issuerID].Token, "a self credential requested for no resource serves the hosted member")

	got, err = hostedMemberTokens(map[uuid.UUID]remotesessions.UpstreamToken{
		issuerID: {Token: "self-token", Resource: "https://remote.example.com/mcp", CredentialOwner: remotesessions.CredentialOwnerSelf},
	}, member)
	require.NoError(t, err)
	require.Nil(t, got, "a self credential audience-bound to a remote upstream never reaches a hosted member")

	_, err = hostedMemberTokens(map[uuid.UUID]remotesessions.UpstreamToken{
		issuerID: {CredentialOwner: remotesessions.CredentialOwnerSelf, ClientCredentialErr: remotesessions.ErrClientCredentialMisconfigured},
	}, member)
	memberErr, ok := errors.AsType[*metaMemberError](err)
	require.True(t, ok, "error: %v", err)
	require.Contains(t, memberErr.message, "Ask an admin of the server's project to fix the server's credential", "the member's tools never run without its credential")

	_, err = hostedMemberTokens(map[uuid.UUID]remotesessions.UpstreamToken{
		issuerID: {CredentialOwner: remotesessions.CredentialOwnerSelf, ClientCredentialErr: remotesessions.ErrClientCredentialUnavailable},
	}, member)
	outage, ok := errors.AsType[memberDiscoveryError](err)
	require.True(t, ok, "error: %v", err)
	require.False(t, outage.retryable, "the request resolved its token map once, so a retry would reread the same outage")
}

// hostedMemberTokens narrows a gate's token map to what a hosted member may
// receive: the entry keyed by the member's own derived remote_session_issuer,
// and only when its grant is unqualified. The tunneled arm of
// routeMetaMemberToken applies the same rule. A lone-token fallback would
// forward a sibling's bearer once partial resolution leaves gaps in the map,
// and a resource-qualified token is audience-bound to the remote upstream it
// was consented for; both cases yield no token. A self client whose
// credential could not be obtained yields the member-scoped remedy instead,
// so the member's tools never run without the credential it holds.
func hostedMemberTokens(tokens map[uuid.UUID]remotesessions.UpstreamToken, member metaMember) (map[uuid.UUID]remotesessions.UpstreamToken, error) {
	if !member.remoteSessionIssuerID.Valid {
		return nil, nil
	}
	entry, ok := tokens[member.remoteSessionIssuerID.UUID]
	if !ok || entry.Resource != "" {
		return nil, nil
	}
	if err := entry.ClientCredentialErr; err != nil {
		return nil, metaMemberClientCredentialError(member, err)
	}
	return map[uuid.UUID]remotesessions.UpstreamToken{member.remoteSessionIssuerID.UUID: entry}, nil
}
