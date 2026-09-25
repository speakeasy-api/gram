package localaccounts

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/dev-idp/pkg/devidentity"
)

func testGuardConfig(t *testing.T) Config {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	t.Chdir(root)
	for _, f := range []string{".git", "compose.yml", "go.mod", "local/devidp/devidp.db"} {
		name := filepath.Join(root, f)
		require.NoError(t, os.MkdirAll(filepath.Dir(name), 0700))
		require.NoError(t, os.WriteFile(name, nil, 0600))
	}
	return Config{Root: root, Environment: "local", IDPBackend: "local", BillingProvider: "local", DatabaseURL: "postgres://gram:local@127.0.0.1:5439/gram?sslmode=disable", IDPDatabase: "file:" + root + "/local/devidp/devidp.db", IDPURL: "http://localhost:35291/oauth2-1", ExpectedDatabasePort: 5439, ExpectedIDPPort: 35291, RedisAddress: "127.0.0.1:6379", ExpectedRedisPort: 6379, ComposeProject: "gram-test", TemporalAddress: "127.0.0.1:7233", TemporalNamespace: "gram-test", TemporalTaskQueue: "main"}
}

func TestValidateConfig(t *testing.T) { //nolint:paralleltest // Exercises process-wide working directory via t.Chdir.
	c := testGuardConfig(t)
	_, err := validateConfig(c)
	require.NoError(t, err)
	tests := map[string]func(*Config){
		"environment":          func(c *Config) { c.Environment = "production" },
		"implicit backend":     func(c *Config) { c.IDPBackend = "" },
		"upstream backend":     func(c *Config) { c.IDPBackend = "workos" },
		"billing":              func(c *Config) { c.BillingProvider = "stripe" },
		"configured providers": func(c *Config) { c.ExternalProvidersConfigured = true },
		"wrong root":           func(c *Config) { c.Root = filepath.Dir(c.Root) },
		"relative root":        func(c *Config) { c.Root = "." },
		"other idp database":   func(c *Config) { c.IDPDatabase = "file:/tmp/devidp.db" },
		"idp database query":   func(c *Config) { c.IDPDatabase += "?mode=ro" },
		"idp external":         func(c *Config) { c.IDPURL = "https://example.com:35291/oauth2-1" },
		"idp wrong port":       func(c *Config) { c.ExpectedIDPPort++ },
		"idp wrong slot":       func(c *Config) { c.IDPURL = "http://localhost:35291/workos" },
		"idp userinfo":         func(c *Config) { c.IDPURL = "http://user@localhost:35291/oauth2-1" },
		"db port":              func(c *Config) { c.ExpectedDatabasePort++ },
		"missing project":      func(c *Config) { c.ComposeProject = "" },
	}
	for name, mutate := range tests {
		t.Log(name)
		bad := c
		mutate(&bad)
		{
			_, err := validateConfig(bad)
			require.Error(t, err, "unsafe config accepted")
		}
	}
	require.NoError(t, os.Remove(strings.TrimPrefix(c.IDPDatabase, "file:")))
	require.NoError(t, os.Symlink(filepath.Join(c.Root, "go.mod"), strings.TrimPrefix(c.IDPDatabase, "file:")))
	{
		_, err := validateConfig(c)
		require.Error(t, err, "symlinked IdP database accepted")
	}
}

