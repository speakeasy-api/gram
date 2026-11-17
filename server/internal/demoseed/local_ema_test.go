//go:build demoseed_safety

package demoseed

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	"github.com/speakeasy-api/gram/server/internal/demoseed/fixture/fixtureauth"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/associationbackfill"
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
	users       map[string]map[string]any // id -> user
	// currentUser is the OAuth 2.1 current user's id; empty answers 404, as a
	// dev-idp nobody has signed in to does.
	currentUser string
	// pageSize, when set, pages list results so callers must follow
	// next_cursor.
	pageSize int
}

// page returns one page of items, ordered by id, and the next cursor.
func (f *fakeDevIDP) page(rows map[string]map[string]any, cursor string) map[string]any {
	ids := make([]string, 0, len(rows))
	for id := range rows {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	start := 0
	if cursor != "" {
		start, _ = strconv.Atoi(cursor)
	}
	end := len(ids)
	if f.pageSize > 0 && start+f.pageSize < end {
		end = start + f.pageSize
	}
	items := []any{}
	for _, id := range ids[min(start, len(ids)):end] {
		items = append(items, rows[id])
	}
	next := ""
	if end < len(ids) {
		next = strconv.Itoa(end)
	}
	return map[string]any{"items": items, "next_cursor": next}
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
		if f.currentUser == "" {
			http.Error(w, `{"name":"not_found","message":"no currentUser set for mode \"oauth2-1\""}`, http.StatusNotFound)
			return
		}
		out = map[string]any{"mode": "oauth2-1", "user": map[string]any{"id": f.currentUser, "email": testDeveloperEmail}}
	case "users.list":
		out = f.page(f.users, str("cursor"))
	case "users.create":
		id := uuid.NewString()
		f.users[id] = map[string]any{"id": id, "email": str("email"), "display_name": str("display_name")}
		out = f.users[id]
	case "emaApps.list":
		out = f.page(f.apps, str("cursor"))
	case "emaApps.create":
		id := uuid.NewString()
		f.apps[id] = map[string]any{"id": id, "client_id": str("client_id"), "client_secret": str("client_secret"), "jwks": "", "enabled": true}
		out = f.apps[id]
	case "emaApps.update":
		app := f.apps[str("id")]
		app["client_secret"] = str("client_secret")
		app["jwks"] = str("jwks")
		app["enabled"] = in["enabled"]
		out = app
	case "emaResources.list":
		out = f.page(f.resources, str("cursor"))
	case "emaResources.create":
		id := uuid.NewString()
		f.resources[id] = map[string]any{"id": id, "slug": str("slug"), "resource_identifier": str("resource_identifier")}
		out = f.resources[id]
	case "emaResources.update":
		res := f.resources[str("id")]
		res["resource_identifier"] = str("resource_identifier")
		out = res
	case "emaTrustRules.create":
		// Upserts on (resource, trusted issuer), as the dev-idp does.
		key := str("resource_id") + " " + str("trusted_issuer")
		rule, ok := f.rules[key]
		if !ok {
			rule = map[string]any{"id": uuid.NewString(), "resource_id": str("resource_id"), "trusted_issuer": str("trusted_issuer")}
			f.rules[key] = rule
		}
		rule["allowed_client_ids"] = str("allowed_client_ids")
		rule["allowed_scopes"] = str("allowed_scopes")
		rule["enabled"] = in["enabled"]
		out = rule
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

func newFakeDevIDP(currentUser string) *fakeDevIDP {
	return &fakeDevIDP{
		mu: sync.Mutex{}, apps: map[string]map[string]any{}, resources: map[string]map[string]any{},
		rules: map[string]map[string]any{}, assignments: map[string]string{}, users: map[string]map[string]any{},
		currentUser: currentUser, pageSize: 0,
	}
}

// On a dev-idp nobody has signed in to, the seed assigns the developer's own
// dev-idp user, finding it by email or creating it, and it follows list
// cursors, so records on a later page are updated rather than duplicated.
func TestProvisionLocalEMAOnFreshDevIDP(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	serverURL, err := url.Parse("https://speakeasy.example.test")
	require.NoError(t, err)
	const clientID = "speakeasy-local-login"
	dev := developer{ID: "", WorkOSID: "", Email: testDeveloperEmail, Name: "Local Developer"}

	for _, existingUser := range []bool{false, true} {
		idp := newFakeDevIDP("")
		idp.pageSize = 1
		for i := range 3 {
			id := fmt.Sprintf("0000000%d-0000-4000-8000-000000000000", i)
			idp.apps[id] = map[string]any{"id": id, "client_id": fmt.Sprintf("other-%d", i), "client_secret": "x", "jwks": "", "enabled": true}
			idp.resources[id] = map[string]any{"id": id, "slug": fmt.Sprintf("other-%d", i), "resource_identifier": "https://other.example.test"}
			idp.users[id] = map[string]any{"id": id, "email": fmt.Sprintf("other-%d@example.test", i), "display_name": "Other"}
		}
		// The seed's own app and resource from an earlier run sit on the last page.
		idp.apps["ffffffff-0000-4000-8000-000000000000"] = map[string]any{"id": "ffffffff-0000-4000-8000-000000000000", "client_id": clientID, "client_secret": "edited", "jwks": "", "enabled": false}
		idp.resources["ffffffff-0000-4000-8000-000000000000"] = map[string]any{"id": "ffffffff-0000-4000-8000-000000000000", "slug": fixtureauth.EMAResourceSlug, "resource_identifier": "https://elsewhere.example.test/mcp/ema"}
		wantUser := ""
		if existingUser {
			wantUser = "ffffffff-0000-4000-8000-000000000000"
			idp.users[wantUser] = map[string]any{"id": wantUser, "email": testDeveloperEmail, "display_name": "Local Developer"}
		}
		devIDP := httptest.NewServer(idp)
		t.Cleanup(devIDP.Close)

		ok := provisionLocalEMA(ctx, testenv.NewLogger(t), LocalUpstreamOptions{
			ServerURL: serverURL, Encryption: nil,
			DevIDPURL: devIDP.URL, DevIDPClientID: clientID, HTTPClient: devIDP.Client(),
		}, dev)
		require.True(t, ok)

		assertDevIDPWired(t, idp, clientID, fixtureauth.Resource(fixtureauth.BaseURL(serverURL), fixtureauth.PathEMAMCP), devIDP.URL, 4, 4)
		idp.mu.Lock()
		var developers []string
		for id, user := range idp.users {
			if user["email"] == testDeveloperEmail {
				developers = append(developers, id)
			}
		}
		require.Len(t, developers, 1, "the developer's dev-idp user must be found or created once")
		if existingUser {
			require.Equal(t, wantUser, developers[0])
		}
		for key := range idp.assignments {
			require.Contains(t, key, " "+developers[0]+" ", "the assignment must name the developer's dev-idp user")
		}
		idp.mu.Unlock()
	}
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

	idp := newFakeDevIDP("11111111-1111-4111-8111-111111111111")
	devIDP := httptest.NewServer(idp)
	t.Cleanup(devIDP.Close)

	base := fixtureauth.BaseURL(serverURL)
	const clientID = "speakeasy-local-login"
	opts := LocalFixturesOptions{
		DeveloperEmail: testDeveloperEmail,
		Environment:    "local",
		Upstreams: LocalUpstreamOptions{
			ServerURL: serverURL, Encryption: enc,
			DevIDPURL: devIDP.URL, DevIDPClientID: clientID, HTTPClient: devIDP.Client(),
		},
	}

	spec := LocalSpec()
	s := &seeder{spec: spec}
	q := testrepo.New(db)
	resource := fixtureauth.Resource(base, fixtureauth.PathEMAMCP)
	for run := range 2 {
		if run == 1 {
			// Someone edited the dev-idp between seeds: the reseed must put
			// the app, the resource and the trust rule back.
			idp.mu.Lock()
			for _, app := range idp.apps {
				app["client_secret"], app["enabled"] = "edited", false
			}
			for _, res := range idp.resources {
				res["resource_identifier"] = "https://elsewhere.example.test/mcp/ema"
			}
			for _, rule := range idp.rules {
				rule["allowed_scopes"], rule["allowed_client_ids"], rule["enabled"] = "edited", "[]", false
			}
			idp.mu.Unlock()
		}
		seedLocalPostgres(ctx, t, db, spec)
		require.NoError(t, RunLocalFixtures(ctx, testenv.NewLogger(t), db, blob, nil, opts))

		server, err := q.GetRemoteMCPServerURLFixture(ctx, s.det("remotemcp-ema-tracker"))
		require.NoError(t, err)
		require.Equal(t, "Enterprise Sign-In (ID-JAG) Server", server.Name.String)
		require.Equal(t, resource, server.Url)

		wiring, err := q.GetLocalEMAWiringFixture(ctx, s.det("ema-binding-tracker"))
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

		// The server exists only locally, so the local fixtures record its
		// typed auth client on the fixture's endpoint and leave no
		// association for the backfill to decide.
		typedResource, err := q.GetLocalEMATypedAuthClientResourceFixture(ctx, testrepo.GetLocalEMATypedAuthClientResourceFixtureParams{RemoteSessionClientID: s.det("ema-client-tracker"), OrganizationID: spec.OrgID})
		require.NoError(t, err)
		require.Equal(t, resource, typedResource)
		associations, err := associationbackfill.Run(ctx, db, associationbackfill.Options{OrganizationID: spec.OrgID, Apply: false, PageSize: 0})
		require.NoError(t, err)
		require.Zero(t, associations.Decided)

		// After every run, including the one after the edits, the dev-idp
		// holds one app with the shared secret, one resource for the
		// fixture's endpoint, one trust rule and the developer's assignment.
		assertDevIDPWired(t, idp, clientID, resource, devIDP.URL, 1, 1)
	}
}

// assertDevIDPWired checks the dev-idp state the local seed sets up. The trust
// rule is what lets the resource authorization server redeem the dev-idp's
// ID-JAGs for this client.
func assertDevIDPWired(t *testing.T, idp *fakeDevIDP, clientID, resource, devIDPURL string, apps, resources int) {
	t.Helper()
	idp.mu.Lock()
	defer idp.mu.Unlock()
	require.Len(t, idp.apps, apps, "the seed must update its app, not add another")
	for _, app := range idp.apps {
		if app["client_id"] != clientID {
			continue
		}
		require.Equal(t, localEMAClientSecret, app["client_secret"])
		require.Equal(t, true, app["enabled"])
	}
	require.Len(t, idp.resources, resources, "the seed must update its resource, not add another")
	for _, res := range idp.resources {
		if res["slug"] != fixtureauth.EMAResourceSlug {
			continue
		}
		require.Equal(t, resource, res["resource_identifier"])
	}
	require.Len(t, idp.rules, 1)
	for _, rule := range idp.rules {
		require.Equal(t, devIDPURL+devIDPOAuth21Prefix, rule["trusted_issuer"])
		require.Equal(t, `["`+clientID+`"]`, rule["allowed_client_ids"])
		require.Equal(t, fixtureauth.EMAScope, rule["allowed_scopes"])
		require.Equal(t, true, rule["enabled"])
	}
	require.Len(t, idp.assignments, 1)
	for _, scopes := range idp.assignments {
		require.Equal(t, fixtureauth.EMAScope, scopes)
	}
}
