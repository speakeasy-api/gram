package assistants

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	"github.com/speakeasy-api/gram/server/internal/externalmcp"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
)

func newCIMDAuthTestService(t *testing.T, conn *pgxpool.Pool) *Service {
	t.Helper()
	svc := newMCPAuthTestService(t, conn)
	serverURL, err := url.Parse("https://gram.example.com")
	require.NoError(t, err)
	svc.core.serverURL = serverURL
	siteURL, err := url.Parse("https://app.getgram.ai")
	require.NoError(t, err)
	svc.core.SetSiteURL(siteURL)
	return svc
}

func seedAssistantOrgMetadata(t *testing.T, conn *pgxpool.Pool) {
	t.Helper()
	err := orgsrepo.New(conn).CreateOrganizationMetadata(t.Context(), orgsrepo.CreateOrganizationMetadataParams{
		ID:   "org-test",
		Name: "Test Org",
		Slug: "acme",
	})
	require.NoError(t, err)
}

func usableAfterNow() pgtype.Timestamptz {
	return pgtype.Timestamptz{
		Time:             time.Now().Add(mcpAuthFlowTTL),
		InfinityModifier: pgtype.Finite,
		Valid:            true,
	}
}

func claimLeaseMinute() pgtype.Interval {
	return pgtype.Interval{
		Microseconds: time.Minute.Microseconds(),
		Days:         0,
		Months:       0,
		Valid:        true,
	}
}

func assistantCIMDDocumentRequest(t *testing.T, id string, customDomain bool) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-client/assistants/"+id, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	if customDomain {
		ctx = customdomains.WithContext(ctx, &customdomains.Context{
			OrganizationID: "org-cimd",
			Domain:         "mcp.customer.example.com",
			DomainID:       uuid.New(),
		})
	}
	return req.WithContext(ctx)
}

func TestAssistantClientMetadataDocumentURL(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("00000000-0000-0000-0000-0000000000aa")
	base, err := url.Parse("https://gram.example.com")
	require.NoError(t, err)
	require.Equal(t, "https://gram.example.com/.well-known/oauth-client/assistants/"+id.String(), AssistantClientMetadataDocumentURL(base, id))

	trailing, err := url.Parse("https://gram.example.com/")
	require.NoError(t, err)
	require.Equal(t, "https://gram.example.com/.well-known/oauth-client/assistants/"+id.String(), AssistantClientMetadataDocumentURL(trailing, id))

	parsed, ok := ParseAssistantClientMetadataDocumentURL(base, AssistantClientMetadataDocumentURL(base, id))
	require.True(t, ok)
	require.Equal(t, id, parsed)
	parsed, ok = ParseAssistantClientMetadataDocumentURL(trailing, AssistantClientMetadataDocumentURL(base, id))
	require.True(t, ok)
	require.Equal(t, id, parsed)
	for _, clientID := range []string{
		"https://other.example.com/.well-known/oauth-client/assistants/" + id.String(),
		"https://gram.example.com/.well-known/oauth-client/" + id.String(),
		"https://gram.example.com/.well-known/oauth-client/assistants/not-a-uuid",
		AssistantClientMetadataDocumentURL(base, id) + "/extra",
		AssistantClientMetadataDocumentURL(base, id) + "?x=1",
	} {
		_, ok = ParseAssistantClientMetadataDocumentURL(base, clientID)
		require.False(t, ok, clientID)
	}
	_, ok = ParseAssistantClientMetadataDocumentURL(nil, AssistantClientMetadataDocumentURL(base, id))
	require.False(t, ok)
}

func TestBuildAssistantClientMetadataDocument(t *testing.T) {
	t.Parallel()

	const clientID = "https://gram.example.com/.well-known/oauth-client/assistants/abc"
	const redirectURI = "https://gram.example.com/rpc/assistantMcpAuth/abc/oauth/callback"
	const clientURI = "https://app.getgram.ai/acme/projects/project/assistants/abc"

	doc := buildAssistantClientMetadataDocument(clientID, assistantClientName("Support Bot"), clientURI, redirectURI)
	body, err := json.Marshal(doc)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, json.Unmarshal(body, &got))
	require.Equal(t, clientID, got["client_id"])
	require.Equal(t, "Gram Assistant: Support Bot", got["client_name"])
	require.Equal(t, clientURI, got["client_uri"])
	require.Equal(t, []any{redirectURI}, got["redirect_uris"])
	require.Equal(t, []any{"authorization_code"}, got["grant_types"])
	require.Equal(t, []any{"code"}, got["response_types"])
	require.Equal(t, "none", got["token_endpoint_auth_method"])
}