func TestDatabaseGuards(t *testing.T) {
	c := Config{ExpectedDatabasePort: 5439}
	for _, dsn := range []string{
		"postgres://gram:p@127.0.0.1:5439/gram?sslmode=disable",
		"postgres://gram:p@[::1]:5439/gram?sslmode=disable",
		"postgres://gram:p@localhost:5439/gram?sslmode=disable&search_path=public",
	} {
		c.DatabaseURL = dsn
		_, err := databaseConfig(c)
		require.NoError(t, err)
	}
	for _, dsn := range []string{
		"postgres://root:p@localhost:5439/gram?sslmode=disable",
		"postgres://gram:p@localhost:5439/other?sslmode=disable",
		"postgres://gram:p@remote.example:5439/gram?sslmode=disable",
		"postgres://gram:p@localhost:5439/gram?sslmode=disable&host=remote.example",
		"postgres://gram:p@localhost:5439/gram?sslmode=disable&hostaddr=192.0.2.1",
		"postgres://gram:p@localhost:5439/gram?sslmode=disable&service=remote",
		"postgres://gram:p@localhost:5439/gram?sslmode=disable&passfile=/tmp/secret",
		"postgres://gram:p@localhost:5439/gram?sslmode=require",
		"postgres://gram:p@localhost:5439/gram?sslmode=disable&search_path=other",
		"postgres://gram:p@localhost:5439/gram?sslmode=disable&search_path=",
		"postgres://gram:p@localhost:5439/gram?sslmode=disable&search_path=public,other",
		"postgres://gram:p@localhost:5439/gram?sslmode=disable&search_path=public&search_path=other",
		"postgres://gram:p@localhost:5439,5438/gram?sslmode=disable",
		"postgres://gram:p@localhost,remote.example:5439/gram?sslmode=disable",
	} {
		c.DatabaseURL = dsn
		{
			_, err := databaseConfig(c)
			require.Error(t, err, "unsafe database accepted")
		}
	}
	t.Setenv("PGOPTIONS", "-c search_path=other")
	t.Setenv("PGHOST", "remote.example")
	t.Setenv("PGSERVICE", "")
	t.Setenv("PGPASSFILE", "/must/not/read")
	t.Setenv("PGSSLCERT", "/must/not/read")
	c.DatabaseURL = "postgres://gram:p@localhost:5439/gram?sslmode=disable"
	pc, err := databaseConfig(c)
	require.NoError(t, err)
	require.False(t, pc.ConnConfig.RuntimeParams["search_path"] != "public" || pc.ConnConfig.RuntimeParams["options"] != "", "database search path was not pinned independently of environment")
	require.Equal(t, "localhost", pc.ConnConfig.Host, "environment overrode explicit host")
}

func TestSelectedIdentity(t *testing.T) {
	t.Parallel()
	const id = "a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d"
	c := Config{Root: "/worktree", IDPDatabase: "file:/worktree/local/devidp/devidp.db"}
	const proof = `,"provenance":{"backend":"local","worktree_root":"/worktree","database_path":"/worktree/local/devidp/devidp.db"}`
	got, err := decodeSelected(strings.NewReader(`{"mode":"oauth2-1","user":{"id":"`+id+`"}`+proof+`}`), c)
	require.NoError(t, err)
	require.Equal(t, id, got.String())
	missingIdentity := `{` + strings.TrimPrefix(proof, ",") + `}`
	require.True(t, json.Valid([]byte(missingIdentity)))
	_, err = decodeSelected(strings.NewReader(missingIdentity), c)
	require.EqualError(t, err, "invalid selected oauth2-1 identity response")
	for _, body := range []string{`{"mode":"workos","user":{"id":"` + id + `"}}`, `{"mode":"oauth2-1","user":{"id":"bad"}}`, `{"mode":"oauth2-1","user":{"id":"00000000-0000-0000-0000-000000000000"}}`} {
		body = strings.TrimSuffix(body, "}") + proof + "}"
		{
			_, err := decodeSelected(strings.NewReader(body), c)
			require.Error(t, err, "invalid identity accepted")
		}
	}
	wid := devidentity.WorkOSUserID(uuid.MustParse(id))
	target := Target{UserID: "gram-user-not-idp-uuid", OrganizationID: "gram-org-not-external-id", WorkOSUserID: wid, WorkOSOrganizationID: "org_local_test"}
	gotTarget, err := soleTarget([]Target{target}, wid, target.WorkOSOrganizationID)
	require.NoError(t, err)
	require.Equal(t, target, gotTarget)
	for _, targets := range [][]Target{nil, {target, target}, {{UserID: "u", OrganizationID: "o", WorkOSUserID: wid}}} {
		{
			_, err := soleTarget(targets, wid, target.WorkOSOrganizationID)
			require.Error(t, err, "ambiguous or unlinked target accepted")
		}
	}
}

