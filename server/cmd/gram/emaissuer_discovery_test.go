package gram

import (
	"encoding/json"
	"flag"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"

	"github.com/speakeasy-api/gram/server/internal/background"
	"github.com/speakeasy-api/gram/server/internal/emaissuer"
	"github.com/speakeasy-api/gram/server/internal/identityproviderconnections/provisiontest"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// The MCP-only listener's middleware chain must let issuer discovery through.
func TestMCPMuxServesEMAIssuerDiscoveryAndKeys(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	infra, cleanup, err := testenv.Launch(ctx, testenv.LaunchOptions{Postgres: true})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, cleanup()) })
	pool, err := infra.CloneTestDatabase(t, "mcp_ema_issuer_discovery")
	require.NoError(t, err)
	organizationID := "org_" + uuid.NewString()
	_, err = organizationsrepo.New(pool).UpsertOrganizationMetadata(ctx, organizationsrepo.UpsertOrganizationMetadataParams{ID: organizationID, Name: "Discovery test", Slug: "discovery-" + uuid.NewString()})
	require.NoError(t, err)
	credential := provisiontest.CreatePlatformSigningCredential(t, ctx, pool)
	provisioner := provisiontest.NewProvisioner(t, pool, provisiontest.NewKMSClients(t).Factory, "https://app.example.com", credential, "")
	_, err = provisioner.EnsureEMAIssuer(ctx, organizationID)
	require.NoError(t, err)

	flags := flag.NewFlagSet("mcp-discovery", flag.ContinueOnError)
	flags.String("server-url", "https://app.example.com", "")
	flags.String("site-url", "https://app.example.com", "")
	flags.String("environment", "prod", "")
	origin, err := url.Parse("https://app.example.com")
	require.NoError(t, err)
	host, err := mcp.NewAuthenticationHost("https://auth.example.com", origin, "prod")
	require.NoError(t, err)
	mux, err := newMCPServerMux(cli.NewContext(nil, flags, nil), testenv.NewLogger(t), pool, origin, host, nil, nil, &background.Publishers{})
	require.NoError(t, err)
	emaissuer.Attach(mux, pool, testenv.NewLogger(t), origin)

	issuer := "https://app.example.com/oauth/idp/org/" + organizationID
	for _, route := range []string{
		issuer,
		issuer + "/.well-known/openid-configuration",
		"https://app.example.com/.well-known/openid-configuration/oauth/idp/org/" + organizationID,
		"https://app.example.com/.well-known/oauth-authorization-server/oauth/idp/org/" + organizationID,
	} {
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, route, nil))
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		var document map[string]string
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &document))
		require.Equal(t, issuer, document["issuer"])
		require.Equal(t, issuer+"/jwks", document["jwks_uri"])
	}

	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, issuer+"/jwks", nil))
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Equal(t, "application/jwk-set+json", response.Header().Get("Content-Type"))
	var published jose.JSONWebKeySet
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &published))
	require.Len(t, published.Keys, 1)
	require.True(t, published.Keys[0].IsPublic())

	response = httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "https://app.example.com/oauth/idp/org/org_"+uuid.NewString()+"/jwks", nil))
	require.Equal(t, http.StatusNotFound, response.Code, "an organization without an issuer resolves nothing")
}
