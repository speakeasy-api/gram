package mcpregistry

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/speakeasy-api/gram/server/internal/platformmcp/localfixture"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"

	"github.com/google/uuid"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpregistry/repo"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"
)

//nolint:paralleltest,tparallel // Subtests share mutable catalog rows and reattach the same service; sequencing is intentional.
func TestDiscoveryRealCredentialsHTTP(t *testing.T) {
	t.Parallel()
	ctx, s, db := newTestService(t)
	logger := testenv.NewLogger(t)
	tracer := testenv.NewTracerProvider(t)
	redis, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	sessions := testenv.NewTestManager(t, logger, tracer, db, redis, cache.Suffix("registry-http"), billing.NewStubClient(logger, tracer))
	fixture := testenv.InitAuthContext(t, ctx, db, sessions)
	ac, ok := contextvalues.GetAuthContext(fixture)
	require.True(t, ok)
	selectors, err := authz.NewSelector(authz.ScopeProjectRead, ac.ProjectID.String()).MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(db).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{OrganizationID: ac.ActiveOrganizationID, PrincipalUrn: urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID), Scope: string(authz.ScopeProjectRead), Selectors: selectors})
	require.NoError(t, err)
	az := authz.NewEngine(logger, db, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
	m := goahttp.NewMuxer()
	require.NoError(t, s.AttachDiscovery(ctx, m, true, auth.New(logger, db, sessions, az), az))
	handler := middleware.SessionMiddleware(m)
	makeKey := func(scopes []string) string {
		key := "gram_local_" + uuid.NewString()
		hash, err := auth.GetAPIKeyHash(key)
		require.NoError(t, err)
		_, err = keysrepo.New(db).CreateAPIKey(ctx, keysrepo.CreateAPIKeyParams{OrganizationID: ac.ActiveOrganizationID, ProjectID: uuid.NullUUID{UUID: *ac.ProjectID, Valid: true}, CreatedByUserID: ac.UserID, Name: "discovery-" + uuid.NewString(), KeyPrefix: key[:16], KeyHash: hash, Scopes: scopes})
		require.NoError(t, err)
		return key
	}
	key := makeKey([]string{"producer"})
	insufficient := makeKey([]string{"chat"})
	send := func(path, key, cookie string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", path, nil)
		r.Header.Set("Gram-Project", *ac.ProjectSlug)
		if key != "" {
			r.Header.Set("Gram-Key", key)
		}
		if cookie != "" {
			r.AddCookie(&http.Cookie{Name: constants.SessionCookie, Value: cookie})
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	for _, tc := range []struct {
		key, cookie string
		status      int
	}{{"", "", 401}, {"invalid", "", 401}, {insufficient, "", 403}, {key, "", 200}, {"", *ac.SessionID, 200}} {
		w := send("/v0.1/servers", tc.key, tc.cookie)
		require.Equal(t, tc.status, w.Code, "cookie=%t key=%t: %s", tc.cookie != "", tc.key != "", w.Body.String())
	}
	for _, suffix := range []string{"", "/io.example%2Ftest/versions", "/io.example%2Ftest/versions/latest"} {
		for _, q := range []string{"?updated_since=", "?updated_since=no", "?updated_since=2026-01-01T00:00:00Z"} {
			w := send("/v0.1/servers"+suffix+q, key, "")
			require.Equal(t, 400, w.Code, w.Body.String())
			require.Contains(t, w.Body.String(), "updated_since")
		}
	}
	for _, q := range []string{"?include_deleted=invalid", "?limit=0", "?limit=101", "?cursor=invalid"} {
		w := send("/v0.1/servers"+q, key, "")
		require.Equal(t, 400, w.Code, w.Body.String())
	}
	for _, q := range []string{"?include_deleted=SECRET", "?limit=SECRET", "?limit=101", "?updated_since=%ZZ"} {
		w := send("/v0.1/servers"+q, key, "")
		require.Equal(t, 400, w.Code)
		require.JSONEq(t, `{"error":"invalid discovery request"}`, w.Body.String())
	}
	oversized := send("/v0.1/servers?search="+strings.Repeat("x", 1025), key, "")
	require.Equal(t, 400, oversized.Code)
	require.JSONEq(t, `{"error":"invalid registry list options"}`, oversized.Body.String())
	id := uuid.New()
	raw := json.RawMessage(`{"server":{"name":"io.example/test","version":"1","description":"synthetic record","extension":9007199254740993},"_meta":{"io.modelcontextprotocol.registry/official":{"status":"deleted"}}}`)
	require.NoError(t, repo.New(db).InsertRegistryEntryFixture(ctx, repo.InsertRegistryEntryFixtureParams{ID: id, Data: raw, Published: true}))
	path := "/v0.1/servers/io.example%2Ftest/versions/"
	w := send(path+"latest", key, "")
	require.Equal(t, 404, w.Code)
	require.JSONEq(t, `{"error":"registry entry not found"}`, w.Body.String())
	w = send(path+"latest?include_deleted=true", key, "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "9007199254740993")
	w = send(path+"old?include_deleted=true", key, "")
	require.Equal(t, 404, w.Code)
	_, err = repo.New(db).SetEntryPublished(ctx, repo.SetEntryPublishedParams{ID: id, Published: false})
	require.NoError(t, err)
	w = send(path+"latest?include_deleted=true", key, "")
	require.Equal(t, 404, w.Code)
	_, err = s.GetByName(ctx, "io.example/test")
	require.NoError(t, err)

	t.Run("historical names and escaped filter cursor bounds", func(t *testing.T) {
		for _, name := range []string{strings.Repeat("a", 7000), strings.Repeat("\n", 2000), "aaa.example/valid-a", "aaa.example/valid-b"} {
			data, err := json.Marshal(map[string]any{"server": map[string]any{"name": name, "description": "test", "version": "1"}})
			require.NoError(t, err)
			require.NoError(t, repo.New(db).InsertRegistryEntryFixture(ctx, repo.InsertRegistryEntryFixtureParams{ID: uuid.New(), Data: data, Published: true}))
		}
		type page struct {
			Servers  []json.RawMessage `json:"servers"`
			Metadata struct {
				NextCursor string `json:"nextCursor"`
			} `json:"metadata"`
		}
		w := send("/v0.1/servers?limit=1", key, "")
		require.Equal(t, 200, w.Code)
		var p page
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &p))
		require.Len(t, p.Servers, 1)
		require.LessOrEqual(t, len(p.Metadata.NextCursor), 8192)
		if p.Metadata.NextCursor != "" {
			w = send("/v0.1/servers?limit=1&cursor="+url.QueryEscape(p.Metadata.NextCursor), key, "")
			require.Equal(t, 200, w.Code, w.Body.String())
		}
		version := strings.Repeat("\x01", 1024)
		for _, name := range []string{"aaa.example/escaped-a", "aaa.example/escaped-b"} {
			data, err := json.Marshal(map[string]any{"server": map[string]any{"name": name, "description": "test", "version": version}})
			require.NoError(t, err)
			require.NoError(t, repo.New(db).InsertRegistryEntryFixture(ctx, repo.InsertRegistryEntryFixtureParams{ID: uuid.New(), Data: data, Published: true}))
		}
		route := "/v0.1/servers?limit=1&version=" + url.QueryEscape(version)
		w = send(route, key, "")
		require.Equal(t, 200, w.Code)
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &p))
		require.NotEmpty(t, p.Metadata.NextCursor)
		require.LessOrEqual(t, len(p.Metadata.NextCursor), 8192)
		w = send(route+"&cursor="+url.QueryEscape(p.Metadata.NextCursor), key, "")
		require.Equal(t, 200, w.Code, w.Body.String())
	})
	t.Run("wrong project credentials", func(t *testing.T) {
		other, err := projectsrepo.New(db).CreateProject(ctx, projectsrepo.CreateProjectParams{Name: "other", Slug: "other", OrganizationID: ac.ActiveOrganizationID})
		require.NoError(t, err)
		for _, cookie := range []bool{false, true} {
			r := httptest.NewRequest("GET", "/v0.1/servers", nil)
			r.Header.Set("Gram-Project", other.Slug)
			if cookie {
				r.AddCookie(&http.Cookie{Name: constants.SessionCookie, Value: *ac.SessionID})
			} else {
				r.Header.Set("Gram-Key", key)
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			require.Contains(t, []int{401, 403, 404}, w.Code, w.Body.String())
		}
	})
	t.Run("current version replacement", func(t *testing.T) {
		_, err := repo.New(db).SetEntryPublished(ctx, repo.SetEntryPublishedParams{ID: id, Published: true})
		require.NoError(t, err)
		_, err = repo.New(db).UpdateEntry(ctx, repo.UpdateEntryParams{StoredRecordLimit: StoredRecordByteLimit, ID: id, Data: []byte(`{"server":{"name":"io.example/test","version":"2","description":"synthetic record"}}`)})
		require.NoError(t, err)
		require.Equal(t, 404, send(path+"1", key, "").Code)
		w := send(path+"latest", key, "")
		require.Equal(t, 200, w.Code)
		require.JSONEq(t, `{"server":{"name":"io.example/test","version":"2","description":"synthetic record"}}`, w.Body.String())
		w = send(strings.TrimSuffix(path, "/"), key, "")
		require.Equal(t, 200, w.Code)
		var page struct {
			Servers []json.RawMessage `json:"servers"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page))
		require.Len(t, page.Servers, 1)
	})
	t.Run("live pagination", func(t *testing.T) {
		insert := func(name string) uuid.UUID {
			id := uuid.New()
			data, err := json.Marshal(map[string]any{"server": map[string]any{"name": name, "version": "1", "description": "synthetic record"}})
			require.NoError(t, err)
			require.NoError(t, repo.New(db).InsertRegistryEntryFixture(ctx, repo.InsertRegistryEntryFixtureParams{ID: id, Data: data, Published: true}))
			return id
		}
		insert("io.example/live-b")
		last := insert("io.example/live-d")
		type page struct {
			Servers []struct {
				Server struct {
					Name string `json:"name"`
				} `json:"server"`
			} `json:"servers"`
			Metadata struct {
				Count      int    `json:"count"`
				NextCursor string `json:"nextCursor"`
			} `json:"metadata"`
		}
		first := send("/v0.1/servers?search=io.example/live&limit=1", key, "")
		require.Equal(t, 200, first.Code)
		var p page
		require.NoError(t, json.Unmarshal(first.Body.Bytes(), &p))
		require.Len(t, p.Servers, 1)
		require.NotEmpty(t, p.Metadata.NextCursor)
		// Writes behind the cursor require a restart; ahead-of-cursor membership
		// changes remain live. This is explicitly not a snapshot or removal feed.
		insert("io.example/live-a")
		insert("io.example/live-c")
		_, err := repo.New(db).SetEntryPublished(ctx, repo.SetEntryPublishedParams{ID: last, Published: false})
		require.NoError(t, err)
		next := send("/v0.1/servers?search=io.example/live&limit=1&cursor="+url.QueryEscape(p.Metadata.NextCursor), key, "")
		require.Equal(t, 200, next.Code)
		var q page
		require.NoError(t, json.Unmarshal(next.Body.Bytes(), &q))
		require.Len(t, q.Servers, 1)
		require.Equal(t, "io.example/live-c", q.Servers[0].Server.Name)
		require.NotEqual(t, p.Servers[0].Server.Name, q.Servers[0].Server.Name)
		require.Empty(t, q.Metadata.NextCursor)
		restart := send("/v0.1/servers?search=io.example/live&limit=1", key, "")
		require.NoError(t, json.Unmarshal(restart.Body.Bytes(), &p))
		require.Equal(t, "io.example/live-a", p.Servers[0].Server.Name)
	})
	t.Run("fixture coexistence", func(t *testing.T) {
		for _, enabled := range []bool{false, true} {
			t.Run(fmt.Sprint(enabled), func(t *testing.T) {
				mux := goahttp.NewMuxer()
				require.NoError(t, s.AttachDiscovery(ctx, mux, enabled, auth.New(logger, db, sessions, az), az))
				origin, err := url.Parse("https://fixture.example.com")
				require.NoError(t, err)
				config, err := localfixture.NewConfig(origin)
				require.NoError(t, err)
				prefix := ""
				if enabled {
					prefix = "/platform-mcp/local-fixture/registry"
					config.SetRegistryPrefix(prefix)
				}
				registryHandler := localfixture.NewRegistryHTTP(config).Handler()
				if prefix != "" {
					registryHandler = http.StripPrefix(prefix, registryHandler)
				}
				mux.Handle("GET", prefix+"/v0.1/servers", registryHandler.ServeHTTP)
				mux.Handle("GET", config.RegistryDetailsPath(), registryHandler.ServeHTTP)
				require.Equal(t, origin.String()+prefix, config.Registry().URL)
				for _, route := range []string{prefix + "/v0.1/servers?version=latest&limit=50", config.RegistryDetailsPath()} {
					w := httptest.NewRecorder()
					mux.ServeHTTP(w, httptest.NewRequest("GET", route, nil))
					require.Equal(t, 200, w.Code, w.Body.String())
					require.Contains(t, w.Body.String(), localfixture.CanonicalRef)
				}
				if enabled {
					w := httptest.NewRecorder()
					mux.ServeHTTP(w, httptest.NewRequest("GET", "/v0.1/servers", nil))
					require.Equal(t, 401, w.Code)
				}
			})
		}
	})
	t.Run("generated dashboard SDK against real server", func(t *testing.T) {
		if _, err := exec.LookPath("mise"); err != nil {
			t.Fatal("dashboard SDK wire test requires mise")
		}
		root, err := filepath.Abs("../../..")
		require.NoError(t, err)
		probeCtx, probeCancel := context.WithTimeout(ctx, 15*time.Second)
		defer probeCancel()
		probe := exec.CommandContext(probeCtx, "mise", "exec", "--", "aube", "exec", "--no-install", "tsx", "--", "--version")
		probe.Dir = root
		if output, err := probe.CombinedOutput(); err != nil {
			t.Fatalf("dashboard SDK wire test requires installed tsx (aube install): %v: %s", err, output)
		}

		for _, name := range []string{"io.example/sdk-a", "io.example/sdk-z"} {
			data, err := json.Marshal(map[string]any{"server": map[string]any{"name": name, "version": "v/1+2", "description": "synthetic record", "extension": map[string]any{"nested": "retained"}}, "_meta": map[string]any{"extension": "retained"}})
			require.NoError(t, err)
			require.Empty(t, s.validator.Validate(data))
			require.NoError(t, repo.New(db).InsertRegistryEntryFixture(ctx, repo.InsertRegistryEntryFixtureParams{ID: uuid.New(), Data: data, Published: true}))
		}
		other, err := projectsrepo.New(db).CreateProject(ctx, projectsrepo.CreateProjectParams{Name: "wire-other", Slug: "wire-other", OrganizationID: ac.ActiveOrganizationID})
		require.NoError(t, err)
		server := httptest.NewServer(handler)
		defer server.Close()
		commandCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
		defer cancel()
		cmd := exec.CommandContext(commandCtx, "mise", "exec", "--", "aube", "exec", "--no-install", "tsx", "--", "client/dashboard/scripts/registry-discovery-wire.ts")
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "REGISTRY_TEST_URL="+server.URL, "REGISTRY_TEST_KEY="+key, "REGISTRY_TEST_PROJECT="+*ac.ProjectSlug, "REGISTRY_TEST_OTHER_PROJECT="+other.Slug, "REGISTRY_TEST_INSUFFICIENT_KEY="+insufficient)
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, string(output))
		t.Log(string(output))
	})

}
