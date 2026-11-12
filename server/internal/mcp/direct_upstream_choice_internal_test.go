package mcp

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/metric/metricdata/metricdatatest"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/mcpconnectionattemptstest"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpidentity"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/identitychaining"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestDirectUpstreamNoAuthNeverChains(t *testing.T) {
	t.Parallel()
	pool, err := TestInfra.CloneTestDatabase(t, "mcp_direct_no_auth")
	require.NoError(t, err)
	for _, mode := range []string{"off", "shadow", "enforced"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			f := newAuthClientFixture(t, pool)
			if mode == "enforced" {
				f.enforce()
			}
			if mode == "shadow" {
				flags := new(feature.InMemory)
				flags.SetFlag(feature.FlagAuthClientResolver, f.orgID, true)
				f.service.features = flags
			}
			selector := f.choose(uuid.NullUUID{})
			issuerID := uuid.New()
			subject := urn.NewUserSubject("user_1")
			proof := validatedSessionProof(t, subject)
			ctx := mcpidentity.NewValidatorBoundary().StampValidatedSession(t.Context(), proof)
			ctx = context.WithValue(ctx, liveSessionPolicyKey{}, true)
			ctx = contextvalues.SetAuthContext(ctx, &contextvalues.AuthContext{UserID: "user_1", ActiveOrganizationID: f.orgID})
			require.NoError(t, f.q.SeedUserSessionIssuerFixture(ctx, authclientbackfill.SeedUserSessionIssuerFixtureParams{ID: issuerID, ProjectID: validUUID(f.projectID), Slug: "issuer"}))
			require.NoError(t, f.q.SeedUserSessionFixture(ctx, authclientbackfill.SeedUserSessionFixtureParams{
				ProjectID: validUUID(f.projectID), OrganizationID: text(f.orgID), UserSessionIssuerID: issuerID,
				SubjectUrn: subject, Jti: proof.JTI(), CredentialID: validUUID(selector.credentialID),
			}))
			// Any chain probe or acquisition panics; token resolution is likewise unset.
			f.service.identityChainer = unusedChainer{}
			for _, tunneled := range []bool{false, true} {
				authentication := &issuerGateAuthentication{
					endpoint: &ResolvedMcpEndpoint{OrganizationID: f.orgID, ProjectID: f.projectID, UserSessionIssuerID: issuerID, McpServerID: validUUID(f.serverID)},
					subject:  subject, chosenClient: validUUID(f.registration),
				}
				token, err := f.service.resolveDirectUpstreamToken(ctx, httptest.NewRecorder(), testenv.NewLogger(t), authentication, "https://upstream.example.com", tunneled, validUUID(f.issuerID))
				require.NoError(t, err)
				require.Equal(t, remotesessions.UpstreamToken{}, token)
				require.True(t, authentication.chosenNoAuth)
			}
		})
	}
}

func TestAgentKeyDirectNoAuthNeverChains(t *testing.T) {
	t.Parallel()
	pool, err := TestInfra.CloneTestDatabase(t, "mcp_agent_key_no_auth")
	require.NoError(t, err)
	f := newAuthClientFixture(t, pool)
	f.enforce()
	keyID, agentID := uuid.New(), uuid.New()
	inserted, err := upstreamclientsrepo.New(pool).RecordAuthClientChoice(t.Context(), upstreamclientsrepo.RecordAuthClientChoiceParams{
		CredentialKind: upstreamclients.CredentialKindAPIKey, CredentialID: keyID, Source: upstreamclients.SelectionSourceIssuance, OrganizationID: f.orgID, AuthClientID: uuid.NullUUID{}, McpServerID: f.serverID,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), inserted)
	ctx := contextvalues.WithPrincipalAPIKeyAuthorization(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: f.orgID, APIKeyID: keyID.String()}, urn.NewPrincipal(urn.PrincipalTypeAgent, agentID.String()), contextvalues.PrincipalCredential{AuthorizerUserID: "user_1"})
	f.service.identityChainer = unusedChainer{}
	for _, tunneled := range []bool{false, true} {
		authentication := &issuerGateAuthentication{endpoint: &ResolvedMcpEndpoint{OrganizationID: f.orgID, ProjectID: f.projectID, McpServerID: validUUID(f.serverID)}, subject: urn.NewAgentSubject(agentID)}
		token, err := f.service.resolveDirectUpstreamToken(ctx, httptest.NewRecorder(), testenv.NewLogger(t), authentication, "https://upstream.example.com", tunneled, validUUID(f.issuerID))
		require.NoError(t, err)
		require.Equal(t, remotesessions.UpstreamToken{}, token)
		require.True(t, authentication.chosenNoAuth)
	}
}

