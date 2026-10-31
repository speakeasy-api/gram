package mcp

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// These tests deliberately provide no database or challenge manager. A selected
// proxied member must never fall through to legacy issuer resolution.
func TestAgentMemberSelectedResolution(t *testing.T) {
	t.Parallel()
	for _, mode := range []feature.AuthClientResolverMode{feature.AuthClientResolverOff, feature.AuthClientResolverShadow, feature.AuthClientResolverEnforced} {
		for _, tc := range []struct {
			name        string
			tokens      map[uuid.UUID]remotesessions.UpstreamToken
			resolveErr  error
			unavailable error
			wantErr     bool
		}{
			{name: "selected token", tokens: map[uuid.UUID]remotesessions.UpstreamToken{uuid.New(): {Token: "selected-token"}}},
			{name: "nil resolution", wantErr: true},
			{name: "empty resolution", tokens: map[uuid.UUID]remotesessions.UpstreamToken{}, wantErr: true},
			{name: "empty token", tokens: map[uuid.UUID]remotesessions.UpstreamToken{uuid.New(): {}}, wantErr: true},
			{name: "resolver failure", resolveErr: errors.New("credential lookup failed"), wantErr: true},
			{name: "missing attachment", resolveErr: remotesessions.ErrNoValidToken, wantErr: true},
			{name: "unavailable member", unavailable: errors.New("member is not permitted"), wantErr: true},
		} {
			t.Run(agentMemberModeName(mode)+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				flags := new(feature.InMemory)
				flags.SetFlag(feature.FlagAuthClientResolver, "test-org", mode == feature.AuthClientResolverEnforced)
				flags.SetFlag(feature.FlagAuthClientResolverShadow, "test-org", mode == feature.AuthClientResolverShadow)
				s := &Service{features: flags}
				member := metaMember{serverID: uuid.New(), projectID: uuid.New(), slug: "selected-member", backend: metaMemberBackendProxied,
					chosenClientID: uuid.NullUUID{UUID: uuid.New(), Valid: true}, upstreamUnavailable: tc.unavailable}
				calls := 0
				choices := &memberChoices{enforced: mode == feature.AuthClientResolverEnforced, byServer: map[uuid.UUID]*memberTokens{}, resolve: func(_ context.Context, got metaMember) (map[uuid.UUID]remotesessions.UpstreamToken, error) {
					calls++
					require.Equal(t, member.chosenClientID, got.chosenClientID)
					return tc.tokens, tc.resolveErr
				}}
				gate := &metaGateContext{agentID: uuid.New(), organizationID: "test-org", memberChoices: choices}
				tokens, err := s.resolveMemberTokens(t.Context(), gate, member)
				if tc.wantErr {
					require.Error(t, err)
					require.Empty(t, tokens)
				} else {
					require.NoError(t, err)
					require.Equal(t, tc.tokens, tokens)
				}
				if tc.unavailable != nil {
					require.ErrorIs(t, err, tc.unavailable)
					require.Zero(t, calls)
				} else {
					require.Equal(t, 1, calls)
				}
			})
		}
	}
}

func agentMemberModeName(mode feature.AuthClientResolverMode) string {
	switch mode {
	case feature.AuthClientResolverEnforced:
		return "enforced"
	case feature.AuthClientResolverShadow:
		return "shadow"
	default:
		return "off"
	}
}

func TestAgentMemberSelectedHeaderAndNoAuth(t *testing.T) {
	t.Parallel()
	for _, clientType := range []string{"", authClientTypeStaticCredential, authClientTypePassThrough} {
		t.Run("no-oauth/"+clientType, func(t *testing.T) {
			t.Parallel()
			s := &Service{}
			member := metaMember{serverID: uuid.New(), backend: metaMemberBackendProxied}
			resolution := authClientResolution{outcome: authClientNoAuth}
			if clientType != "" {
				resolution = authClientResolution{outcome: authClientResolved, clientType: clientType}
			}
			s.applyMemberAuthClient(t.Context(), &member, resolution, nil)
			require.True(t, member.upstreamNoAuth)
			require.False(t, member.chosenClientID.Valid)
			choices := &memberChoices{byServer: map[uuid.UUID]*memberTokens{}, resolve: func(context.Context, metaMember) (map[uuid.UUID]remotesessions.UpstreamToken, error) {
				t.Fatal("header/no-auth member must not resolve OAuth credentials")
				return nil, nil
			}}
			tokens, err := s.resolveMemberTokens(t.Context(), &metaGateContext{agentID: uuid.New(), memberChoices: choices}, member)
			require.NoError(t, err)
			require.Empty(t, tokens)
		})
	}
}