func TestSelectedIdentityRequiresDaemonProvenance(t *testing.T) {
	t.Parallel()
	const id = "a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d"
	const proof = `"backend":"local","worktree_root":"/worktree","database_path":"/worktree/local/devidp/devidp.db"`
	for _, tc := range []struct {
		name  string
		proof string
		want  bool
	}{
		{"matching", proof, true},
		{"missing", "", false},
		{"wrong backend", strings.Replace(proof, `"backend":"local"`, `"backend":"workos"`, 1), false},
		{"wrong root", strings.Replace(proof, `"worktree_root":"/worktree"`, `"worktree_root":"/other"`, 1), false},
		{"wrong database", strings.Replace(proof, `"database_path":"/worktree/local/devidp/devidp.db"`, `"database_path":"/other/devidp.db"`, 1), false},
		{"memory database", strings.Replace(proof, `/worktree/local/devidp/devidp.db`, ``, 1), false},
	} {
		body := `{"mode":"oauth2-1","user":{"id":"` + id + `","email":"fixture@example.test"},"workos":null`
		if tc.proof != "" {
			body += `,"provenance":{` + tc.proof + `}`
		}
		body += `}`
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/rpc/devIdp.getCurrentUser" || r.Method != http.MethodPost {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
		}))
		t.Cleanup(srv.Close)
		u, err := url.Parse(srv.URL)
		require.NoError(t, err)
		port, err := strconv.ParseUint(u.Port(), 10, 16)
		require.NoError(t, err)
		c := Config{Root: "/worktree", IDPDatabase: "file:/worktree/local/devidp/devidp.db", IDPURL: srv.URL + "/oauth2-1", ExpectedIDPPort: uint16(port)}
		got, err := selectedUser(t.Context(), c)
		if tc.want {
			require.NoError(t, err, tc.name)
			require.Equal(t, id, got.String())
		} else {
			require.ErrorContains(t, err, "provenance", tc.name)
			require.Equal(t, uuid.Nil, got, tc.name)
		}
	}
}

func TestContainerAndQuiescenceEvidence(t *testing.T) {
	t.Parallel()
	c := Config{Root: "/worktree", ComposeProject: "gram-test", ExpectedDatabasePort: 5439, TemporalAddress: "localhost:7233", TemporalNamespace: "gram-test", TemporalTaskQueue: "main"}
	var e containerEvidence
	require.NoError(t, json.Unmarshal([]byte(`{"Running":true,"Labels":{"com.docker.compose.project":"gram-test","com.docker.compose.service":"gram-db","com.docker.compose.project.working_dir":"/worktree"},"Ports":{"5432/tcp":[{"HostIP":"0.0.0.0","HostPort":"5439"}]}}`), &e))
	require.NoError(t, validateServiceContainer(c, e, "gram-db", "5432/tcp", c.ExpectedDatabasePort))
	e.Labels["com.docker.compose.project.working_dir"] = "/other"
	{
		err := validateServiceContainer(c, e, "gram-db", "5432/tcp", c.ExpectedDatabasePort)
		require.Error(t, err, "wrong worktree accepted")
	}
	require.NoError(t, validateTemporal(c))
	for _, mutate := range []func(*Config){func(c *Config) { c.TemporalNamespace = "default" }, func(c *Config) { c.TemporalNamespace = "other" }, func(c *Config) { c.TemporalAddress = "remote.example:7233" }, func(c *Config) { c.TemporalTaskQueue = "" }} {
		bad := c
		mutate(&bad)
		{
			err := validateTemporal(bad)
			require.Error(t, err, "unsafe Temporal scope accepted")
		}
	}
	require.NoError(t, stoppedDaemon([]byte(`{"name":"worker","pid":null,"status":"stopped"}`), "worker"))
	for _, body := range []string{`{}`, `{"name":"worker","pid":42,"status":"stopped"}`, `{"name":"worker","pid":null,"status":"running"}`, `{"name":"server","pid":null,"status":"stopped"}`} {
		{
			err := stoppedDaemon([]byte(body), "worker")
			require.Error(t, err, "unsafe daemon accepted")
		}
	}
}

