package marketplace_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	adminrepo "github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/marketplace"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/plugins/marketplacetokens"
	"github.com/speakeasy-api/gram/server/internal/plugins/marketplacetokens/marketplacetokenstest"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	ghclient "github.com/speakeasy-api/gram/server/internal/thirdparty/github"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true})
	if err != nil {
		log.Fatalf("launch test infrastructure: %v", err)
	}
	infra = res
	code := m.Run()
	if err := cleanup(); err != nil {
		log.Fatalf("cleanup test infrastructure: %v", err)
	}
	os.Exit(code)
}

// newConnection creates an organization, a project and its GitHub connection
// without a marketplace token, and returns the database, organization and
// project.
func newConnection(t *testing.T) (*pgxpool.Pool, string, uuid.UUID) {
	t.Helper()

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	orgID := "org_" + uuid.NewString()
	_, err = orgrepo.New(conn).UpsertOrganizationMetadata(t.Context(), orgrepo.UpsertOrganizationMetadataParams{
		ID:          orgID,
		Name:        "Marketplace Resolver Org",
		Slug:        "marketplace-resolver-" + uuid.NewString(),
		WorkosID:    conv.ToPGText(orgID),
		Whitelisted: pgtype.Bool{Bool: false, Valid: false},
	})
	require.NoError(t, err)

	project, err := projectsrepo.New(conn).CreateProject(t.Context(), projectsrepo.CreateProjectParams{
		Name:           "Marketplace Resolver Project",
		Slug:           "marketplace-resolver-" + uuid.NewString()[:8],
		OrganizationID: orgID,
	})
	require.NoError(t, err)

	_, err = pluginsrepo.New(conn).UpsertGitHubConnection(t.Context(), pluginsrepo.UpsertGitHubConnectionParams{
		ProjectID:                project.ID,
		OrganizationID:           conv.ToPGText(orgID),
		InstallationID:           1,
		RepoOwner:                "resolver-owner",
		RepoName:                 "resolver-" + uuid.NewString(),
		PublishedMcpFingerprints: nil,
		PublishedHooksVersion:    pgtype.Text{String: "", Valid: false},
		PublishedHooksConfig:     nil,
	})
	require.NoError(t, err)

	return conn, orgID, project.ID
}

func TestDBResolverResolvesAHashedToken(t *testing.T) {
	t.Parallel()

	conn, _, projectID := newConnection(t)
	token, err := marketplacetokens.Mint()
	require.NoError(t, err)
	marketplacetokenstest.Plant(t, conn, projectID, token)

	upstream, err := marketplace.NewLocalDBResolver(testenv.NewLogger(t), conn).Resolve(t.Context(), token)
	require.NoError(t, err)
	require.Equal(t, "resolver-owner", upstream.Owner)
}

func TestDBResolverResolvesAPlaintextTokenTheBackfillHasNotMoved(t *testing.T) {
	t.Parallel()

	conn, _, projectID := newConnection(t)
	token, err := marketplacetokens.Mint()
	require.NoError(t, err)
	marketplacetokenstest.PlantLegacy(t, conn, projectID, token)

	upstream, err := marketplace.NewLocalDBResolver(testenv.NewLogger(t), conn).Resolve(t.Context(), token)
	require.NoError(t, err, "install URLs handed out before tokens were hashed keep working")
	require.Equal(t, "resolver-owner", upstream.Owner)
}

func TestDBResolverRefusesATokenWhileItsOrganizationIsDisabled(t *testing.T) {
	t.Parallel()

	conn, orgID, projectID := newConnection(t)
	token, err := marketplacetokens.Mint()
	require.NoError(t, err)
	marketplacetokenstest.Plant(t, conn, projectID, token)
	resolver := marketplace.NewLocalDBResolver(testenv.NewLogger(t), conn)

	_, err = adminrepo.New(conn).AdminDisableOrganization(t.Context(), orgID)
	require.NoError(t, err)
	_, err = resolver.Resolve(t.Context(), token)
	require.ErrorIs(t, err, marketplace.ErrNotFound)

	_, err = adminrepo.New(conn).AdminEnableOrganization(t.Context(), orgID)
	require.NoError(t, err)
	upstream, err := resolver.Resolve(t.Context(), token)
	require.NoError(t, err, "re-enabling the organization restores the same install URL")
	require.Equal(t, "resolver-owner", upstream.Owner)
}