func TestAgentMemberSelectedAttachmentPin(t *testing.T) {
	t.Parallel()
	for _, typed := range []bool{false, true} {
		for _, replaced := range []bool{false, true} {
			name := "legacy"
			if typed {
				name = "typed"
			}
			if replaced {
				name += "/replaced"
			} else {
				name += "/unchanged"
			}
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				checked := uuid.New()
				source := &fakeUpstreamTokens{issuerID: uuid.New(), bound: checked}
				s := &Service{upstreamTokens: source}
				endpoint := &ResolvedMcpEndpoint{ProjectID: uuid.New(), OrganizationID: "test-org", UserSessionIssuerID: uuid.New()}
				agent := uuid.New()
				member := metaMember{serverID: uuid.New(), projectID: endpoint.ProjectID, slug: "pinned-member", backend: metaMemberBackendProxied,
					chosenClientID: uuid.NullUUID{UUID: uuid.New(), Valid: true}, typedAuthClient: typed,
					authClient: &authClientResolution{attachedSession: checked}}
				choices := &memberChoices{enforced: true, byServer: map[uuid.UUID]*memberTokens{}, resolve: func(ctx context.Context, member metaMember) (map[uuid.UUID]remotesessions.UpstreamToken, error) {
					return s.resolveChosenMemberTokens(ctx, endpoint, urn.NewAgentSubject(agent), true, member)
				}}
				// Simulate replacing the attachment after permission checked its
				// exact session, but before the credential lookup reads it.
				if replaced {
					source.bound = uuid.New()
				}
				tokens, err := s.resolveMemberTokens(t.Context(), &metaGateContext{agentID: agent, memberChoices: choices}, member)
				if replaced {
					require.Error(t, err)
					require.Empty(t, tokens)
				} else {
					require.NoError(t, err)
					require.Equal(t, checked, tokens[source.issuerID].RemoteSessionID)
				}
			})
		}
	}
}

func TestAgentMemberResolvedTypeRequiresAttachment(t *testing.T) {
	t.Parallel()
	pool, err := TestInfra.CloneTestDatabase(t, "agent_member_resolved_type")
	require.NoError(t, err)
	f := newAuthClientFixture(t, pool)
	f.oauthClient(authClientTypeRemoteSession)
	f.enforce()
	member := metaMember{serverID: f.serverID, projectID: f.projectID, slug: "member", backend: metaMemberBackendProxied}
	gate := &metaGateContext{agentID: uuid.New(), organizationID: f.orgID}
	// The catalog supplies the personal client type even when the caller has
	// not supplied one. No attachment lookup is available in this fixture.
	connection, err := f.service.authorizeAgentMemberClient(t.Context(), gate, member, f.issuerID, validUUID(f.registration), "")
	require.Error(t, err)
	require.Equal(t, uuid.Nil, connection.attachedSession)
}

// A legacy agent gateway member serves an unattached agent key from its
// validated authorizer's own connection under the agent's own mcp:auth, and
// an explicit attachment still serves first, pinned.
func TestAgentMemberLegacyClientUsesTheAgentKeysAuthorizer(t *testing.T) {
	t.Parallel()
	pool, err := TestInfra.CloneTestDatabase(t, "agent_member_key_authorizer")
	require.NoError(t, err)
	personal := personalAuthGrant()
	connectOnly := []authz.Grant{authz.NewGrant(authz.ScopeMCPConnect, authz.WildcardResource)}
	other := authz.NewGrantWithSelector(authz.ScopeMCPAuth, authz.Selector{
		authz.SelectorKeyResourceKind: authz.ResourceKindMCPAuthClient, authz.SelectorKeyResourceID: authz.WildcardResource,
		authz.SelectorKeyClientType: authz.AuthClientTypeRemoteSession, authz.SelectorKeyConnectionOwner: "user_other",
	})
	cases := []struct {
		name     string
		agent    []authz.Grant
		block    bool
		attached bool
		allowed  bool
	}{
		{"the agent's mcp:auth serves its authorizer's connection without an attachment", []authz.Grant{personal}, false, false, true},
		{"without the agent's mcp:auth it refuses", connectOnly, false, false, false},
		{"a grant for another owner refuses", []authz.Grant{other}, false, false, false},
		{"an owner block refuses", []authz.Grant{personal}, true, false, false},
		{"an explicit attachment serves first, pinned", []authz.Grant{personal}, false, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newAuthClientFixture(t, pool)
			client := f.oauthClient(authClientTypeRemoteSession)
			f.enforce()
			owner := []authz.Grant{personal}
			if tc.block {
				owner = append(owner, authz.NewGrantWithSelector(authz.ScopeMCPBlockedAuth, authz.Selector{
					authz.SelectorKeyResourceKind: authz.ResourceKindMCPAuthClient, authz.SelectorKeyResourceID: client.String(),
					authz.SelectorKeyClientType: authz.AuthClientTypeRemoteSession, authz.SelectorKeyConnectionOwner: "user_owner",
				}))
			}
			f.service.authz = authz.NewEngine(testenv.NewLogger(t), nil, func(context.Context, string) (bool, error) { return false, nil }, workos.NewStubClient(), authz.EngineOpts{
				AdmitPrincipalCredential: func(context.Context, *pgxpool.Pool) (authz.PrincipalCredentialAdmission, error) {
					return authz.PrincipalCredentialAdmission{
						OwnerUserID: "user_owner", Credential: tc.agent, Agent: tc.agent, Owner: owner,
						Authorizer: authz.RepresentedUserPolicy{UserID: "user_owner", Policy: []authz.Grant{personal}}, LegacyAuthCeiling: false,
					}, nil
				},
			})
			f.service.authClientPermitter = mandatoryPermitter{engine: f.service.authz}
			session := uuid.New()
			f.service.attachedConnections = &fakeAttachedConnections{conn: remotesessions.AttachedConnection{RemoteSessionID: session, OwnerUserID: "user_owner"}, attached: tc.attached, err: nil, asked: nil}
			source := &fakeUpstreamTokens{issuerID: uuid.New(), bound: session}
			f.service.upstreamTokens = source
			agentID := uuid.New()
			ctx := admittedAgentContext(t, f.orgID, agentID)
			gate := &metaGateContext{agentID: agentID, organizationID: f.orgID}
			member := metaMember{serverID: f.serverID, projectID: f.projectID, slug: "member", backend: metaMemberBackendProxied}

			tokens, err := f.service.agentMemberClientTokens(ctx, gate, member, f.issuerID, f.registration, "")
			if !tc.allowed {
				require.Error(t, err)
				require.Empty(t, source.subjects, "a refused member reads no credential")
				return
			}
			require.NoError(t, err)
			require.Equal(t, session, tokens[source.issuerID].RemoteSessionID)
			require.Len(t, source.subjects, 1)
			if tc.attached {
				require.Equal(t, urn.NewAgentSubject(agentID), source.subjects[0], "the attachment serves its pinned connection")
			} else {
				require.Equal(t, urn.NewUserSubject("user_owner"), source.subjects[0], "the authorizer's own connection serves")
			}
		})
	}
}

