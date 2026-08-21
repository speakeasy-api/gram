package mcp_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	mcpservers_repo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	remotemcp_repo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type consentActionFixture struct {
	ti       *testInstance
	endpoint *mcp.ResolvedMcpEndpoint
	stateID  string
	// clientA/clientB derive distinct resources; clientC derives none.
	clientA uuid.UUID
	clientB uuid.UUID
	clientC uuid.UUID
}

const (
	consentUpstreamA = "https://upstream-a.example.com"
	consentUpstreamB = "https://upstream-b.example.com"
)

// createConsentRemoteClient mints a remote_session_issuer (fake AS endpoints;
// BuildAuthorizationUrl never contacts them) plus a client attached to every
// given user_session_issuer.
func createConsentRemoteClient(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID uuid.UUID, organizationID, slug string, userSessionIssuerIDs []uuid.UUID) uuid.UUID {
	t.Helper()
	q := remotesessions_repo.New(conn)
	rsi, err := q.CreateRemoteSessionIssuer(ctx, remotesessions_repo.CreateRemoteSessionIssuerParams{
		ProjectID:                         uuid.NullUUID{UUID: projectID, Valid: true},
		Slug:                              slug + "-rsi",
		Issuer:                            "https://" + slug + "-as.example.com",
		AuthorizationEndpoint:             conv.ToPGText("https://" + slug + "-as.example.com/authorize"),
		TokenEndpoint:                     conv.ToPGText("https://" + slug + "-as.example.com/token"),
		ScopesSupported:                   []string{},
		GrantTypesSupported:               []string{"authorization_code", "refresh_token"},
		ResponseTypesSupported:            []string{"code"},
		TokenEndpointAuthMethodsSupported: []string{"none"},
		CodeChallengeMethodsSupported:     []string{"S256"},
	})
	require.NoError(t, err)

	rsc, err := q.CreateRemoteSessionClient(ctx, remotesessions_repo.CreateRemoteSessionClientParams{
		ProjectID:             conv.ToNullUUID(projectID),
		OrganizationID:        conv.ToPGTextEmpty(organizationID),
		RemoteSessionIssuerID: rsi.ID,
		ClientID:              slug + "-external-client",
		ClientIDIssuedAt:      pgtype.Timestamptz{Time: time.Now(), Valid: true},
	})
	require.NoError(t, err)
	for _, usi := range userSessionIssuerIDs {
		require.NoError(t, q.AttachRemoteSessionClientToUserSessionIssuer(ctx, remotesessions_repo.AttachRemoteSessionClientToUserSessionIssuerParams{
			RemoteSessionClientID: rsc.ID,
			UserSessionIssuerID:   usi,
		}))
	}
	return rsc.ID
}

// attachConsentRemoteMcpServer binds a remote-backed mcp_server to issuerID
// so clients on that issuer derive serverURL as their resource.
func attachConsentRemoteMcpServer(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID, issuerID uuid.UUID, slug, serverURL string) {
	t.Helper()
	remoteServer, err := remotemcp_repo.New(conn).CreateServer(ctx, remotemcp_repo.CreateServerParams{
		ID:            uuid.New(),
		ProjectID:     projectID,
		TransportType: "sse",
		Url:           serverURL,
	})
	require.NoError(t, err)
	_, err = mcpservers_repo.New(conn).CreateMCPServer(ctx, mcpservers_repo.CreateMCPServerParams{
		ID:                  uuid.New(),
		ProjectID:           projectID,
		Name:                conv.ToPGText(slug),
		Slug:                conv.ToPGText(slug),
		RemoteMcpServerID:   conv.ToNullUUID(remoteServer.ID),
		Visibility:          "private",
		UserSessionIssuerID: conv.ToNullUUID(issuerID),
	})
	require.NoError(t, err)
}

