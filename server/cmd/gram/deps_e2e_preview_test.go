package gram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/e2econtrol"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// The preview path end to end against a real database, cache and session
// manager: the reset creates the pull request's own organization and runner
// beside another tenant, the link signs that runner in, the session cannot
// leave the organization, and the next reset ends it.
func TestE2EControlOnAPreviewResetsAndSignsInToItsOwnTenantOnly(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	logger := testenv.NewLogger(t)

	infra, cleanup, err := testenv.Launch(ctx, testenv.LaunchOptions{Postgres: true, Redis: true, ClickHouse: false, Temporal: false, Presidio: false})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, cleanup()) })
	db, err := infra.CloneTestDatabase(t, "e2e_control_preview")
	require.NoError(t, err)
	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	manager := testenv.NewTestManager(t, logger, testenv.NewTracerProvider(t), db, redisClient, cache.Suffix("e2e-control-preview"), nil)

	// Another tenant with a signed-in member, and the built-in roles a
	// preview's database has from its environment.
	neighbour := testenv.InitAuthContext(t, ctx, db, manager)
	neighbourAuth, ok := contextvalues.GetAuthContext(neighbour)
	require.True(t, ok)
	require.NoError(t, authz.SeedSystemRoleGrants(ctx, db, mockidp.MockOrgID))

	siteURL, err := url.Parse("https://pr-4242.dev.example.test")
	require.NoError(t, err)
	mux := goahttp.NewMuxer()
	require.NoError(t, attachE2EControl(ctx, logger, mux, e2eControlDeps{
		environment: "dev",
		token:       testE2EControlToken,
		// The clone has a generated name; a preview's is its pull request's.
		databaseName:   func(context.Context) (string, error) { return "gram_preview_pr_4242", nil },
		developerEmail: nil,
		siteURL:        siteURL,
		serverURL:      siteURL,
		issuerURL:      "https://issuer.example.test",
		db:             db,
		clickhouse:     nil,
		blob:           assetstest.NewTestBlobStore(t),
		encryption:     testenv.NewEncryptionClient(t),
		redis:          redisClient,
		users:          e2eUsers{db: db},
		sessions:       manager,
	}))

	call := func(method, target string) *httptest.ResponseRecorder {
		req := httptest.NewRequestWithContext(ctx, method, target, nil)
		req.Header.Set(e2econtrol.TokenHeader, testE2EControlToken)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}
	signIn := func() *httptest.ResponseRecorder {
		res := call(http.MethodPost, "/rpc/e2e.signInLink")
		require.Equal(t, http.StatusOK, res.Code)
		var link struct {
			Path string `json:"path"`
		}
		require.NoError(t, json.Unmarshal(res.Body.Bytes(), &link))
		return call(http.MethodGet, link.Path)
	}

	// Before the first reset the runner does not exist, so nobody is signed in.
	require.Equal(t, http.StatusForbidden, signIn().Code)

	// The reset is repeatable.
	for range 2 {
		require.Equal(t, http.StatusNoContent, call(http.MethodPost, "/rpc/e2e.reseed").Code)
	}

	res := signIn()
	require.Equal(t, http.StatusSeeOther, res.Code)
	require.Equal(t, "https://pr-4242.dev.example.test/e2e-pr-4242", res.Header().Get("Location"))
	cookies := res.Result().Cookies()
	require.Len(t, cookies, 1)
	require.Equal(t, constants.SessionCookie, cookies[0].Name)
	sessionID := cookies[0].Value

	// The cookie is an ordinary dashboard session for the runner: a member of
	// the preview's organization, not platform staff.
	authenticated, err := manager.Authenticate(ctx, sessionID)
	require.NoError(t, err)
	auth, ok := contextvalues.GetAuthContext(authenticated)
	require.True(t, ok)
	require.Equal(t, "org_e2e_pr_4242", auth.ActiveOrganizationID)
	require.Equal(t, "user_e2e4242_e2e-runner", auth.UserID)
	require.False(t, auth.IsAdmin)
	require.Equal(t, "e2e-pr-4242", auth.OrganizationSlug)

	// It cannot be moved to the shared demo organization or to the neighbour's,
	// the two things auth.enterDemo and auth.switchScopes would do.
	session, err := manager.GetSession(ctx, sessionID)
	require.NoError(t, err)
	for _, organizationID := range []string{constants.DemoOrganizationID, neighbourAuth.ActiveOrganizationID} {
		moved := session
		moved.ActiveOrganizationID = organizationID
		require.ErrorContains(t, manager.UpdateSession(ctx, session, moved), "confined")
	}
	// The neighbour's member is not confined, and still a member of only theirs.
	neighbourSession, err := manager.GetSession(ctx, *neighbourAuth.SessionID)
	require.NoError(t, err)
	demo := neighbourSession
	demo.ActiveOrganizationID = constants.DemoOrganizationID
	require.NoError(t, manager.UpdateSession(ctx, neighbourSession, demo))
	organizations, err := orgrepo.New(db).ListOrganizationsForUser(ctx, conv.ToPGText(neighbourAuth.UserID))
	require.NoError(t, err)
	require.Len(t, organizations, 1)
	require.Equal(t, mockidp.MockOrgID, organizations[0].ID)

	// The next reset ends the runner's earlier sessions.
	require.Equal(t, http.StatusNoContent, call(http.MethodPost, "/rpc/e2e.reseed").Code)
	_, err = manager.Authenticate(ctx, sessionID)
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeUnauthorized, shareable.Code)
	require.Equal(t, http.StatusSeeOther, signIn().Code, "and a new link signs in again")
}