func TestBuildAssistantClientMetadataDocumentOmitsEmptyClientURI(t *testing.T) {
	t.Parallel()

	doc := buildAssistantClientMetadataDocument("https://gram.example.com/.well-known/oauth-client/assistants/abc", "Gram Assistant", "", "https://gram.example.com/callback")
	body, err := json.Marshal(doc)
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, json.Unmarshal(body, &got))
	_, present := got["client_uri"]
	require.False(t, present)
}

func TestIssuerSupportsAssistantCIMD(t *testing.T) {
	t.Parallel()

	require.False(t, issuerSupportsAssistantCIMD(nil))
	require.False(t, issuerSupportsAssistantCIMD(&externalmcp.OAuthDiscoveryResult{
		ClientIDMetadataDocumentSupported: false,
		TokenEndpointAuthMethodsSupported: nil,
	}))
	require.True(t, issuerSupportsAssistantCIMD(&externalmcp.OAuthDiscoveryResult{
		ClientIDMetadataDocumentSupported: true,
		TokenEndpointAuthMethodsSupported: nil,
	}))
	require.True(t, issuerSupportsAssistantCIMD(&externalmcp.OAuthDiscoveryResult{
		ClientIDMetadataDocumentSupported: true,
		TokenEndpointAuthMethodsSupported: []string{"none", "client_secret_basic"},
	}))
	require.False(t, issuerSupportsAssistantCIMD(&externalmcp.OAuthDiscoveryResult{
		ClientIDMetadataDocumentSupported: true,
		TokenEndpointAuthMethodsSupported: []string{"client_secret_basic"},
	}))
}

func TestAssistantCIMDAllowed(t *testing.T) {
	t.Parallel()

	svc := newCIMDAuthTestService(t, nil)
	require.False(t, svc.assistantCIMDAllowed(t.Context(), "org-test", "acme"))

	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagAssistantOAuthCIMD, "org-test", true)
	svc.core.SetFeatureProvider(flags)
	require.True(t, svc.assistantCIMDAllowed(t.Context(), "org-test", "acme"))
	require.False(t, svc.assistantCIMDAllowed(t.Context(), "other-org", "acme"))

	plain, err := url.Parse("http://localhost:8080")
	require.NoError(t, err)
	svc.core.serverURL = plain
	require.False(t, svc.assistantCIMDAllowed(t.Context(), "org-test", "acme"), "CIMD client ids must be https")
}

func TestNewMCPAuthTokenRequestPublicClient(t *testing.T) {
	t.Parallel()

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	req, err := newMCPAuthTokenRequest(t.Context(), "https://auth.example.com/token", form, "https://gram.example.com/.well-known/oauth-client/assistants/abc", "")
	require.NoError(t, err)
	body, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	values, err := url.ParseQuery(string(body))
	require.NoError(t, err)
	require.Equal(t, "https://gram.example.com/.well-known/oauth-client/assistants/abc", values.Get("client_id"))
	require.Empty(t, req.Header.Get("Authorization"))
}

func TestNewMCPAuthTokenRequestConfidentialClient(t *testing.T) {
	t.Parallel()

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	req, err := newMCPAuthTokenRequest(t.Context(), "https://auth.example.com/token", form, "client-id", "s3cret")
	require.NoError(t, err)
	body, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	values, err := url.ParseQuery(string(body))
	require.NoError(t, err)
	require.Empty(t, values.Get("client_id"))
	user, password, ok := req.BasicAuth()
	require.True(t, ok)
	require.Equal(t, "client-id", user)
	require.Equal(t, "s3cret", password)
}

func TestGetOrRegisterMCPAuthClientUsesCIMDWithoutRegistration(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_mcp_oauth_cimd_create")
	require.NoError(t, err)
	projectID, assistantID, _, _ := insertAssistantFixture(t, conn)

	service := newCIMDAuthTestService(t, conn)
	redirectURI := "https://gram.example.com/rpc/assistantMcpAuth/" + assistantID.String() + "/oauth/callback"
	wantClientID := AssistantClientMetadataDocumentURL(service.core.serverURL, assistantID)

	first, err := service.getOrRegisterMCPAuthClient(
		t.Context(), projectID, assistantID, "https://auth.example.com", "", redirectURI, true,
	)
	require.NoError(t, err)
	second, err := service.getOrRegisterMCPAuthClient(
		t.Context(), projectID, assistantID, "https://auth.example.com", "", redirectURI, true,
	)
	require.NoError(t, err)

	require.Equal(t, first, second)
	require.Equal(t, wantClientID, first.ClientID)
	require.Empty(t, first.ClientSecretEncrypted)

	row, err := assistantrepo.New(conn).GetAssistantMCPOAuthClient(t.Context(), assistantrepo.GetAssistantMCPOAuthClientParams{
		ProjectID:         projectID,
		AssistantID:       assistantID,
		OauthServerIssuer: "https://auth.example.com",
		RedirectUri:       redirectURI,
		UsableAfter:       usableAfterNow(),
		ClaimLease:        claimLeaseMinute(),
	})
	require.NoError(t, err)
	require.True(t, row.Usable.Bool)
	require.True(t, row.ClientIDMetadataUri.Valid)
	require.Equal(t, wantClientID, row.ClientIDMetadataUri.String)
}

