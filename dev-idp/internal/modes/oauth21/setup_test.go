package oauth21

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/dev-idp/internal/bootstrap"
	"github.com/speakeasy-api/gram/dev-idp/internal/config"
	"github.com/speakeasy-api/gram/dev-idp/internal/database/repo"
	"github.com/speakeasy-api/gram/dev-idp/internal/keystore"
)

// testExternalURL is the base URL DB-backed handlers in this package are
// built with. Resource authorization server issuer identifiers derive from
// it, so mint tests can name an audience without a live listener.
const testExternalURL = "https://idp.example.com"

// dbHandler is a Handler wired to a fresh in-memory database, plus the
// handles a test needs to seed fixtures and read back what was written.
type dbHandler struct {
	*Handler
	db      *sql.DB
	queries *repo.Queries
}

// newDBHandler boots an in-memory dev-idp database, applies the schema, and
// returns a Handler bound to it. Each call is fully isolated.
func newDBHandler(t *testing.T) *dbHandler {
	t.Helper()

	logger := newTestLogger(t)
	db, err := bootstrap.Open(t.Context(), config.DB{Mode: config.DBModeMemory, Path: ""}, logger)
	require.NoError(t, err, "open in-memory dev-idp database")
	t.Cleanup(func() { _ = db.Close() })

	ks, err := keystore.New(nil, logger)
	require.NoError(t, err, "init keystore")

	h := NewHandler(Config{ExternalURL: testExternalURL, LoginClientID: ""}, ks, logger, newTestTracer(t), db)
	return &dbHandler{Handler: h, db: db, queries: repo.New(db)}
}

// postForm drives the handler's own mux so tests exercise routing and body
// limits the same way a real request would.
func (h *dbHandler) postForm(t *testing.T, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.Handler.Handler().ServeHTTP(rec, req)
	return rec
}

// seedUser creates a user row and returns it.
func (h *dbHandler) seedUser(t *testing.T, email string) repo.User {
	t.Helper()
	user, err := h.queries.CreateUser(t.Context(), repo.CreateUserParams{
		ID:           uuid.New(),
		Email:        email,
		DisplayName:  email,
		PhotoUrl:     sql.NullString{String: "", Valid: false},
		GithubHandle: sql.NullString{String: "", Valid: false},
		Admin:        false,
		Whitelisted:  true,
	})
	require.NoError(t, err, "seed user")
	return user
}

// seedApp registers an enterprise-managed authorization requesting app.
func (h *dbHandler) seedApp(t *testing.T, clientID, secret string) repo.EmaApp {
	t.Helper()
	app, err := h.queries.CreateEmaApp(t.Context(), repo.CreateEmaAppParams{
		ID:           uuid.New(),
		ClientID:     clientID,
		ClientSecret: secret,
		Name:         clientID,
		Enabled:      true,
	})
	require.NoError(t, err, "seed ema app")
	return app
}

// seedResource registers a resource app. The returned row's slug determines
// the audience value a mint request must send.
func (h *dbHandler) seedResource(t *testing.T, slug, resourceIdentifier string) repo.EmaResource {
	t.Helper()
	resource, err := h.queries.CreateEmaResource(t.Context(), repo.CreateEmaResourceParams{
		ID:                 uuid.New(),
		Slug:               slug,
		Name:               slug,
		ResourceIdentifier: resourceIdentifier,
	})
	require.NoError(t, err, "seed ema resource")
	return resource
}

// seedAssignment grants a user the right to drive an app against a resource.
func (h *dbHandler) seedAssignment(t *testing.T, app repo.EmaApp, user repo.User, resource repo.EmaResource, scopes string) repo.EmaAppAssignment {
	t.Helper()
	assignment, err := h.queries.CreateEmaAppAssignment(t.Context(), repo.CreateEmaAppAssignmentParams{
		ID:            uuid.New(),
		AppID:         app.ID,
		UserID:        user.ID,
		ResourceID:    resource.ID,
		GrantedScopes: scopes,
	})
	require.NoError(t, err, "seed ema assignment")
	return assignment
}

// signIDToken mints an id_token for a user exactly as the authorization_code
// grant would, so mint tests can present a real subject_token.
func (h *dbHandler) signIDToken(t *testing.T, user repo.User) string {
	t.Helper()
	token, err := h.Handler.signIDToken(t.Context(), h.queries, user.ID, "test-login-client")
	require.NoError(t, err, "sign id_token")
	return token
}

// seedRefreshToken writes an active refresh token for a user.
func (h *dbHandler) seedRefreshToken(t *testing.T, user repo.User) string {
	t.Helper()
	value := "refresh-" + uuid.NewString()
	_, err := h.queries.CreateToken(t.Context(), repo.CreateTokenParams{
		Token:     value,
		UserID:    user.ID,
		ClientID:  "test-login-client",
		Kind:      "refresh_token",
		Scope:     sql.NullString{String: "", Valid: false},
		ExpiresAt: time.Now().Add(time.Hour),
	})
	require.NoError(t, err, "seed refresh token")
	return value
}