func TestRedisGuards(t *testing.T) {
	t.Parallel()
	c := Config{RedisAddress: "127.0.0.1:6379", ExpectedRedisPort: 6379}
	require.NoError(t, validateRedis(c))
	for _, address := range []string{"remote.example:6379", "127.0.0.1:6380", "/tmp/redis.sock", "redis://localhost:6379", "localhost"} {
		bad := c
		bad.RedisAddress = address
		{
			err := validateRedis(bad)
			require.Error(t, err, "unsafe Redis address accepted")
		}
	}
	var e containerEvidence
	require.NoError(t, json.Unmarshal([]byte(`{"Running":true,"Labels":{"com.docker.compose.project":"gram-test","com.docker.compose.service":"gram-cache","com.docker.compose.project.working_dir":"/worktree"},"Ports":{"35299/tcp":[{"HostIP":"0.0.0.0","HostPort":"6379"}]}}`), &e))
	c.Root = "/worktree"
	c.ComposeProject = "gram-test"
	require.NoError(t, validateServiceContainer(c, e, "gram-cache", "35299/tcp", 6379))
	{
		err := validateServiceContainer(c, e, "gram-cache", "35299/tcp", 6380)
		require.Error(t, err, "wrong Redis port accepted")
	}
	e.Labels["com.docker.compose.project"] = "gram-other"
	{
		err := validateServiceContainer(c, e, "gram-cache", "35299/tcp", 6379)
		require.Error(t, err, "wrong Redis project accepted")
	}
}

func TestStoppedWorktreeDaemon(t *testing.T) {
	t.Parallel()
	for _, body := range []string{
		`{"id":"other/worker","namespace":"other","name":"worker","pid":null,"status":"stopped"}`,
		`{"id":"worktree/worker","namespace":"other","name":"worker","pid":null,"status":"stopped"}`,
		`{"name":"worker","pid":null,"status":"stopped"}`,
	} {
		require.Error(t, stoppedWorktreeDaemon([]byte(body), "worktree", "worker"), "foreign or missing namespace accepted")
	}
	require.NoError(t, stoppedWorktreeDaemon([]byte(`{"id":"worktree/worker","namespace":"worktree","name":"worker","pid":null,"status":"stopped"}`), "worktree", "worker"))
}

func TestPrimaryCheckoutUnsupported(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(root, ".git"), 0700))
	_, err := validateConfig(Config{Root: root, ComposeProject: "gram-test", TemporalNamespace: "gram-test"})
	require.ErrorContains(t, err, "initialized secondary worktrees only")
	require.ErrorContains(t, err, "run ./zero there")
}

func TestUninitializedWorktreeUnsupported(t *testing.T) {
	t.Parallel()
	_, err := validateConfig(Config{Root: t.TempDir()})
	require.ErrorContains(t, err, "explicit Compose project and matching non-default Temporal namespace")
}

func TestDefaultTemporalNamespaceUnsupported(t *testing.T) {
	t.Parallel()
	_, err := validateConfig(Config{Root: t.TempDir(), ComposeProject: "gram-test", TemporalNamespace: "default"})
	require.ErrorContains(t, err, "initialized secondary worktrees only")
}

func TestDatabaseServiceRefused(t *testing.T) {
	t.Parallel()
	_, err := databaseConfig(Config{
		DatabaseURL:          "postgres://gram:local@127.0.0.1:5439/gram?sslmode=disable",
		ExpectedDatabasePort: 5439,
		PGService:            "untrusted",
	})
	require.ErrorContains(t, err, "PGSERVICE must be unset")
}

// resolveGuardDB isolates Resolve's PostgreSQL boundary without a live database.
type resolveGuardDB struct {
	Queryer
	target Target
}

func (db resolveGuardDB) Query(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
	return &resolveGuardRows{Rows: nil, target: db.target, read: false}, nil
}

type resolveGuardRows struct {
	pgx.Rows
	target Target
	read   bool
}