func TestAgentKeyUngatedChallengeHasResourceMetadata(t *testing.T) {
	t.Parallel()
	pool, err := TestInfra.CloneTestDatabase(t, "mcp_ungated_challenge")
	require.NoError(t, err)
	f := newAuthClientFixture(t, pool)
	f.enforce()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
	f.service.metrics = mcpmetrics.NewMetrics(provider.Meter("test"), testenv.NewLogger(t))
	f.service.authClientPermitter = fixedPermitter(true)
	clientID := f.adminClient(authClientTypeStaticCredential, uuid.NullUUID{})
	f.copyHeader(clientID, "X-Api-Key", false, false, text("chosen-value"), pgtype.Text{})
	keyID := uuid.New()
	inserted, err := upstreamclientsrepo.New(pool).RecordAuthClientChoice(t.Context(), upstreamclientsrepo.RecordAuthClientChoiceParams{
		CredentialKind: upstreamclients.CredentialKindAPIKey, CredentialID: keyID, Source: upstreamclients.SelectionSourceIssuance,
		OrganizationID: f.orgID, AuthClientID: validUUID(clientID), McpServerID: f.serverID,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), inserted)
	require.NoError(t, f.q.SetAuthClientEnabledFixture(t.Context(), authclientbackfill.SetAuthClientEnabledFixtureParams{ID: clientID, Enabled: false}))
	ctx := contextvalues.WithPrincipalAPIKeyAuthorization(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: f.orgID, APIKeyID: keyID.String()}, urn.NewPrincipal(urn.PrincipalTypeAgent, uuid.NewString()), contextvalues.PrincipalCredential{AuthorizerUserID: "user_1"})
	ctx = contextvalues.SetRequestContext(ctx, &contextvalues.RequestContext{Host: "mcp.example.com"})
	endpoint := &mcpendpointsrepo.McpEndpoint{ProjectID: f.projectID, Slug: "chosen-server"}
	server := &mcpserversrepo.McpServer{ID: f.serverID, ProjectID: f.projectID, RemoteMcpServerID: validUUID(f.remoteID)}
	for _, baseURL := range []string{"https://mcp.example.com", "https://private.example.com"} {
		response := httptest.NewRecorder()
		_, _, err := f.service.agentKeyUngatedUpstream(ctx, response, nil, testenv.NewLogger(t), baseURL, endpoint, server, f.orgID, remotesessions.UpstreamToken{}, nil)
		require.Error(t, err)
		var challenge *oops.ShareableError
		require.ErrorAs(t, err, &challenge)
		require.Equal(t, oops.CodeUnauthorized, challenge.Code)
		require.Contains(t, response.Header().Get("WWW-Authenticate"), `resource_metadata="`+baseURL+`/.well-known/oauth-protected-resource/mcp/chosen-server"`)
	}
	var data metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(ctx, &data))
	found := false
	for _, scope := range data.ScopeMetrics {
		for _, metric := range scope.Metrics {
			if metric.Name == mcpmetrics.InstrumentMCPRequestRejected {
				found = true
				metricdatatest.AssertHasAttributes(t, metric,
					attr.McpURL("mcp.example.com/mcp/chosen-server"),
					attr.McpSurface(string(mcpmetrics.SurfaceHosting)),
				)
			}
		}
	}
	require.True(t, found, "reconnect must record rejection telemetry")

}

// A chaining refusal on a direct server writes one request row naming who fixes it.
func TestDirectUpstreamChainingRefusalWritesARequestRow(t *testing.T) {
	t.Parallel()
	pool, err := TestInfra.CloneTestDatabase(t, "mcp_direct_chaining_refusal")
	require.NoError(t, err)
	for _, tc := range []struct {
		reason identitychaining.Reason
		code   mcpconnectionattempts.Code
	}{
		{identitychaining.ReasonReauthenticationRequired, mcpconnectionattempts.CodeReauthenticationRequired},
		{identitychaining.ReasonIneligibleIdentity, mcpconnectionattempts.CodeIneligibleIdentity},
	} {
		t.Run(string(tc.reason), func(t *testing.T) {
			t.Parallel()
			f := newAuthClientFixture(t, pool)
			subject := urn.NewUserSubject("user_1")
			proof := validatedSessionProof(t, subject)
			ctx := mcpidentity.NewValidatorBoundary().StampValidatedSession(t.Context(), proof)
			ctx = context.WithValue(ctx, liveSessionPolicyKey{}, true)
			ctx = contextvalues.SetAuthContext(ctx, &contextvalues.AuthContext{UserID: "user_1", ActiveOrganizationID: f.orgID})
			f.service.upstreamTokens = &fakeUpstreamTokens{issuerID: uuid.New()}
			chainer := &recordingChainer{outcome: identitychaining.Outcome{Stage: identitychaining.StageAuthorization, Reason: tc.reason, Confidence: identitychaining.ConfidenceVerified, Retryable: false, Cached: false, Provider: ""}, governs: true}
			f.service.identityChainer = chainer
			rec := mcpconnectionattemptstest.New(t)
			f.service.connections = rec.Recorder
			authentication := &issuerGateAuthentication{
				// A toolset endpoint has no stored choice, so the preset client chains.
				endpoint: &ResolvedMcpEndpoint{OrganizationID: f.orgID, ProjectID: f.projectID, UserSessionIssuerID: uuid.New(), McpServerID: uuid.NullUUID{}, ToolsetID: validUUID(uuid.New())},
				subject:  subject, chosenClient: validUUID(f.registration),
			}
			_, err := f.service.resolveDirectUpstreamToken(ctx, httptest.NewRecorder(), testenv.NewLogger(t), authentication, "https://upstream.example.com", false, validUUID(f.issuerID))
			if tc.reason == identitychaining.ReasonIneligibleIdentity {
				require.Error(t, err, "an ineligible account is refused, not challenged")
			}
			require.Len(t, chainer.requests, 1)
			rec.RequireEnded(mcpconnectionattempts.StepMCPRequest, tc.code)
			require.Len(t, rec.Rows(), 1)
		})
	}
}