func TestGetOrRegisterMCPAuthClientKeepsDCRWhenCIMDEnabled(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_mcp_oauth_cimd_keeps_dcr")
	require.NoError(t, err)
	projectID, assistantID, _, _ := insertAssistantFixture(t, conn)

	var registrations atomic.Int32
	registrationServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		registrations.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(mcpAuthClientRegistrationResponse{
			ClientID:              "dcr-client",
			ClientSecret:          "dcr-secret",
			ClientSecretExpiresAt: 0,
		})
	}))
	t.Cleanup(registrationServer.Close)

	service := newCIMDAuthTestService(t, conn)
	redirectURI := "https://gram.example.com/rpc/assistantMcpAuth/" + assistantID.String() + "/oauth/callback"
	first, err := service.getOrRegisterMCPAuthClient(
		t.Context(), projectID, assistantID, registrationServer.URL, registrationServer.URL, redirectURI, false,
	)
	require.NoError(t, err)
	require.Equal(t, "dcr-client", first.ClientID)

	second, err := service.getOrRegisterMCPAuthClient(
		t.Context(), projectID, assistantID, registrationServer.URL, registrationServer.URL, redirectURI, true,
	)
	require.NoError(t, err)
	require.Equal(t, int32(1), registrations.Load())
	require.Equal(t, first, second)
	require.Equal(t, "dcr-client", second.ClientID)
}

func TestGetOrRegisterMCPAuthClientInvalidatedCIMDFallsBackToDCR(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_mcp_oauth_cimd_invalidated")
	require.NoError(t, err)
	projectID, assistantID, _, _ := insertAssistantFixture(t, conn)

	var registrations atomic.Int32
	registrationServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		registrations.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(mcpAuthClientRegistrationResponse{
			ClientID:              "fallback-dcr",
			ClientSecret:          "fallback-secret",
			ClientSecretExpiresAt: 0,
		})
	}))
	t.Cleanup(registrationServer.Close)

	service := newCIMDAuthTestService(t, conn)
	redirectURI := "https://gram.example.com/rpc/assistantMcpAuth/" + assistantID.String() + "/oauth/callback"
	cimd, err := service.getOrRegisterMCPAuthClient(
		t.Context(), projectID, assistantID, registrationServer.URL, registrationServer.URL, redirectURI, true,
	)
	require.NoError(t, err)
	require.Contains(t, cimd.ClientID, "/.well-known/oauth-client/assistants/")
	service.invalidateMCPAuthClient(t.Context(), projectID, assistantID, registrationServer.URL, cimd.ClientID)

	fallback, err := service.getOrRegisterMCPAuthClient(
		t.Context(), projectID, assistantID, registrationServer.URL, registrationServer.URL, redirectURI, true,
	)
	require.NoError(t, err)
	require.Equal(t, int32(1), registrations.Load())
	require.Equal(t, "fallback-dcr", fallback.ClientID)
	secret, err := service.core.encryptionClient.Decrypt(fallback.ClientSecretEncrypted)
	require.NoError(t, err)
	require.Equal(t, "fallback-secret", secret)
}

