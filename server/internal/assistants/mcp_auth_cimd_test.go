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