// seedMultiClientConsentEndpoint: one shared user_session_issuer with three
// bound clients, two pinned to distinct upstreams via their own issuer link.
func seedMultiClientConsentEndpoint(t *testing.T) (context.Context, consentActionFixture) {
	t.Helper()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	shared := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	issuerA := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	issuerB := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	attachConsentRemoteMcpServer(t, ctx, ti.conn, projectID, issuerA, "age3328-srv-a", consentUpstreamA+"/")
	attachConsentRemoteMcpServer(t, ctx, ti.conn, projectID, issuerB, "age3328-srv-b", consentUpstreamB)

	clientA := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "age3328-a", []uuid.UUID{shared, issuerA})
	clientB := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "age3328-b", []uuid.UUID{shared, issuerB})
	clientC := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "age3328-c", []uuid.UUID{shared})

	endpoint := &mcp.ResolvedMcpEndpoint{
		AudienceURN:          urn.NewUserSessionIssuer(shared).String(),
		CIMDAdmissionModeRaw: pgtype.Text{String: "", Valid: false},
		CustomDomainID:       uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		IsPublic:             false,
		McpServerID:          uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		OrganizationID:       orgID,
		ProjectID:            projectID,
		RouteBase:            "mcp",
		Slug:                 "age3328-consent",
		ToolsetID:            uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		// Deliberately matches no client: the action must never thread it in.
		UpstreamResource:    "https://endpoint-level.example.com",
		UserSessionIssuerID: shared,
	}

	subject := urn.NewUserSubject(uuid.NewString())
	stateID := uuid.NewString()
	require.NoError(t, ti.authnChallengeCache.Store(ctx, mcp.AuthnChallengeState{
		ID:                  stateID,
		UserSessionIssuerID: shared,
		Endpoint: mcp.EndpointRef{
			McpSlug:        endpoint.Slug,
			RouteBase:      endpoint.RouteBase,
			CustomDomainID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		},
		ClientID:            "age3328-mcp-client",
		RedirectURI:         "http://example.com/cb",
		CodeChallenge:       "",
		CodeChallengeMethod: "",
		CSRFToken:           "csrf-token",
		Subject:             &subject,
		CreatedAt:           time.Now(),
	}))

	return ctx, consentActionFixture{
		ti:       ti,
		endpoint: endpoint,
		stateID:  stateID,
		clientA:  clientA,
		clientB:  clientB,
		clientC:  clientC,
	}
}

// postConnectAction drives one client's connect action and returns the
// upstream authorize redirect URL.
func postConnectAction(t *testing.T, fx consentActionFixture, clientID uuid.UUID) *url.URL {
	t.Helper()

	form := url.Values{}
	form.Set("state", fx.stateID)
	form.Set("csrf_token", "csrf-token")
	form.Set("action", "connect")
	form.Set("client_id", clientID.String())

	req := httptest.NewRequest(http.MethodPost, "/mcp/"+fx.endpoint.Slug+"/connect/remote-session", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	require.NoError(t, fx.ti.service.ServeConsentAction(w, req, fx.endpoint))
	require.Equal(t, http.StatusSeeOther, w.Code)

	loc, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	return loc
}

// mintedRemoteLoginState reads back the stored RemoteLoginState — its
// Resource is what the code exchange records onto the grant.
func mintedRemoteLoginState(t *testing.T, ctx context.Context, fx consentActionFixture, upstreamState string) remotesessions.RemoteLoginState {
	t.Helper()
	loginCache := cache.NewTypedObjectCache[remotesessions.RemoteLoginState](fx.ti.logger, fx.ti.cacheAdapter, cache.SuffixNone)
	state, err := loginCache.Get(ctx, "remoteLogin:"+upstreamState)
	require.NoError(t, err)
	return state
}

func TestServeConsentAction_ConnectSendsPerClientResource(t *testing.T) {
	t.Parallel()

	ctx, fx := seedMultiClientConsentEndpoint(t)

	locA := postConnectAction(t, fx, fx.clientA)
	require.Equal(t, "age3328-a-as.example.com", locA.Host)
	require.Equal(t, consentUpstreamA, locA.Query().Get("resource"))
	stateA := mintedRemoteLoginState(t, ctx, fx, locA.Query().Get("state"))
	require.Equal(t, consentUpstreamA, stateA.Resource)
	require.Equal(t, fx.clientA, stateA.RemoteSessionClientID)

	locB := postConnectAction(t, fx, fx.clientB)
	require.Equal(t, "age3328-b-as.example.com", locB.Host)
	require.Equal(t, consentUpstreamB, locB.Query().Get("resource"))
	stateB := mintedRemoteLoginState(t, ctx, fx, locB.Query().Get("state"))
	require.Equal(t, consentUpstreamB, stateB.Resource)
	require.Equal(t, fx.clientB, stateB.RemoteSessionClientID)
}

func TestServeConsentAction_ConnectWithoutDerivableResourceSendsNone(t *testing.T) {
	t.Parallel()

	ctx, fx := seedMultiClientConsentEndpoint(t)

	loc := postConnectAction(t, fx, fx.clientC)
	require.Equal(t, "age3328-c-as.example.com", loc.Host)
	_, hasResource := loc.Query()["resource"]
	require.False(t, hasResource, "a client with no derivable resource must send none — not the endpoint-level one")
	state := mintedRemoteLoginState(t, ctx, fx, loc.Query().Get("state"))
	require.Empty(t, state.Resource)
}
