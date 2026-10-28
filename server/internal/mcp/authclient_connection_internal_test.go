package mcp

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// fakeAttachedConnections answers the attachment lookup with a fixed
// connection and records what it was asked.
type fakeAttachedConnections struct {
	conn     remotesessions.AttachedConnection
	attached bool
	err      error
	asked    []remotesessions.AuthClientTokenRequest
}

func (f *fakeAttachedConnections) AuthClientAttachedConnection(_ context.Context, req remotesessions.AuthClientTokenRequest) (remotesessions.AttachedConnection, bool, error) {
	f.asked = append(f.asked, req)
	return f.conn, f.attached, f.err
}

// enforcedShadowService makes f decide with the mandatory mcp:auth check and
// record the mandatory shadow, as an enforcing organization does.
func enforcedShadowService(t *testing.T, f *authClientFixture) *sdkmetric.ManualReader {
	t.Helper()
	flags := new(feature.InMemory)
	flags.SetFlag(feature.FlagMandatoryAuthzShadow, f.orgID, true)
	reader := sdkmetric.NewManualReader()
	f.service.logger = testenv.NewLogger(t)
	f.service.metrics = mcpmetrics.NewMetrics(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"), testenv.NewLogger(t))
	f.service.features = flags
	f.service.mandatoryShadowLog = newMandatoryShadowLog()
	f.service.authz = authz.NewEngine(testenv.NewLogger(t), nil, func(context.Context, string) (bool, error) { return false, nil }, workos.NewStubClient())
	f.service.authClientPermitter = mandatoryPermitter{engine: f.service.authz}
	return reader
}

// authClientShadowOutcomes counts recorded auth client shadow decisions by
// outcome and reason.
func authClientShadowOutcomes(t *testing.T, reader *sdkmetric.ManualReader) map[string]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))
	out := map[string]int64{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "mcp.authz.mandatory_shadow" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			for _, dp := range sum.DataPoints {
				surface, _ := dp.Attributes.Value("gram.authz.mandatory.surface")
				if surface.AsString() != mandatorySurfaceAuthClient {
					continue
				}
				outcome, _ := dp.Attributes.Value("gram.authz.mandatory.outcome")
				reason, _ := dp.Attributes.Value("gram.authz.mandatory.reason")
				out[outcome.AsString()+"/"+reason.AsString()] += dp.Value
			}
		}
	}
	return out
}

func personalAuthGrant() authz.Grant {
	return authz.NewGrantWithSelector(authz.ScopeMCPAuth, authz.Selector{
		authz.SelectorKeyResourceKind: authz.ResourceKindMCPAuthClient, authz.SelectorKeyResourceID: authz.WildcardResource,
		authz.SelectorKeyClientType: authz.AuthClientTypeRemoteSession, authz.SelectorKeyConnectionOwner: authz.SelectorValueRepresentedUser,
	})
}

