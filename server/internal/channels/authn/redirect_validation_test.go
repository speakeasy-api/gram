package authn

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/channels/authn/fixturerepo"
	"github.com/speakeasy-api/gram/server/internal/channels/authn/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd"
)

var invalidChannelRedirects = []string{
	"https://client.example.com/callback#fragment",
	"https://client.example.com/callback#",
	"https://user:password@client.example.com/callback",
	"http://user@localhost/callback",
	"https:foo",
	"http:foo",
	"https:///callback",
	"https://:443/callback",
	"/callback",
	"http://client.example.com/callback",
	"https://client.example.com:bad/callback",
	"native:/callback#",
	"native://user@callback",
}

var validChannelRedirects = []string{
	"https://client.example.com/callback?value=%23fragment",
	"http://127.0.0.1:4567/callback",
	"http://[::1]:4567/callback",
	"http://localhost:4567/callback",
	"com.example.app:/oauth/callback",
	"native:callback",
	"native://callback",
}

func TestChannelsValidateRedirectURI(t *testing.T) {
	t.Parallel()
	for _, redirect := range invalidChannelRedirects {
		t.Run(redirect, func(t *testing.T) {
			t.Parallel()
			require.Error(t, validateRedirectURI(redirect))
		})
	}
	for _, redirect := range validChannelRedirects {
		t.Run(redirect, func(t *testing.T) {
			t.Parallel()
			require.NoError(t, validateRedirectURI(redirect))
		})
	}
}

func TestChannelsRedirectPersistence(t *testing.T) {
	t.Parallel()
	container, clone, err := testenv.NewTestPostgres(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, container.Terminate(context.Background())) })
	db, err := clone(t, "channels_redirect_validation")
	require.NoError(t, err)
	f := newOAuthFixture(t, db)
	clientID := "https://client.example.com/metadata.json"
	for _, redirect := range invalidChannelRedirects {
		body, err := json.Marshal(registration{ClientName: "Invalid callback client", RedirectURIs: []string{testRedirect, redirect}, AuthMethod: "none"})
		require.NoError(t, err)
		response := f.request(http.MethodPost, "/channels/oauth/register", string(body), "")
		require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
		require.JSONEq(t, `{"error":"invalid_redirect_uri"}`, response.Body.String())
		var count int
		require.NoError(t, db.QueryRow(t.Context(), "SELECT count(*) FROM channels_oauth_clients").Scan(&count))
		require.Zero(t, count, "invalid registration must not persist any redirects")

		f.service.clients = fakeMetadataResolver{document: &cimd.Document{ClientID: clientID, ClientName: "Invalid callback client", RedirectURIs: []string{testRedirect, redirect}, TokenEndpointAuthMethod: "none"}}
		_, err = f.service.resolveClient(t.Context(), clientID, true)
		require.Error(t, err)
		exists, err := fixturerepo.New(db).ClientExists(t.Context(), clientID)
		require.NoError(t, err)
		require.False(t, exists, "invalid metadata must not persist any redirects")
	}

	body, err := json.Marshal(registration{ClientName: "Valid callback client", RedirectURIs: validChannelRedirects, AuthMethod: "none"})
	require.NoError(t, err)
	response := f.request(http.MethodPost, "/channels/oauth/register", string(body), "")
	require.Equal(t, http.StatusCreated, response.Code, response.Body.String())
	var registered struct {
		ClientID string `json:"client_id"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &registered))
	stored, err := repo.New(db).GetClient(t.Context(), registered.ClientID)
	require.NoError(t, err)
	require.Equal(t, validChannelRedirects, stored.RedirectUris)

	f.service.clients = fakeMetadataResolver{document: &cimd.Document{ClientID: clientID, ClientName: "Valid callback client", RedirectURIs: validChannelRedirects, TokenEndpointAuthMethod: "none"}}
	stored, err = f.service.resolveClient(t.Context(), clientID, true)
	require.NoError(t, err)
	require.Equal(t, validChannelRedirects, stored.RedirectUris)
	for _, redirect := range invalidChannelRedirects {
		f.service.clients = fakeMetadataResolver{document: &cimd.Document{ClientID: clientID, ClientName: "Invalid callback client", RedirectURIs: []string{redirect}, TokenEndpointAuthMethod: "none"}}
		_, err = f.service.resolveClient(t.Context(), clientID, true)
		require.Error(t, err)
		stored, err = repo.New(db).GetClient(t.Context(), clientID)
		require.NoError(t, err)
		require.Equal(t, validChannelRedirects, stored.RedirectUris, "invalid metadata must not overwrite registered redirects")
	}
}
