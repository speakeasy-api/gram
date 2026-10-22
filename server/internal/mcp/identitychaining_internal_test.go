package mcp

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/aivendors"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpidentity"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/identitychaining"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const chainingTestOrg = "org-chaining"

// recordingChainer answers every acquisition with outcome and records requests.
type recordingChainer struct {
	mu       sync.Mutex
	outcome  identitychaining.Outcome
	governs  bool
	requests []identitychaining.Request
}

func (c *recordingChainer) Governs(context.Context, identitychaining.Request) bool {
	return c.governs
}

func (c *recordingChainer) Serves(context.Context, identitychaining.Request) (uuid.UUID, bool) {
	return uuid.Nil, false
}

func (c *recordingChainer) Configured(context.Context, string, uuid.UUID, uuid.UUID) bool {
	return false
}

func (c *recordingChainer) HasUsableCredential(context.Context, identitychaining.Request) bool {
	return false
}

func (c *recordingChainer) RefreshDecision(context.Context, identitychaining.Request) identitychaining.RefreshVerdict {
	return identitychaining.RefreshVerdict{Decision: identitychaining.RefreshNotChained, RemoteSessionIssuerID: uuid.Nil, Provider: "", Reason: ""}
}

func (c *recordingChainer) Acquire(_ context.Context, req identitychaining.Request) (identitychaining.Token, identitychaining.Outcome) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.requests = append(c.requests, req)
	return identitychaining.Token{}, c.outcome
}

func chainingService(t *testing.T, chainer identityChainer) *Service {
	t.Helper()
	return &Service{
		aiCatalog:       aivendors.NewStore(aivendors.NewSnapshot(aivendors.Products())),
		identityChainer: chainer}
}

func humanChainingContext(t *testing.T, userID, organizationID string) context.Context {
	t.Helper()
	ctx := mcpidentity.NewValidatorBoundary().StampValidatedSession(t.Context(), validatedSessionProof(t, urn.NewUserSubject(userID)))
	return contextvalues.SetAuthContext(ctx, &contextvalues.AuthContext{UserID: userID, ActiveOrganizationID: organizationID, OrganizationSlug: "chaining"})
}

func TestIdentityChainingRequest_OnlyValidatedHumans(t *testing.T) {
	t.Parallel()
	project, issuer := uuid.New(), uuid.New()
	for _, tc := range []struct {
		name    string
		ctx     func(t *testing.T) context.Context
		chainer bool
		issuer  uuid.UUID
		want    bool
	}{
		{"validated human", func(t *testing.T) context.Context {
			t.Helper()
			return humanChainingContext(t, "user-1", chainingTestOrg)
		}, true, issuer, true},
		{"no executor", func(t *testing.T) context.Context {
			t.Helper()
			return humanChainingContext(t, "user-1", chainingTestOrg)
		}, false, issuer, false},
		{"ungated endpoint", func(t *testing.T) context.Context {
			t.Helper()
			return humanChainingContext(t, "user-1", chainingTestOrg)
		}, true, uuid.Nil, false},
		{"another organization", func(t *testing.T) context.Context { t.Helper(); return humanChainingContext(t, "user-1", "org-other") }, true, issuer, false},
		{"assistant acting for a user", func(t *testing.T) context.Context {
			t.Helper()
			ctx := mcpidentity.NewValidatorBoundary().StampAssistant(t.Context())
			return contextvalues.SetAuthContext(ctx, &contextvalues.AuthContext{UserID: "user-1", ActiveOrganizationID: chainingTestOrg, OrganizationSlug: "chaining"})
		}, true, issuer, false},
		{"api key", func(t *testing.T) context.Context {
			t.Helper()
			ctx := mcpidentity.NewValidatorBoundary().StampAPIKey(t.Context(), "api-key-1")
			return contextvalues.SetAuthContext(ctx, &contextvalues.AuthContext{UserID: "user-1", ActiveOrganizationID: chainingTestOrg, OrganizationSlug: "chaining"})
		}, true, issuer, false},
		{"auth context for another user", func(t *testing.T) context.Context {
			t.Helper()
			ctx := mcpidentity.NewValidatorBoundary().StampValidatedSession(t.Context(), validatedSessionProof(t, urn.NewUserSubject("user-1")))
			return contextvalues.SetAuthContext(ctx, &contextvalues.AuthContext{UserID: "user-2", ActiveOrganizationID: chainingTestOrg, OrganizationSlug: "chaining"})
		}, true, issuer, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var chainer identityChainer
			if tc.chainer {
				chainer = &recordingChainer{}
			}
			s := chainingService(t, chainer)
			ctx := tc.ctx(t)
			chosen := uuid.NullUUID{UUID: uuid.New(), Valid: true}
			req, ok := s.identityChainingRequest(ctx, chainingTestOrg, project, tc.issuer, "https://upstream.example.test/mcp", false, uuid.NullUUID{}, chosen)
			require.Equal(t, tc.want, ok)
			if tc.want {
				identity, hasIdentity := mcpidentity.FromContext(ctx)
				require.True(t, hasIdentity)
				require.NotEmpty(t, req.UserSessionJTI)
				require.Equal(t, identitychaining.Request{OrganizationID: chainingTestOrg, ProjectID: project, UserSessionIssuerID: issuer, UserID: "user-1", UserSessionJTI: identity.SessionJTI(), UpstreamResource: "https://upstream.example.test/mcp", RemoteSessionIssuerID: uuid.NullUUID{}, RemoteSessionClientID: chosen}, req)
			}
		})
	}
}

