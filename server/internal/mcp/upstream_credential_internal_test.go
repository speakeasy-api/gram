package mcp

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

func TestServedCredentialNamesAnOwnerOnlyForAPersonalConnection(t *testing.T) {
	t.Parallel()
	clientID := uuid.New()
	cases := []struct {
		name       string
		clientID   uuid.UUID
		clientType string
		want       contextvalues.UpstreamCredential
	}{
		{"personal connection", clientID, authClientTypeRemoteSession, contextvalues.UpstreamCredential{AuthClientID: clientID.String(), AuthClientType: authClientTypeRemoteSession, ConnectionOwnerUserID: "user_owner"}},
		{"service account", clientID, authClientTypeServiceAccount, contextvalues.UpstreamCredential{AuthClientID: clientID.String(), AuthClientType: authClientTypeServiceAccount, ConnectionOwnerUserID: ""}},
		{"static credential", clientID, authClientTypeStaticCredential, contextvalues.UpstreamCredential{AuthClientID: clientID.String(), AuthClientType: authClientTypeStaticCredential, ConnectionOwnerUserID: ""}},
		{"registration with no auth client", uuid.Nil, authClientTypeRemoteSession, contextvalues.UpstreamCredential{AuthClientID: "", AuthClientType: authClientTypeRemoteSession, ConnectionOwnerUserID: "user_owner"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, servedCredential(tc.clientID, tc.clientType, "user_owner"))
		})
	}
}

func TestAuthClientResolutionServesACredentialOnlyWhenAClientServes(t *testing.T) {
	t.Parallel()
	clientID := uuid.New()
	cases := []struct {
		name    string
		res     *authClientResolution
		serves  bool
		ownerID string
	}{
		{"resolved personal client", &authClientResolution{outcome: authClientResolved, clientID: clientID, clientType: authClientTypeRemoteSession, connectionOwner: "user_owner"}, true, "user_owner"},
		{"resolved shared client", &authClientResolution{outcome: authClientResolved, clientID: clientID, clientType: authClientTypeServiceAccount, connectionOwner: "user_owner"}, true, ""},
		{"named client handed back", &authClientResolution{outcome: authClientLegacy, clientID: clientID, clientType: authClientTypeServiceAccount}, true, ""},
		{"unmapped choice", &authClientResolution{outcome: authClientLegacy, clientType: ""}, false, ""},
		{"no auth", &authClientResolution{outcome: authClientNoAuth, clientID: clientID, clientType: authClientTypeStaticCredential}, false, ""},
		{"refused", &authClientResolution{outcome: authClientNoAccess, clientID: clientID, clientType: authClientTypeRemoteSession}, false, ""},
		{"no resolution", nil, false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := tc.res.servedCredential()
			require.Equal(t, tc.serves, ok)
			if !tc.serves {
				return
			}
			require.Equal(t, clientID.String(), got.AuthClientID)
			require.Equal(t, tc.res.clientType, got.AuthClientType)
			require.Equal(t, tc.ownerID, got.ConnectionOwnerUserID)
		})
	}
}

// A gateway member the credential's choices serve is dialed with its
// resolved auth client named, so the call's log row can say how Speakeasy
// signed in: a personal connection with its owner, or a shared credential
// with none.
func TestRouteMetaMemberNamesTheChosenAuthClient(t *testing.T) {
	t.Parallel()
	pool, err := TestInfra.CloneTestDatabase(t, "route_member_credential")
	require.NoError(t, err)
	cases := []struct {
		name       string
		clientType string
		wantOwner  string
	}{
		{"personal connection", authClientTypeRemoteSession, "user_owner"},
		{"shared credential", authClientTypeServiceAccount, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newAuthClientFixture(t, pool)
			resource := "https://mcp.example.com/" + f.remoteID.String()[:8]
			clientID := uuid.New()
			member := metaMember{serverID: f.serverID, projectID: f.projectID, slug: "member", backend: metaMemberBackendProxied, remoteServerID: validUUID(f.remoteID), chosenClientID: validUUID(f.registration),
				authClient: &authClientResolution{outcome: authClientResolved, clientID: clientID, clientType: tc.clientType, connectionOwner: "user_owner"}}
			choices := &memberChoices{byServer: map[uuid.UUID]*memberTokens{}, resolve: func(context.Context, metaMember) (map[uuid.UUID]remotesessions.UpstreamToken, error) {
				return map[uuid.UUID]remotesessions.UpstreamToken{uuid.New(): {Token: "chosen", Resource: resource}}, nil
			}}

			dial, backend, err := f.service.routeMetaMember(t.Context(), f.service.logger, metaGateContext{agentID: uuid.New(), organizationID: f.orgID, memberChoices: choices}, member, "")
			require.NoError(t, err)
			require.Equal(t, "remote", backend)
			require.True(t, dial.hasCredential)
			require.Equal(t, contextvalues.UpstreamCredential{AuthClientID: clientID.String(), AuthClientType: tc.clientType, ConnectionOwnerUserID: tc.wantOwner}, dial.credential)
		})
	}
}

// An agent gateway member served by a shared client through the member's own
// issuer records that client with no connection owner.
func TestAgentMemberLegacyClientRecordsASharedCredential(t *testing.T) {
	t.Parallel()
	pool, err := TestInfra.CloneTestDatabase(t, "agent_member_shared_credential")
	require.NoError(t, err)
	f := newAuthClientFixture(t, pool)
	client := f.oauthClient(authClientTypeServiceAccount)
	f.service.upstreamTokens = &fakeUpstreamTokens{issuerID: uuid.New(), bound: uuid.New()}
	agentID := uuid.New()
	gate := &metaGateContext{agentID: agentID, organizationID: f.orgID, servedCredentials: map[uuid.UUID]contextvalues.UpstreamCredential{}}
	member := metaMember{serverID: f.serverID, projectID: f.projectID, slug: "member", backend: metaMemberBackendProxied}

	_, err = f.service.agentMemberClientTokens(admittedAgentContext(t, f.orgID, agentID), gate, member, f.issuerID, f.registration, authClientTypeServiceAccount)
	require.NoError(t, err)

	require.Equal(t, contextvalues.UpstreamCredential{AuthClientID: client.String(), AuthClientType: authClientTypeServiceAccount, ConnectionOwnerUserID: ""}, gate.servedCredentials[f.registration])
}
