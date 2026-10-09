package mcp_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

// markConsentClientSelf makes a consent client hold its upstream credential
// for itself, with the confidential authentication that requires.
func markConsentClientSelf(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID uuid.UUID, orgID string, clientID uuid.UUID) {
	t.Helper()

	fixtures := testrepo.New(conn)
	rows, err := fixtures.ForceRemoteSessionClientAuthMethodFixture(ctx, testrepo.ForceRemoteSessionClientAuthMethodFixtureParams{
		TokenEndpointAuthMethod: conv.ToPGText("client_secret_basic"),
		ID:                      clientID,
		ProjectID:               conv.ToNullUUID(projectID),
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), rows)

	rows, err = fixtures.ForceRemoteSessionClientCredentialOwnerFixture(ctx, testrepo.ForceRemoteSessionClientCredentialOwnerFixtureParams{
		CredentialOwner: string(remotesessions.CredentialOwnerSelf),
		ID:              clientID,
		ProjectID:       conv.ToNullUUID(projectID),
		OrganizationID:  conv.ToPGText(orgID),
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), rows)
}

// seedSelfClientConsentEndpoint binds a self client, and a subject client
// when withSubjectClient, to one consent endpoint.
func seedSelfClientConsentEndpoint(t *testing.T, slug string, withSubjectClient bool) consentActionFixture {
	t.Helper()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	shared := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	selfClient := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, slug+"-self", "", []uuid.UUID{shared})
	markConsentClientSelf(t, ctx, ti.conn, projectID, orgID, selfClient)

	subjectClient := uuid.Nil
	if withSubjectClient {
		subjectClient = createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, slug+"-subject", "", []uuid.UUID{shared})
	}

	endpoint, stateID, subject := mintConsentEndpointState(t, ctx, ti, projectID, orgID, shared, slug+"-consent")

	fx := consentActionFixture{
		ti:        ti,
		endpoint:  endpoint,
		stateID:   stateID,
		projectID: projectID,
		orgID:     orgID,
		shared:    shared,
		subject:   subject,
		clientA:   selfClient,
		clientB:   subjectClient,
		clientC:   uuid.Nil,
		clientD:   uuid.Nil,
	}
	registerPageClient(t, ctx, fx)

	return fx
}

func TestServeConsent_SelfClientHasNoConnectionCard(t *testing.T) {
	t.Parallel()

	fx := seedSelfClientConsentEndpoint(t, "self-card", true)

	// The subject's connection is the page's only card, so the first render
	// auto-connects it; a counted self client would have suppressed that.
	code, _, loc := render(t, fx)
	require.Equal(t, http.StatusSeeOther, code)
	require.NotNil(t, loc)
	require.Equal(t, "self-card-subject-as.example.com", loc.Host)

	code, html, _ := render(t, fx)
	require.Equal(t, http.StatusOK, code, html)
	require.Contains(t, html, "0 of 1 connected", "only the subject's own connection is listed")
	require.Contains(t, html, `data-remote-client="`+fx.clientB.String()+`"`)
	require.NotContains(t, html, fx.clientA.String())
}

func TestServeConsent_SelfClientAloneNeedsNoConnectStep(t *testing.T) {
	t.Parallel()

	fx := seedSelfClientConsentEndpoint(t, "self-alone", false)

	// No connection to make, so no upstream auto-connect redirect and no
	// connection list.
	code, html, loc := render(t, fx)
	require.Equal(t, http.StatusOK, code, html)
	require.Nil(t, loc)
	require.NotContains(t, html, "data-service-connections")
	require.NotContains(t, html, fx.clientA.String())
}

func TestServeConsentAction_RefusesConnectingSelfClient(t *testing.T) {
	t.Parallel()

	fx := seedSelfClientConsentEndpoint(t, "self-connect", true)

	form := url.Values{}
	form.Set("state", fx.stateID)
	form.Set("csrf_token", "csrf-token")
	form.Set("action", "connect")
	form.Set("client_id", fx.clientA.String())

	req := httptest.NewRequest(http.MethodPost, "/mcp/"+fx.endpoint.Slug+"/connect/remote-session", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	err := fx.ti.service.ServeConsentAction(w, req, fx.endpoint)

	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeBadRequest, shareable.Code)
	require.Empty(t, w.Header().Get("Location"), "no upstream authorize redirect for a self client")
}

// connectAmbiguousClient posts a connect for a subject client attached to two
// upstreams, on an endpoint whose upstream is the first, and returns the
// resource the login recorded. withSelfClient also binds a self client that
// serves the endpoint's upstream.
func connectAmbiguousClient(t *testing.T, slug string, withSelfClient bool) string {
	t.Helper()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	shared := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	other := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	attachConsentRemoteMcpServer(t, ctx, ti.conn, projectID, shared, slug+"-srv-a", consentUpstreamA)
	attachConsentRemoteMcpServer(t, ctx, ti.conn, projectID, other, slug+"-srv-b", consentUpstreamB)

	subjectClient := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, slug+"-subject", "", []uuid.UUID{shared, other})
	if withSelfClient {
		selfClient := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, slug+"-self", "", []uuid.UUID{shared})
		markConsentClientSelf(t, ctx, ti.conn, projectID, orgID, selfClient)
	}

	endpoint, stateID, subject := mintConsentEndpointState(t, ctx, ti, projectID, orgID, shared, slug+"-consent")
	endpoint.UpstreamResource = consentUpstreamA
	fx := consentActionFixture{
		ti:        ti,
		endpoint:  endpoint,
		stateID:   stateID,
		projectID: projectID,
		orgID:     orgID,
		shared:    shared,
		subject:   subject,
		clientA:   subjectClient,
		clientB:   uuid.Nil,
		clientC:   uuid.Nil,
		clientD:   uuid.Nil,
	}

	loc := postConnectAction(t, fx, subjectClient)
	return mintedRemoteLoginState(t, ctx, fx, loc.Query().Get("state")).Resource
}

func TestServeConsentAction_SelfClientKeepsItsUpstreamFromSiblings(t *testing.T) {
	t.Parallel()

	require.Equal(t, consentUpstreamA, connectAmbiguousClient(t, "self-sibling-control", false),
		"with no other client serving it, the ambiguous client claims the endpoint's upstream")
	require.Empty(t, connectAmbiguousClient(t, "self-sibling", true),
		"a self client serving the upstream keeps a subject client from claiming it, or both would route there")
}
