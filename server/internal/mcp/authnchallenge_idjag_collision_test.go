package mcp_test

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// serveCodexDocument answers every client metadata document fetch with Codex's
// document from a local server, and counts the fetches.
func serveCodexDocument(t *testing.T, ti *testInstance) *atomic.Int64 {
	t.Helper()
	var fetches atomic.Int64
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fetches.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"client_id":                  codexCIMDClientID,
			"client_name":                "Codex",
			"redirect_uris":              []string{"http://127.0.0.1/callback"},
			"token_endpoint_auth_method": "none",
		}); err != nil {
			t.Errorf("encode client metadata document: %v", err)
		}
	}))
	t.Cleanup(srv.Close)
	client := srv.Client()
	transport, ok := client.Transport.(*http.Transport)
	require.True(t, ok)
	transport.TLSClientConfig.ServerName = "example.com"
	transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, srv.Listener.Addr().String())
	}
	ti.service.SetCIMDResolver(cimd.NewResolverFromClient(client, testenv.NewMeterProvider(t), testenv.NewLogger(t)))
	return &fetches
}

// A secret-bearing registered row holding a catalog document URL is never
// served as that document: the fetched document cannot overwrite it, so the
// client is unknown and nothing is minted.
func TestTokenIDJAGExchangeRefusesRegisteredRowHoldingCatalogClientID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithMeterProvider(t, testenv.NewMeterProvider(t))
	fetches := serveCodexDocument(t, ti)
	authCtx := requireProjectAuthContext(t, ctx)
	toolset, _, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
	issuer := createTrustedIDJAGIssuer(t, ctx, ti, authCtx.ActiveOrganizationID, "https://identity.example.test", "https://identity.example.test/keys")
	toolset, err := toolsetsrepo.New(ti.conn).UpdateToolsetUserSessionIssuer(ctx, toolsetsrepo.UpdateToolsetUserSessionIssuerParams{
		UserSessionIssuerID: uuid.NullUUID{UUID: issuer.ID, Valid: true},
		Slug:                toolset.Slug,
		ProjectID:           *authCtx.ProjectID,
	})
	require.NoError(t, err)
	_, err = usersessionsrepo.New(ti.conn).CreateUserSessionClient(ctx, usersessionsrepo.CreateUserSessionClientParams{
		UserSessionIssuerID:     issuer.ID,
		ClientID:                codexCIMDClientID,
		ClientSecretHash:        conv.ToPGText("registered-secret-hash"),
		ClientName:              "Registered collision",
		RedirectUris:            []string{"http://127.0.0.1/callback"},
		TokenEndpointAuthMethod: oauthwire.AuthMethodClientSecretPost,
	})
	require.NoError(t, err)

	w := postIDJAGToken(t, ctx, ti, toolset.McpSlug.String, codexCIMDClientID, "unused.assertion.value")
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	require.JSONEq(t, `{"error":"invalid_client","error_description":"client authentication failed"}`, w.Body.String())
	require.EqualValues(t, 1, fetches.Load(), "the document is fetched, so the refusal is the upsert guard's")

	row, err := usersessionsrepo.New(ti.conn).GetUserSessionClientByClientID(ctx, usersessionsrepo.GetUserSessionClientByClientIDParams{UserSessionIssuerID: issuer.ID, ClientID: codexCIMDClientID})
	require.NoError(t, err)
	require.Equal(t, "Registered collision", row.ClientName)
	require.False(t, row.ClientIDMetadataUri.Valid)
}