// An agent is served only by the connection a user attached to it, so the
// mandatory check and its shadow both name that connection's owner, read from
// the exact binding, and the represented-user rule still decides.
func TestAttachedAgentAuthClientChecksTheAttachedConnectionOwner(t *testing.T) {
	t.Parallel()

	pool, err := TestInfra.CloneTestDatabase(t, "mcp_auth_client_attached_owner")
	require.NoError(t, err)

	setup := func(t *testing.T, lookup *fakeAttachedConnections) (*authClientFixture, *upstreamSelector, *sdkmetric.ManualReader, urn.SessionSubject) {
		t.Helper()
		f := newAuthClientFixture(t, pool)
		client := f.oauthClient(authClientTypeRemoteSession)
		selector := f.choose(validUUID(client))
		selector.userSessionIssuerID = uuid.New()
		reader := enforcedShadowService(t, f)
		f.service.attachedConnections = lookup
		return f, selector, reader, urn.NewAgentSubject(uuid.New())
	}

	t.Run("the represented user's attached connection is permitted and pinned", func(t *testing.T) {
		t.Parallel()
		session := uuid.New()
		lookup := &fakeAttachedConnections{conn: remotesessions.AttachedConnection{RemoteSessionID: session, OwnerUserID: "user_1"}, attached: true, err: nil, asked: nil}
		f, selector, reader, agent := setup(t, lookup)
		res, err := f.service.resolveAuthClientForServing(sessionContext(t, f.orgID, personalAuthGrant()), selector, f.serverID, f.projectID, agent)
		require.NoError(t, err)
		require.Equal(t, authClientResolved, res.outcome)
		require.Equal(t, session, res.attachedSession, "tokens must come from the connection the check named")
		require.Len(t, lookup.asked, 1, "the permit and its shadow share one lookup")
		require.Equal(t, remotesessions.AuthClientTokenRequest{ProjectID: f.projectID, OrganizationID: f.orgID, UserSessionIssuerID: selector.userSessionIssuerID, Subject: agent, RegistrationID: f.registration, SkipUnusable: false}, lookup.asked[0], "the lookup is scoped to the exact tenant, sign-in, caller and registration")
		require.Equal(t, map[string]int64{"allow/none": 1}, authClientShadowOutcomes(t, reader), "the shadow records the check instead of skipping the agent")
	})

	t.Run("another user's attached connection is never permitted", func(t *testing.T) {
		t.Parallel()
		lookup := &fakeAttachedConnections{conn: remotesessions.AttachedConnection{RemoteSessionID: uuid.New(), OwnerUserID: "user_2"}, attached: true, err: nil, asked: nil}
		f, selector, reader, agent := setup(t, lookup)
		res, err := f.service.resolveAuthClientForServing(sessionContext(t, f.orgID, personalAuthGrant(), authz.NewGrant(authz.ScopeMCPAuth, authz.WildcardResource)), selector, f.serverID, f.projectID, agent)
		require.NoError(t, err)
		require.Equal(t, authClientNoAccess, res.outcome, "ownership filters grants and never grants, even against a wildcard")
		require.Equal(t, authClientReasonNotPermitted, res.reason)
		require.Equal(t, map[string]int64{"deny/not_granted": 1}, authClientShadowOutcomes(t, reader))
	})

	t.Run("a removed grant refuses the owner's attached connection", func(t *testing.T) {
		t.Parallel()
		lookup := &fakeAttachedConnections{conn: remotesessions.AttachedConnection{RemoteSessionID: uuid.New(), OwnerUserID: "user_1"}, attached: true, err: nil, asked: nil}
		f, selector, reader, agent := setup(t, lookup)
		res, err := f.service.resolveAuthClientForServing(sessionContext(t, f.orgID), selector, f.serverID, f.projectID, agent)
		require.NoError(t, err)
		require.Equal(t, authClientNoAccess, res.outcome, "a binding is not a grant")
		require.Equal(t, map[string]int64{"deny/no_policy": 1}, authClientShadowOutcomes(t, reader))
	})

	t.Run("no usable attachment fails closed", func(t *testing.T) {
		t.Parallel()
		lookup := &fakeAttachedConnections{conn: remotesessions.AttachedConnection{RemoteSessionID: uuid.Nil, OwnerUserID: ""}, attached: false, err: nil, asked: nil}
		f, selector, reader, agent := setup(t, lookup)
		_, err := f.service.resolveAuthClientForServing(sessionContext(t, f.orgID, personalAuthGrant()), selector, f.serverID, f.projectID, agent)
		require.ErrorContains(t, err, "missing connection_owner", "an ownerless personal check is refused as invalid, as before, never allowed")
		require.Empty(t, authClientShadowOutcomes(t, reader))

		// Before enforcement, the connect-equivalent rule still decides and
		// the shadow keeps skipping a check it has no owner for.
		f.service.authClientPermitter = connectPermitter{}
		res, err := f.service.resolveAuthClientForServing(sessionContext(t, f.orgID, personalAuthGrant()), selector, f.serverID, f.projectID, agent)
		require.NoError(t, err)
		require.Equal(t, authClientResolved, res.outcome)
		require.Equal(t, uuid.Nil, res.attachedSession)
		require.Equal(t, map[string]int64{"skipped/" + mandatoryReasonNoConnectionOwner: 1}, authClientShadowOutcomes(t, reader), "without an owner the shadow keeps skipping")
	})

	t.Run("a refused attachment lookup fails closed", func(t *testing.T) {
		t.Parallel()
		lookup := &fakeAttachedConnections{conn: remotesessions.AttachedConnection{RemoteSessionID: uuid.New(), OwnerUserID: "user_1"}, attached: true, err: remotesessions.ErrInvalidAuthorizationRequest, asked: nil}
		f, selector, _, agent := setup(t, lookup)
		_, err := f.service.resolveAuthClientForServing(sessionContext(t, f.orgID, personalAuthGrant()), selector, f.serverID, f.projectID, agent)
		require.ErrorContains(t, err, "missing connection_owner", "a refused lookup names no owner, so nothing is allowed")
	})

	t.Run("a failed attachment lookup is an error, never an allow", func(t *testing.T) {
		t.Parallel()
		lookup := &fakeAttachedConnections{conn: remotesessions.AttachedConnection{RemoteSessionID: uuid.New(), OwnerUserID: "user_1"}, attached: true, err: errors.New("database unavailable"), asked: nil}
		f, selector, _, agent := setup(t, lookup)
		_, err := f.service.resolveAuthClientForServing(sessionContext(t, f.orgID, personalAuthGrant()), selector, f.serverID, f.projectID, agent)
		require.Error(t, err)
	})

	t.Run("a user's own connection needs no attachment lookup", func(t *testing.T) {
		t.Parallel()
		lookup := &fakeAttachedConnections{conn: remotesessions.AttachedConnection{RemoteSessionID: uuid.New(), OwnerUserID: "user_2"}, attached: true, err: nil, asked: nil}
		f, selector, _, _ := setup(t, lookup)
		res, err := f.service.resolveAuthClientForServing(sessionContext(t, f.orgID, personalAuthGrant()), selector, f.serverID, f.projectID, urn.NewUserSubject("user_1"))
		require.NoError(t, err)
		require.Equal(t, authClientResolved, res.outcome)
		require.Equal(t, uuid.Nil, res.attachedSession)
		require.Empty(t, lookup.asked)
	})
}