func TestUpstreamTokenPresent(t *testing.T) {
	t.Parallel()
	issuer := uuid.New()
	tokens := map[uuid.UUID]remotesessions.UpstreamToken{
		issuer: {Token: "t", Resource: "https://upstream.example.test/mcp/", RemoteSessionClientID: uuid.New(), RemoteSessionID: uuid.New()},
	}
	require.True(t, upstreamTokenPresent(tokens, "https://upstream.example.test/mcp", false, uuid.NullUUID{}))
	require.False(t, upstreamTokenPresent(tokens, "https://other.example.test/mcp", false, uuid.NullUUID{}))
	require.False(t, upstreamTokenPresent(nil, "https://upstream.example.test/mcp", false, uuid.NullUUID{}))
	require.True(t, upstreamTokenPresent(tokens, "https://upstream.example.test/mcp", true, uuid.NullUUID{UUID: issuer, Valid: true}))
	require.False(t, upstreamTokenPresent(tokens, "https://upstream.example.test/mcp", true, uuid.NullUUID{UUID: uuid.New(), Valid: true}))
	require.False(t, upstreamTokenPresent(tokens, "https://upstream.example.test/mcp", true, uuid.NullUUID{}), "a tunnel without a derived issuer holds no token")
}

func TestIdentityChainingError(t *testing.T) {
	t.Parallel()
	retryable := identityChainingError(identitychaining.Outcome{Stage: identitychaining.StageExchange, Reason: identitychaining.ReasonTransientFailure, Confidence: identitychaining.ConfidenceInferred, Retryable: true, Cached: false, Provider: ""})
	var shareable *oops.ShareableError
	require.ErrorAs(t, retryable, &shareable)
	require.Equal(t, oops.CodeUnavailable, shareable.Code)
	require.Contains(t, retryable.Error(), "transient_failure")

	denied := identityChainingError(identitychaining.Outcome{Stage: identitychaining.StageExchange, Reason: identitychaining.ReasonInvalidTarget, Confidence: identitychaining.ConfidenceInferred, Retryable: false, Cached: false, Provider: ""})
	require.ErrorAs(t, denied, &shareable)
	require.Equal(t, oops.CodeForbidden, shareable.Code, "an explicit binding never falls back to interactive authorization")
}

func TestMetaMemberChainer(t *testing.T) {
	t.Parallel()
	project, issuer := uuid.New(), uuid.New()
	gate := &metaGateContext{projectID: project, organizationID: chainingTestOrg, userSessionIssuerID: issuer}
	member := metaMember{slug: "docs", projectID: project, chosenClientID: uuid.NullUUID{UUID: uuid.New(), Valid: true}}

	t.Run("member outside the gateway project", func(t *testing.T) {
		t.Parallel()
		s := chainingService(t, &recordingChainer{})
		other := member
		other.projectID = uuid.New()
		require.Nil(t, s.metaMemberChainer(gate, other))
	})
	t.Run("ungated gateway", func(t *testing.T) {
		t.Parallel()
		s := chainingService(t, &recordingChainer{})
		ungated := *gate
		ungated.userSessionIssuerID = uuid.Nil
		require.Nil(t, s.metaMemberChainer(&ungated, member))
	})
	for _, tc := range []struct {
		name    string
		outcome identitychaining.Outcome
		wantErr bool
	}{
		{"not applicable stays anonymous", identitychaining.Outcome{Stage: identitychaining.StageSelection, Reason: identitychaining.ReasonNotApplicable, Confidence: identitychaining.ConfidenceVerified, Retryable: false, Cached: false, Provider: ""}, false},
		{"reauthentication is member scoped", identitychaining.Outcome{Stage: identitychaining.StageDelegation, Reason: identitychaining.ReasonReauthenticationRequired, Confidence: identitychaining.ConfidenceVerified, Retryable: false, Cached: false, Provider: ""}, true},
		{"provider rejection is member scoped", identitychaining.Outcome{Stage: identitychaining.StageRedemption, Reason: identitychaining.ReasonInvalidGrant, Confidence: identitychaining.ConfidenceUnknown, Retryable: false, Cached: false, Provider: ""}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			chainer := &recordingChainer{outcome: tc.outcome}
			s := chainingService(t, chainer)
			chain := s.metaMemberChainer(gate, member)
			require.NotNil(t, chain)
			token, err := chain(humanChainingContext(t, "user-1", chainingTestOrg), "https://docs.example.test/mcp")
			require.Empty(t, token)
			if tc.wantErr {
				var memberErr *metaMemberError
				require.ErrorAs(t, err, &memberErr, "chaining failures surface as member-scoped tool errors")
				require.Contains(t, memberErr.message, `"docs"`)
			} else {
				require.NoError(t, err)
			}
			require.Len(t, chainer.requests, 1)
			require.Equal(t, project, chainer.requests[0].ProjectID)
			require.Equal(t, issuer, chainer.requests[0].UserSessionIssuerID)
		})
	}
}