func TestGetOrRegisterMCPAuthClientInvalidatedCIMDRetriesWithoutRegistration(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_mcp_oauth_cimd_invalidated_no_dcr")
	require.NoError(t, err)
	projectID, assistantID, _, _ := insertAssistantFixture(t, conn)

	service := newCIMDAuthTestService(t, conn)
	redirectURI := "https://gram.example.com/rpc/assistantMcpAuth/" + assistantID.String() + "/oauth/callback"
	wantClientID := AssistantClientMetadataDocumentURL(service.core.serverURL, assistantID)

	first, err := service.getOrRegisterMCPAuthClient(
		t.Context(), projectID, assistantID, "https://auth.example.com", "", redirectURI, true,
	)
	require.NoError(t, err)
	require.Equal(t, wantClientID, first.ClientID)
	service.invalidateMCPAuthClient(t.Context(), projectID, assistantID, "https://auth.example.com", first.ClientID)

	second, err := service.getOrRegisterMCPAuthClient(
		t.Context(), projectID, assistantID, "https://auth.example.com", "", redirectURI, true,
	)
	require.NoError(t, err)
	require.Equal(t, first, second)

	row, err := assistantrepo.New(conn).GetAssistantMCPOAuthClient(t.Context(), assistantrepo.GetAssistantMCPOAuthClientParams{
		ProjectID:         projectID,
		AssistantID:       assistantID,
		OauthServerIssuer: "https://auth.example.com",
		RedirectUri:       redirectURI,
		UsableAfter:       usableAfterNow(),
		ClaimLease:        claimLeaseMinute(),
	})
	require.NoError(t, err)
	require.True(t, row.Usable.Bool)
	require.False(t, row.Invalidated.Bool)
}

func TestGetOrRegisterMCPAuthClientRetiredDCRMovesToCIMD(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_mcp_oauth_cimd_after_dcr")
	require.NoError(t, err)
	projectID, assistantID, _, _ := insertAssistantFixture(t, conn)

	registrationServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(mcpAuthClientRegistrationResponse{
			ClientID:              "dcr-client",
			ClientSecret:          "dcr-secret",
			ClientSecretExpiresAt: 0,
		})
	}))
	t.Cleanup(registrationServer.Close)

	service := newCIMDAuthTestService(t, conn)
	redirectURI := "https://gram.example.com/rpc/assistantMcpAuth/" + assistantID.String() + "/oauth/callback"
	dcr, err := service.getOrRegisterMCPAuthClient(
		t.Context(), projectID, assistantID, registrationServer.URL, registrationServer.URL, redirectURI, false,
	)
	require.NoError(t, err)
	require.Equal(t, "dcr-client", dcr.ClientID)
	service.invalidateMCPAuthClient(t.Context(), projectID, assistantID, registrationServer.URL, dcr.ClientID)

	// The authorization server dropped dynamic registration; the retired
	// DCR row must not pin the assistant to a path that no longer exists.
	cimd, err := service.getOrRegisterMCPAuthClient(
		t.Context(), projectID, assistantID, registrationServer.URL, "", redirectURI, true,
	)
	require.NoError(t, err)
	require.Equal(t, AssistantClientMetadataDocumentURL(service.core.serverURL, assistantID), cimd.ClientID)
	require.Empty(t, cimd.ClientSecretEncrypted)
}

func TestGetOrRegisterMCPAuthClientRetiresCIMDWhenDisabled(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_mcp_oauth_cimd_disabled")
	require.NoError(t, err)
	projectID, assistantID, _, _ := insertAssistantFixture(t, conn)

	var registrations atomic.Int32
	registrationServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		registrations.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(mcpAuthClientRegistrationResponse{
			ClientID:              "dcr-client",
			ClientSecret:          "dcr-secret",
			ClientSecretExpiresAt: 0,
		})
	}))
	t.Cleanup(registrationServer.Close)

	service := newCIMDAuthTestService(t, conn)
	redirectURI := "https://gram.example.com/rpc/assistantMcpAuth/" + assistantID.String() + "/oauth/callback"
	cimd, err := service.getOrRegisterMCPAuthClient(
		t.Context(), projectID, assistantID, registrationServer.URL, registrationServer.URL, redirectURI, true,
	)
	require.NoError(t, err)
	require.Contains(t, cimd.ClientID, "/.well-known/oauth-client/assistants/")
	require.Zero(t, registrations.Load())

	// CIMD switched off (flag or upstream): the live CIMD row is retired
	// and a confidential client registered instead of reusing a public
	// client_id the authorization server no longer accepts.
	dcr, err := service.getOrRegisterMCPAuthClient(
		t.Context(), projectID, assistantID, registrationServer.URL, registrationServer.URL, redirectURI, false,
	)
	require.NoError(t, err)
	require.Equal(t, int32(1), registrations.Load())
	require.Equal(t, "dcr-client", dcr.ClientID)
	secret, err := service.core.encryptionClient.Decrypt(dcr.ClientSecretEncrypted)
	require.NoError(t, err)
	require.Equal(t, "dcr-secret", secret)
}