func (r *resolveGuardRows) Next() bool {
	if r.read {
		return false
	}
	r.read = true
	return true
}
func (r *resolveGuardRows) Close()     {}
func (r *resolveGuardRows) Err() error { return nil }
func (r *resolveGuardRows) Scan(dest ...any) error {
	values := []string{r.target.UserID, r.target.OrganizationID, r.target.WorkOSUserID, r.target.WorkOSOrganizationID}
	for i, value := range values {
		switch dst := dest[i].(type) {
		case *string:
			*dst = value
		case *pgtype.Text:
			*dst = pgtype.Text{String: value, Valid: true}
		default:
			return fmt.Errorf("unexpected scan destination %T", dst)
		}
	}
	return nil
}

//nolint:paralleltest // Uses t.Chdir and serially changes the isolated IdP fixture for rechecks.
func TestResolveRequiresMatchingIDPMembership(t *testing.T) {
	c := testGuardConfig(t)
	userID := uuid.MustParse("a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d")
	const orgID = "b1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d"
	target := Target{UserID: "gram-user", OrganizationID: "gram-org", WorkOSUserID: devidentity.WorkOSUserID(userID), WorkOSOrganizationID: "org_local_fixture"}
	db := resolveGuardDB{Queryer: nil, target: target}
	membership := fmt.Sprintf(`{"user_id":%q,"organization_id":%q}`, userID.String(), orgID)
	memberships := `{"items":[` + membership + `],"next_cursor":""}`
	organization := fmt.Sprintf(`{"items":[{"id":%q,"workos_id":%q}],"next_cursor":""}`, orgID, target.WorkOSOrganizationID)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/rpc/devIdp.getCurrentUser":
			_, _ = fmt.Fprintf(w, `{"mode":"oauth2-1","user":{"id":%q},"provenance":{"backend":"local","worktree_root":%q,"database_path":%q}}`, userID.String(), c.Root, strings.TrimPrefix(c.IDPDatabase, "file:"))
		case "/rpc/memberships.list":
			var payload struct {
				UserID string `json:"user_id"`
				Limit  int    `json:"limit"`
			}
			if json.NewDecoder(r.Body).Decode(&payload) != nil || payload.UserID != userID.String() || payload.Limit != 2 {
				http.Error(w, "wrong membership filter", 400)
				return
			}
			_, _ = w.Write([]byte(memberships))
		case "/rpc/organizations.list":
			_, _ = w.Write([]byte(organization))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	u, err := url.Parse(srv.URL)
	require.NoError(t, err)
	port, err := strconv.ParseUint(u.Port(), 10, 16)
	require.NoError(t, err)
	c.IDPURL = srv.URL + "/oauth2-1"
	c.ExpectedIDPPort = uint16(port)
	for _, tc := range []struct {
		name, memberships, organization string
		valid                           bool
	}{
		{"valid distinct internal IDs", memberships, organization, true},
		{"cross-store mismatch", memberships, strings.ReplaceAll(organization, "org_local_fixture", "org_other"), false},
		{"no memberships", `{"items":[],"next_cursor":""}`, organization, false},
		{"ambiguous memberships", `{"items":[` + membership + `,` + membership + `],"next_cursor":""}`, organization, false},
		{"more memberships", strings.ReplaceAll(memberships, `"next_cursor":""`, `"next_cursor":"more"`), organization, false},
		{"wrong user", strings.ReplaceAll(memberships, userID.String(), orgID), organization, false},
		{"missing link", memberships, strings.ReplaceAll(organization, "org_local_fixture", ""), false},
		{"missing organization", memberships, `{"items":[],"next_cursor":""}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			memberships, organization = tc.memberships, tc.organization
			got, err := Resolve(t.Context(), db, c)
			if !tc.valid {
				require.Error(t, err)
				require.Empty(t, got)
				return
			}
			require.NoError(t, err)
			require.Equal(t, target, got)
			// The runner calls Resolve again with its transaction before any mutation.
			organization = strings.ReplaceAll(organization, "org_local_fixture", "org_changed")
			_, err = Resolve(t.Context(), db, c)
			require.Error(t, err)
		})
	}
}