func TestDBResolverRejectsAnUnknownToken(t *testing.T) {
	t.Parallel()

	conn, _, projectID := newConnection(t)
	token, err := marketplacetokens.Mint()
	require.NoError(t, err)
	marketplacetokenstest.Plant(t, conn, projectID, token)
	other, err := marketplacetokens.Mint()
	require.NoError(t, err)

	_, err = marketplace.NewLocalDBResolver(testenv.NewLogger(t), conn).Resolve(t.Context(), other)
	require.ErrorIs(t, err, marketplace.ErrNotFound)

	// A token's hash is not itself a token: a URL built from a leaked hash
	// does not resolve.
	_, err = marketplace.NewLocalDBResolver(testenv.NewLogger(t), conn).Resolve(t.Context(), marketplacetokens.Hash(token))
	require.ErrorIs(t, err, marketplace.ErrNotFound)
}

type githubTransport func(*http.Request) (*http.Response, error)

func (f githubTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestMarketplaceProxyRequiresVerifiedRepositoryBinding(t *testing.T) {
	t.Parallel()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	privateKey := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})

	for _, tt := range []struct {
		name               string
		installBinding     int64
		githubRepositoryID int64
		wantStatus         int
		wantRequests       []string
	}{
		{"missing binding", 0, 0, http.StatusNotFound, nil},
		{"mismatched installation", 2, 0, http.StatusNotFound, nil},
		{"replacement repository", 1, 99, http.StatusInternalServerError, []string{"mint", "verify"}},
		{"verified binding", 1, 42, http.StatusOK, []string{"mint", "verify", "download"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			db, _, projectID := newConnection(t)
			queries := pluginsrepo.New(db)
			conn, err := queries.GetGitHubConnection(t.Context(), projectID)
			require.NoError(t, err)
			token, err := marketplacetokens.Mint()
			require.NoError(t, err)
			marketplacetokenstest.Plant(t, db, projectID, token)
			if tt.installBinding != 0 {
				require.NoError(t, queries.InsertPluginRepositoryBinding(t.Context(), pluginsrepo.InsertPluginRepositoryBindingParams{
					ProjectID: projectID, RepositoryID: 42, InstallationID: tt.installBinding,
					RepoOwner: conn.RepoOwner, RepoName: conn.RepoName,
				}))
			}

			var requests []string
			httpClient := &http.Client{Transport: githubTransport(func(req *http.Request) (*http.Response, error) {
				var status int
				var body string
				switch req.URL.Host + req.URL.Path {
				case "api.github.com/app/installations/1/access_tokens":
					requests = append(requests, "mint")
					payload, err := io.ReadAll(req.Body)
					require.NoError(t, err)
					require.JSONEq(t, `{"repository_ids":[42]}`, string(payload))
					status = http.StatusCreated
					body = fmt.Sprintf(`{"token":"scoped-token","expires_at":%q}`, time.Now().Add(time.Hour).Format(time.RFC3339))
				case "api.github.com/repos/" + conn.RepoOwner + "/" + conn.RepoName:
					requests = append(requests, "verify")
					require.Equal(t, "token scoped-token", req.Header.Get("Authorization"))
					status = http.StatusOK
					payload, err := json.Marshal(map[string]any{"id": tt.githubRepositoryID, "full_name": conn.RepoOwner + "/" + conn.RepoName, "private": true, "visibility": "private"})
					require.NoError(t, err)
					body = string(payload)
				case "github.com/" + conn.RepoOwner + "/" + conn.RepoName + ".git/info/refs":
					requests = append(requests, "download")
					user, password, ok := req.BasicAuth()
					require.True(t, ok)
					require.Equal(t, "x-access-token", user)
					require.Equal(t, "scoped-token", password)
					status = http.StatusOK
					body = "git refs"
				default:
					t.Fatalf("unexpected outbound request: %s %s", req.Method, req.URL)
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}, nil
			})}
			github, err := ghclient.NewClient(1, privateKey, httpClient, testenv.NewTracerProvider(t))
			require.NoError(t, err)
			logger := testenv.NewLogger(t)
			server := marketplace.NewServer(marketplace.NewDBResolver(logger, db, github), httpClient, logger)
			rec := httptest.NewRecorder()
			server.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
				marketplace.RoutePrefix+token+".git/info/refs?service=git-upload-pack", nil))
			require.Equal(t, tt.wantStatus, rec.Code)
			require.Equal(t, tt.wantRequests, requests)
		})
	}
}