// A handback to the upstream client choice reads the connection the permit
// was decided for: the unattached agent key's authorizer, directly and for a
// gateway member, typed or not; any other caller keeps its own subject.
func TestAgentKeyHandbackReadsTheAuthorizersConnection(t *testing.T) {
	t.Parallel()
	for _, owner := range []string{"user_owner", ""} {
		for _, typed := range []bool{false, true} {
			t.Run(fmt.Sprintf("owner=%q/typed=%v", owner, typed), func(t *testing.T) {
				t.Parallel()
				agent := urn.NewAgentSubject(uuid.New())
				want := agent
				if owner != "" {
					want = urn.NewUserSubject(owner)
				}
				source := &fakeUpstreamTokens{issuerID: uuid.New(), bound: uuid.New()}
				s := &Service{upstreamTokens: source}
				endpoint := &ResolvedMcpEndpoint{ProjectID: uuid.New(), OrganizationID: "test-org", UserSessionIssuerID: uuid.New()}
				member := metaMember{serverID: uuid.New(), projectID: endpoint.ProjectID, slug: "member", backend: metaMemberBackendProxied,
					chosenClientID: uuid.NullUUID{UUID: uuid.New(), Valid: true}, typedAuthClient: typed,
					authClient: &authClientResolution{authorizerOwner: owner}}
				_, err := s.resolveChosenMemberTokens(t.Context(), endpoint, agent, true, member)
				require.NoError(t, err)
				authentication := &issuerGateAuthentication{endpoint: endpoint, subject: agent, chosenClient: uuid.NullUUID{UUID: uuid.New(), Valid: true}, authorizerOwner: owner}
				if typed {
					authentication.authClient = &authClientResolution{authorizerOwner: owner}
				}
				_, err = s.resolveSelectedAccessTokens(t.Context(), authentication, true)
				require.NoError(t, err)
				require.Equal(t, []urn.SessionSubject{want, want}, source.subjects)
			})
		}
	}
}

func TestAgentMemberRoutingPreservesAmbiguity(t *testing.T) {
	t.Parallel()
	pool, err := TestInfra.CloneTestDatabase(t, "agent_member_route_ambiguity")
	require.NoError(t, err)
	f := newAuthClientFixture(t, pool)
	resource := "https://mcp.example.com/" + f.remoteID.String()[:8]
	member := metaMember{serverID: f.serverID, projectID: f.projectID, slug: "member", backend: metaMemberBackendProxied, remoteServerID: validUUID(f.remoteID), chosenClientID: validUUID(f.registration)}
	choices := &memberChoices{byServer: map[uuid.UUID]*memberTokens{}, resolve: func(context.Context, metaMember) (map[uuid.UUID]remotesessions.UpstreamToken, error) {
		return map[uuid.UUID]remotesessions.UpstreamToken{
			uuid.New(): {Token: "first", Resource: resource},
			uuid.New(): {Token: "second", Resource: resource},
		}, nil
	}}
	_, backend, err := f.service.routeMetaMember(t.Context(), f.service.logger, metaGateContext{agentID: uuid.New(), organizationID: f.orgID, memberChoices: choices}, member, "")
	require.Equal(t, "remote", backend)
	require.ErrorIs(t, err, errAmbiguousMemberCredential)
}