func TestIdentityChainingRequest_TunnelRoutesByOwnIssuer(t *testing.T) {
	t.Parallel()
	project, issuer, tunnelIssuer := uuid.New(), uuid.New(), uuid.New()
	s := chainingService(t, &recordingChainer{})
	ctx := humanChainingContext(t, "user-1", chainingTestOrg)

	chosen := uuid.NullUUID{UUID: uuid.New(), Valid: true}
	req, ok := s.identityChainingRequest(ctx, chainingTestOrg, project, issuer, "https://tunnel.example.test/mcp", true, uuid.NullUUID{UUID: tunnelIssuer, Valid: true}, chosen)
	require.True(t, ok)
	require.Equal(t, uuid.NullUUID{UUID: tunnelIssuer, Valid: true}, req.RemoteSessionIssuerID, "a tunnel may only select a binding for its own issuer")

	_, ok = s.identityChainingRequest(ctx, chainingTestOrg, project, issuer, "https://tunnel.example.test/mcp", true, uuid.NullUUID{}, chosen)
	require.False(t, ok, "a tunnel without a derived issuer calls anonymously and never chains")

	req, ok = s.identityChainingRequest(ctx, chainingTestOrg, project, issuer, "https://remote.example.test/mcp", false, uuid.NullUUID{UUID: tunnelIssuer, Valid: true}, chosen)
	require.True(t, ok)
	require.False(t, req.RemoteSessionIssuerID.Valid, "a remote backend routes by the URL it dials")
}

// A session credential chains only through the client it chose, so one
// without a choice, a no-auth choice included, never chains, even where a
// binding for another client of the issuer would serve the upstream.
func TestIdentityChainingRequest_RequiresAChosenClient(t *testing.T) {
	t.Parallel()
	project, issuer := uuid.New(), uuid.New()
	s := chainingService(t, &recordingChainer{})
	ctx := humanChainingContext(t, "user-1", chainingTestOrg)

	_, ok := s.identityChainingRequest(ctx, chainingTestOrg, project, issuer, "https://remote.example.test/mcp", false, uuid.NullUUID{}, uuid.NullUUID{})
	require.False(t, ok)

	member := metaMember{slug: "remote", projectID: project}
	gate := &metaGateContext{projectID: project, organizationID: chainingTestOrg, userSessionIssuerID: issuer}
	chainer := &recordingChainer{}
	token, err := chainingService(t, chainer).metaMemberChainer(gate, member)(ctx, "https://remote.example.test/mcp")
	require.NoError(t, err)
	require.Empty(t, token)
	require.Empty(t, chainer.requests, "a member without a chosen client never asks the chainer")
}

func TestMetaMemberChainer_TunneledMemberRoutesByOwnIssuer(t *testing.T) {
	t.Parallel()
	project, issuer, tunnelIssuer := uuid.New(), uuid.New(), uuid.New()
	gate := &metaGateContext{projectID: project, organizationID: chainingTestOrg, userSessionIssuerID: issuer}
	member := metaMember{slug: "tunnel", projectID: project, tunneledServerID: uuid.NullUUID{UUID: uuid.New(), Valid: true}, remoteSessionIssuerID: uuid.NullUUID{UUID: tunnelIssuer, Valid: true}, chosenClientID: uuid.NullUUID{UUID: uuid.New(), Valid: true}}
	chainer := &recordingChainer{outcome: identitychaining.Outcome{Stage: identitychaining.StageSelection, Reason: identitychaining.ReasonNotApplicable, Confidence: identitychaining.ConfidenceVerified, Retryable: false, Cached: false, Provider: ""}}
	s := chainingService(t, chainer)

	_, err := s.metaMemberChainer(gate, member)(humanChainingContext(t, "user-1", chainingTestOrg), "https://victim.example.test/mcp")
	require.NoError(t, err)
	require.Len(t, chainer.requests, 1)
	require.Equal(t, uuid.NullUUID{UUID: tunnelIssuer, Valid: true}, chainer.requests[0].RemoteSessionIssuerID, "a tunnel claiming a sibling's resource only selects bindings for its own issuer")

	unidentified := member
	unidentified.remoteSessionIssuerID = uuid.NullUUID{}
	token, err := s.metaMemberChainer(gate, unidentified)(humanChainingContext(t, "user-1", chainingTestOrg), "https://victim.example.test/mcp")
	require.NoError(t, err)
	require.Empty(t, token)
	require.Len(t, chainer.requests, 1, "a tunnel without a derived issuer never chains")
}

