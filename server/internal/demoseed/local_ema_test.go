//go:build demoseed_safety

package demoseed

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/mcp-fixture/fixtureauth"
	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

// fakeDevIDP records the EMA policy the local seed registers through the
// dev-idp's management RPCs and answers them the way the dev-idp does.
type fakeDevIDP struct {
	mu          sync.Mutex
	apps        map[string]map[string]any // id -> app
	resources   map[string]map[string]any // id -> resource
	rules       map[string]map[string]any // resource id + issuer -> rule
	assignments map[string]string         // app id + user id + resource id -> scopes
}

func (f *fakeDevIDP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var in map[string]any
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	str := func(k string) string { s, _ := in[k].(string); return s }
	var out any
	switch strings.TrimPrefix(r.URL.Path, "/rpc/") {
	case "devIdp.getCurrentUser":
		out = map[string]any{"mode": "oauth2-1", "user": map[string]any{"id": "11111111-1111-4111-8111-111111111111", "email": testDeveloperEmail}}
	case "emaApps.list":
		items := []any{}
		for _, app := range f.apps {
			items = append(items, app)
		}
		out = map[string]any{"items": items}
	case "emaApps.create":
		id := uuid.NewString()
		f.apps[id] = map[string]any{"id": id, "client_id": str("client_id"), "client_secret": str("client_secret"), "jwks": "", "enabled": true}
		out = f.apps[id]
	case "emaApps.update":
		app := f.apps[str("id")]
		app["client_secret"] = str("client_secret")
		out = app
	case "emaResources.list":
		items := []any{}
		for _, res := range f.resources {
			items = append(items, res)
		}
		out = map[string]any{"items": items}
	case "emaResources.create":
		id := uuid.NewString()
		f.resources[id] = map[string]any{"id": id, "slug": str("slug"), "resource_identifier": str("resource_identifier")}
		out = f.resources[id]
	case "emaResources.update":
		res := f.resources[str("id")]
		res["resource_identifier"] = str("resource_identifier")
		out = res
	case "emaTrustRules.create":
		key := str("resource_id") + " " + str("trusted_issuer")
		if _, ok := f.rules[key]; !ok {
			f.rules[key] = map[string]any{"id": uuid.NewString(), "allowed_scopes": str("allowed_scopes"), "allowed_client_ids": str("allowed_client_ids")}
		}
		out = f.rules[key]
	case "emaTrustRules.update":
		out = map[string]any{"id": str("id")}
	case "emaAppAssignments.create":
		f.assignments[str("app_id")+" "+str("user_id")+" "+str("resource_id")] = str("granted_scopes")
		out = map[string]any{"id": uuid.NewString()}
	default:
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// The local seed registers the Enterprise Sign-In (ID-JAG) Server's EMA
// policy at the dev-idp and points the server's sign-in, authorization server
// and binding at the dev-idp and the fixture, on every reseed.
func TestLocalFixturesWireEnterpriseSignIn(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	blob := assetstest.NewTestBlobStore(t)

	key := make([]byte, 32)
	_, err = rand.Read(key)
	require.NoError(t, err)
	enc, err := encryption.New(base64.StdEncoding.EncodeToString(key))
	require.NoError(t, err)
	serverURL, err := url.Parse("https://speakeasy.example.test")
	require.NoError(t, err)

	idp := &fakeDevIDP{mu: sync.Mutex{}, apps: map[string]map[string]any{}, resources: map[string]map[string]any{}, rules: map[string]map[string]any{}, assignments: map[string]string{}}
	devIDP := httptest.NewServer(idp)
	t.Cleanup(devIDP.Close)

	const base = "https://fixture.example.test:4443"
	const clientID = "speakeasy-local-login"
	opts := LocalFixturesOptions{
		DeveloperEmail: testDeveloperEmail,
		Environment:    "local",
		MCPFixture: LocalMCPFixtureOptions{
			BaseURL: base, ServerURL: serverURL, AccessTokenTTL: time.Hour, Encryption: enc,
			DevIDPURL: devIDP.URL, DevIDPClientID: clientID, HTTPClient: devIDP.Client(),
		},
	}

	spec := LocalSpec()
	s := &seeder{spec: spec}
	q := testrepo.New(db)
	resource := fixtureauth.Resource(base, fixtureauth.PathEMAMCP)
	for range 2 {
		seedLocalPostgres(ctx, t, db, spec)
		require.NoError(t, RunLocalFixtures(ctx, testenv.NewLogger(t), db, blob, nil, opts))

		server, err := q.GetRemoteMCPServerURLFixture(ctx, s.det("remotemcp-enterprise-sign-in"))
		require.NoError(t, err)
		require.Equal(t, "Enterprise Sign-In (ID-JAG) Server", server.Name.String)
		require.Equal(t, resource, server.Url)

		wiring, err := q.GetLocalEMAWiringFixture(ctx, s.det("ema-binding-enterprise-sign-in"))
		require.NoError(t, err)
		require.Equal(t, resource, wiring.Resource)
		require.Equal(t, devIDP.URL+devIDPOAuth21Prefix, wiring.IdentityProvider)
		require.Equal(t, clientID, wiring.IdentityProviderClientID)
		secret, err := enc.Decrypt(wiring.IdentityProviderClientSecret.String)
		require.NoError(t, err)
		require.Equal(t, localEMAClientSecret, secret)
		require.Equal(t, fixtureauth.EMAAuthorizationServer(devIDP.URL), wiring.AuthorizationServer)
		require.Equal(t, fixtureauth.EMAAuthorizationServer(devIDP.URL)+"/token", wiring.AuthorizationServerTokenEndpoint.String)
		require.Equal(t, clientID, wiring.AuthorizationServerClientID, "the dev-idp's resource authorization server requires the client the ID-JAG names")
	}

	// The dev-idp holds one app with the shared secret, one resource for the
	// fixture's endpoint, one trust rule and the developer's assignment.
	require.Len(t, idp.apps, 1)
	for _, app := range idp.apps {
		require.Equal(t, clientID, app["client_id"])
		require.Equal(t, localEMAClientSecret, app["client_secret"])
	}
	require.Len(t, idp.resources, 1)
	for _, res := range idp.resources {
		require.Equal(t, fixtureauth.EMAResourceSlug, res["slug"])
		require.Equal(t, resource, res["resource_identifier"])
	}
	require.Len(t, idp.rules, 1)
	require.Len(t, idp.assignments, 1)
	for _, scopes := range idp.assignments {
		require.Equal(t, fixtureauth.EMAScope, scopes)
	}
}