func TestPinAttachedSessionServesOnlyThePermittedConnection(t *testing.T) {
	t.Parallel()

	permitted, moved := uuid.New(), uuid.New()
	issuerA, issuerB := uuid.New(), uuid.New()
	tokens := map[uuid.UUID]remotesessions.UpstreamToken{
		issuerA: {Token: "permitted", RemoteSessionID: permitted},
		issuerB: {Token: "moved", RemoteSessionID: moved},
	}
	res := &authClientResolution{attachedSession: permitted}

	pinned, err := pinAttachedSession(tokens, res, true)
	require.NoError(t, err)
	require.Equal(t, map[uuid.UUID]remotesessions.UpstreamToken{issuerA: tokens[issuerA]}, pinned)

	_, err = pinAttachedSession(tokens, res, false)
	require.ErrorIs(t, err, remotesessions.ErrNoValidToken, "a binding that moved after the check is a missing token, never an anonymous call")

	unpinned, err := pinAttachedSession(tokens, &authClientResolution{}, false)
	require.NoError(t, err)
	require.Equal(t, tokens, unpinned, "a user's own connection is not pinned")
	unpinned, err = pinAttachedSession(tokens, nil, false)
	require.NoError(t, err)
	require.Equal(t, tokens, unpinned)
}

// A permit request built without resolving its owner, as a direct caller of
// the permitter builds one, still checks a user's own connection, and never
// assumes an owner for anyone else.
func TestMandatoryPermitterKeepsAUsersOwnConnectionWhenUnresolved(t *testing.T) {
	t.Parallel()

	permitter := mandatoryPermitter{engine: authz.NewEngine(testenv.NewLogger(t), nil, func(context.Context, string) (bool, error) { return false, nil }, workos.NewStubClient())}
	ctx := sessionContext(t, shadowOrg, personalAuthGrant())
	req := authClientPermitRequest{
		OrganizationID: shadowOrg, ProjectID: uuid.New(), ServerID: uuid.New(), ClientID: uuid.New(), ClientType: authClientTypeRemoteSession,
		RegistrationID: validUUID(uuid.New()), CredentialKind: "", CredentialID: uuid.Nil, Subject: urn.NewUserSubject("user_1"), UserSessionIssuerID: uuid.Nil, ConnectionOwner: "",
	}
	allowed, err := permitter.Permitted(ctx, req)
	require.NoError(t, err)
	require.True(t, allowed, "a user's own connection is theirs")

	req.Subject = urn.NewAgentSubject(uuid.New())
	allowed, err = permitter.Permitted(ctx, req)
	require.ErrorContains(t, err, "missing connection_owner")
	require.False(t, allowed, "an agent's owner is never assumed")

	req.ConnectionOwner = "user_2"
	req.Subject = urn.NewUserSubject("user_1")
	allowed, err = permitter.Permitted(ctx, req)
	require.NoError(t, err)
	require.False(t, allowed, "a resolved owner wins over the subject")
}

// admittedAgentContext is an agent API key caller as authentication admits it:
// principal authorization mode, the agent as the actor, and the user it acts
// for as the credential's authorizer. The engine re-admits it through admit.
func admittedAgentContext(t *testing.T, orgID string, agentID uuid.UUID) context.Context {
	t.Helper()
	return contextvalues.WithPrincipalAPIKeyAuthorization(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: orgID, APIKeyID: uuid.NewString()},
		urn.NewPrincipal(urn.PrincipalTypeAgent, agentID.String()), contextvalues.PrincipalCredential{AuthorizerUserID: "user_owner"})
}