func TestUpstreamTokenPresent_SelfCredential(t *testing.T) {
	t.Parallel()

	issuer := uuid.New()
	tokens := map[uuid.UUID]remotesessions.UpstreamToken{
		issuer: {Token: "self-token", CredentialOwner: remotesessions.CredentialOwnerSelf},
	}
	require.True(t, upstreamTokenPresent(tokens, "https://upstream.example.test/mcp", false, uuid.NullUUID{UUID: issuer, Valid: true}))
	require.False(t, upstreamTokenPresent(tokens, "https://upstream.example.test/mcp", false, uuid.NullUUID{UUID: uuid.New(), Valid: true}))

	unavailable := map[uuid.UUID]remotesessions.UpstreamToken{
		issuer: {CredentialOwner: remotesessions.CredentialOwnerSelf, ClientCredentialErr: remotesessions.ErrClientCredentialMisconfigured},
	}
	require.True(t, upstreamTokenPresent(unavailable, "https://upstream.example.test/mcp", false, uuid.NullUUID{UUID: issuer, Valid: true}), "routing must report the failure rather than chain around it")

	audienceBound := map[uuid.UUID]remotesessions.UpstreamToken{
		issuer: {Token: "self-token", Resource: "https://upstream.example.test/mcp/", CredentialOwner: remotesessions.CredentialOwnerSelf},
	}
	require.True(t, upstreamTokenPresent(audienceBound, "https://upstream.example.test/mcp", false, uuid.NullUUID{UUID: issuer, Valid: true}), "a self credential requested for this upstream is present")
	require.False(t, upstreamTokenPresent(audienceBound, "https://sibling.example.test/mcp", false, uuid.NullUUID{UUID: issuer, Valid: true}), "a self credential requested for a sibling upstream is not")
}

func (g *recordingChainer) CheckConsent(ctx context.Context, req identitychaining.Request, _ uuid.UUID) identitychaining.Outcome {
	_, outcome := g.Acquire(ctx, req)
	return outcome
}

// A credential's chosen client constrains chaining as it does interactive
// resolution, on a direct backend and on every gateway member.
func TestIdentityChainingCarriesTheChosenClient(t *testing.T) {
	t.Parallel()
	project, issuer, chosen := uuid.New(), uuid.New(), uuid.New()
	chainer := &recordingChainer{outcome: identitychaining.Outcome{Stage: identitychaining.StageSelection, Reason: identitychaining.ReasonNotApplicable, Confidence: identitychaining.ConfidenceVerified, Retryable: false, Cached: false, Provider: ""}}
	s := chainingService(t, chainer)
	ctx := humanChainingContext(t, "user-1", chainingTestOrg)

	req, ok := s.identityChainingRequest(ctx, chainingTestOrg, project, issuer, "https://remote.example.test/mcp", false, uuid.NullUUID{}, uuid.NullUUID{UUID: chosen, Valid: true})
	require.True(t, ok)
	require.Equal(t, uuid.NullUUID{UUID: chosen, Valid: true}, req.RemoteSessionClientID)

	gate := &metaGateContext{projectID: project, organizationID: chainingTestOrg, userSessionIssuerID: issuer}
	member := metaMember{slug: "remote", projectID: project, chosenClientID: uuid.NullUUID{UUID: chosen, Valid: true}}
	_, err := s.metaMemberChainer(gate, member)(ctx, "https://remote.example.test/mcp")
	require.NoError(t, err)
	require.Len(t, chainer.requests, 1)
	require.Equal(t, uuid.NullUUID{UUID: chosen, Valid: true}, chainer.requests[0].RemoteSessionClientID)
}

func (c *recordingChainer) RetireConsentCredentials(context.Context, string, uuid.UUID, uuid.UUID, string, uuid.UUID) error {
	return nil
}
