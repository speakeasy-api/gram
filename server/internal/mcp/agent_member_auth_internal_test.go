package mcp

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// These tests deliberately provide no database or challenge manager. A selected
// proxied member must never fall through to legacy issuer resolution.
func TestAgentMemberSelectedResolution(t *testing.T) {
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
	for _, clientType := range []string{"", authClientTypeStaticCredential, authClientTypePassThrough} {
		t.Run("no-oauth/"+clientType, func(t *testing.T) {
			s := &Service{}
			member := metaMember{serverID: uuid.New(), backend: metaMemberBackendProxied, upstreamNoAuth: true}
			if clientType != "" {
				member.authClient = &authClientResolution{outcome: authClientResolved, clientType: clientType}
			}
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