// An agent admitted through its own credential path is checked against the
// owner of the exact connection attached to it, while the user it represents
// stays its authorizer: that user's attachment serves only under every
// admitted policy, and an owner block or another lender's connection refuses.
func TestAdmittedAgentAuthClientUsesTheAttachedConnectionOwner(t *testing.T) {
	t.Parallel()

	pool, err := TestInfra.CloneTestDatabase(t, "mcp_auth_client_admitted_agent")
	require.NoError(t, err)

	personal := personalAuthGrant()
	cases := []struct {
		name       string
		owner      string
		ownerGrant func(client uuid.UUID) []authz.Grant
		authorizer []authz.Grant
		outcome    authClientOutcome
		shadow     string
	}{
		{"the represented user's attachment serves", "user_owner", func(uuid.UUID) []authz.Grant { return []authz.Grant{personal} }, []authz.Grant{personal}, authClientResolved, "allow"},
		{"an owner block on that connection refuses", "user_owner", func(client uuid.UUID) []authz.Grant {
			return []authz.Grant{personal, authz.NewGrantWithSelector(authz.ScopeMCPBlockedAuth, authz.Selector{
				authz.SelectorKeyResourceKind: authz.ResourceKindMCPAuthClient, authz.SelectorKeyResourceID: client.String(),
				authz.SelectorKeyClientType: authz.AuthClientTypeRemoteSession, authz.SelectorKeyConnectionOwner: "user_owner",
			})}
		}, []authz.Grant{personal}, authClientNoAccess, "deny"},
		{"another lender's attachment refuses", "user_lender", func(uuid.UUID) []authz.Grant {
			return []authz.Grant{personal, authz.NewGrant(authz.ScopeMCPAuth, authz.WildcardResource)}
		}, []authz.Grant{personal, authz.NewGrant(authz.ScopeMCPAuth, authz.WildcardResource)}, authClientNoAccess, "deny"},
		{"the represented user's own policy must allow", "user_owner", func(uuid.UUID) []authz.Grant { return []authz.Grant{personal} }, []authz.Grant{authz.NewGrant(authz.ScopeMCPConnect, authz.WildcardResource)}, authClientNoAccess, "deny"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newAuthClientFixture(t, pool)
			client := f.oauthClient(authClientTypeRemoteSession)
			selector := f.choose(validUUID(client))
			selector.userSessionIssuerID = uuid.New()
			reader := enforcedShadowService(t, f)
			f.service.authz = authz.NewEngine(testenv.NewLogger(t), nil, func(context.Context, string) (bool, error) { return false, nil }, workos.NewStubClient(), authz.EngineOpts{
				AdmitPrincipalCredential: func(context.Context, *pgxpool.Pool) (authz.PrincipalCredentialAdmission, error) {
					return authz.PrincipalCredentialAdmission{
						OwnerUserID: "user_owner", Credential: []authz.Grant{personal}, Agent: []authz.Grant{personal}, Owner: tc.ownerGrant(client),
						Authorizer: authz.RepresentedUserPolicy{UserID: "user_owner", Policy: tc.authorizer}, LegacyAuthCeiling: false,
					}, nil
				},
			})
			f.service.authClientPermitter = mandatoryPermitter{engine: f.service.authz}
			session := uuid.New()
			lookup := &fakeAttachedConnections{conn: remotesessions.AttachedConnection{RemoteSessionID: session, OwnerUserID: tc.owner}, attached: true, err: nil, asked: nil}
			f.service.attachedConnections = lookup
			agentID := uuid.New()
			agent := urn.NewAgentSubject(agentID)

			ctx := admittedAgentContext(t, f.orgID, agentID)
			authorization, err := f.service.authz.PrepareMandatory(ctx)
			require.NoError(t, err)
			require.Equal(t, authz.MandatoryCallerAgent, authorization.Caller(), "the caller is admitted as the agent")
			require.Equal(t, "user_owner", authorization.RepresentedUserID(), "the represented user is the credential's authorizer")

			res, err := f.service.resolveAuthClientForServing(ctx, selector, f.serverID, f.projectID, agent)
			require.NoError(t, err)
			require.Equal(t, tc.outcome, res.outcome)
			require.Len(t, lookup.asked, 1)
			require.Equal(t, agent, lookup.asked[0].Subject, "the admitted subject is unchanged")
			outcomes := map[string]int64{}
			for key, n := range authClientShadowOutcomes(t, reader) {
				outcomes[strings.SplitN(key, "/", 2)[0]] += n
			}
			require.Equal(t, map[string]int64{tc.shadow: 1}, outcomes, "the shadow decides the same check")
		})
	}
}