func TestHandleMCPAuthCallbackInvalidClientRetiresCIMDClient(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_mcp_oauth_cimd_callback_invalid_client")
	require.NoError(t, err)
	projectID, assistantID, _, threadID := insertAssistantFixture(t, conn)

	service := newCIMDAuthTestService(t, conn)
	service.core.assistantTokens = assistanttokens.New("test-jwt-secret", conn, nil)
	service.signaler = &stubWorkflowSignaler{signalledThreads: nil}
	redirectURI := "https://gram.example.com/rpc/assistantMcpAuth/" + assistantID.String() + "/oauth/callback"
	cimd, err := service.getOrRegisterMCPAuthClient(
		t.Context(), projectID, assistantID, "https://auth.example.com", "", redirectURI, true,
	)
	require.NoError(t, err)

	state, err := service.core.assistantTokens.GenerateMCPAuthFlow(assistanttokens.MCPAuthFlowInput{
		OrgID:             "org-test",
		ProjectID:         projectID,
		UserID:            "user-test",
		AssistantID:       assistantID,
		ThreadID:          threadID,
		AttemptID:         "",
		FlowID:            "flow-1",
		ServerID:          uuid.NewString(),
		McpURL:            "https://gram.example.com/mcp/test",
		ClientID:          cimd.ClientID,
		ClientSecret:      "",
		RedirectURI:       redirectURI,
		CodeVerifier:      "",
		TokenEndpoint:     "https://auth.example.com/token",
		OAuthServerIssuer: "https://auth.example.com",
		TTL:               time.Minute,
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/rpc/assistantMcpAuth/flow-1/oauth/callback?state="+url.QueryEscape(state)+"&error=invalid_client", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "flow-1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	require.NoError(t, service.handleMCPAuthCallback(rec, req))
	require.Equal(t, http.StatusOK, rec.Code)

	row, err := assistantrepo.New(conn).GetAssistantMCPOAuthClient(t.Context(), assistantrepo.GetAssistantMCPOAuthClientParams{
		ProjectID:         projectID,
		AssistantID:       assistantID,
		OauthServerIssuer: "https://auth.example.com",
		RedirectUri:       redirectURI,
		UsableAfter:       usableAfterNow(),
		ClaimLease:        claimLeaseMinute(),
	})
	require.NoError(t, err)
	require.False(t, row.Usable.Bool)
	require.True(t, row.Invalidated.Bool)
}

func TestHandleAssistantClientMetadataDocumentServesDocument(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_mcp_oauth_cimd_document")
	require.NoError(t, err)
	seedAssistantOrgMetadata(t, conn)
	_, assistantID, _, _ := insertAssistantFixture(t, conn)

	service := newCIMDAuthTestService(t, conn)
	rec := httptest.NewRecorder()
	err = service.handleAssistantClientMetadataDocument(rec, assistantCIMDDocumentRequest(t, assistantID.String(), false))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, rec.Code)

	var got map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	wantClientID := AssistantClientMetadataDocumentURL(service.core.serverURL, assistantID)
	require.Equal(t, wantClientID, got["client_id"])
	require.Equal(t, "Gram Assistant: Assistant", got["client_name"])
	require.Equal(t, "https://app.getgram.ai/acme/projects/project/assistants/"+assistantID.String(), got["client_uri"])
	require.Equal(t, []any{service.core.serverURL.JoinPath("rpc", "assistantMcpAuth", assistantID.String(), "oauth", "callback").String()}, got["redirect_uris"])
	require.Equal(t, "none", got["token_endpoint_auth_method"])
	require.Equal(t, "application/json; charset=utf-8", rec.Header().Get("Content-Type"))
	require.Contains(t, rec.Header().Get("Cache-Control"), "max-age=3600")
}

func TestHandleAssistantClientMetadataDocumentNotFound(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_mcp_oauth_cimd_document_404")
	require.NoError(t, err)
	service := newCIMDAuthTestService(t, conn)

	err = service.handleAssistantClientMetadataDocument(httptest.NewRecorder(), assistantCIMDDocumentRequest(t, uuid.NewString(), false))
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)
}

func TestHandleAssistantClientMetadataDocumentNotFoundOnCustomDomain(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_mcp_oauth_cimd_document_custom")
	require.NoError(t, err)
	seedAssistantOrgMetadata(t, conn)
	_, assistantID, _, _ := insertAssistantFixture(t, conn)
	service := newCIMDAuthTestService(t, conn)

	err = service.handleAssistantClientMetadataDocument(httptest.NewRecorder(), assistantCIMDDocumentRequest(t, assistantID.String(), true))
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)
}
