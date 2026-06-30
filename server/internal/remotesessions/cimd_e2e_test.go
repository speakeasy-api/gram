// cimd_e2e_test.go drives the full outbound CIMD round-trip against a live
// dev-idp acting as the upstream OAuth 2.1 authorization server. Unlike the
// httptest-mock tests in cimd_test.go (which only assert what Gram *sends*),
// this exercises the load-bearing CIMD behavior: the AS dereferences Gram's
// hosted client metadata document URL and accepts it as the client_id.
//
// Setup: one httptest server serves Gram's HandleClientMetadataDocument and is
// the ChallengeManager's serverURL, so the document's redirect_uris match the
// outbound redirect_uri and the dev-idp can fetch the document over localhost.

package remotesessions_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/dev-idp/pkg/devidptest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// httpGetNoFollow GETs a URL without following redirects, so the test can read
// the upstream /authorize 302 Location (carrying ?code).
func httpGetNoFollow(t *testing.T, rawURL string) *http.Response {
	t.Helper()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Get(rawURL) //nolint:noctx // test helper
	require.NoError(t, err)
	return resp
}

func TestCIMD_OutboundRoundTripAgainstDevIDP(t *testing.T) {
	t.Parallel()

	idp := devidptest.Launch(t, devidptest.LaunchOpts{})
	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	enc := testenv.NewEncryptionClient(t)
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
	require.NoError(t, err)

	// One server serves Gram's CIMD document and is the ChallengeManager's
	// serverURL, so the document's redirect_uris match the outbound redirect_uri
	// and the dev-idp can fetch the document over localhost. mgr is bound after
	// the server starts (closure captures it), resolving the URL chicken-and-egg.
	var mgr *remotesessions.ChallengeManager
	router := chi.NewRouter()
	router.Get("/.well-known/oauth-client/{id}", func(w http.ResponseWriter, r *http.Request) {
		oops.ErrHandle(testenv.NewLogger(t), mgr.HandleClientMetadataDocument).ServeHTTP(w, r)
	})
	gramSrv := httptest.NewServer(router)
	t.Cleanup(gramSrv.Close)

	// A real Redis-backed cache is required: BuildAuthorizationUrl writes the
	// RemoteLoginState that HandleRemoteLoginCallback reads back, so the
	// NoopCache-wired newCIMDChallengeManager helper would drop it mid-flow.
	mgr = remotesessions.NewChallengeManager(testenv.NewLogger(t), ti.conn, enc, policy, ti.redisCache, mustURL(t, gramSrv.URL))

	// Issuer pointing at the dev-idp's oauth2-1 mode, advertising CIMD support.
	q := repo.New(ti.conn)
	issuerID := createCIMDIssuer(t, ctx, ti, "cimd-e2e", idp.OAuth21URL+"/authorize", idp.OAuth21URL+"/token")

	userIssuer := createUserSessionIssuer(t, ctx, ti.conn, "cimd-e2e-usi")

	// Seed a CIMD client whose id is the one embedded in its document URL, so
	// the document handler resolves it. This is what createCimd produces; seeded
	// directly here so the URL can be a server the dev-idp can actually reach.
	clientID := uuid.Must(uuid.NewV7())
	docURL := gramSrv.URL + "/.well-known/oauth-client/" + clientID.String()
	client, err := q.CreateRemoteSessionClientCIMD(ctx, repo.CreateRemoteSessionClientCIMDParams{
		ID:                    clientID,
		ProjectID:             conv.ToNullUUID(*authCtx.ProjectID),
		RemoteSessionIssuerID: issuerID,
		ClientIDMetadataUri:   docURL,
		ClientIDIssuedAt:      conv.ToPGTimestamptz(time.Now().UTC()),
		Scope:                 nil,
		Audience:              pgtype.Text{},
	})
	require.NoError(t, err)
	require.Equal(t, docURL, client.ClientID, "CIMD client_id is the document URL")

	require.NoError(t, q.AttachRemoteSessionClientToUserSessionIssuer(ctx, repo.AttachRemoteSessionClientToUserSessionIssuerParams{
		RemoteSessionClientID: client.ID,
		UserSessionIssuerID:   userIssuer,
	}))

	clients, err := mgr.ListClients(ctx, *authCtx.ProjectID, userIssuer)
	require.NoError(t, err)
	require.Len(t, clients, 1)

	subject := urn.NewUserSubject("cimd-e2e-subject")
	authURL, err := mgr.BuildAuthorizationUrl(ctx, remotesessions.ParentChallenge{
		ID:                  uuid.NewString(),
		ProjectID:           *authCtx.ProjectID,
		UserSessionIssuerID: userIssuer,
		Subject:             &subject,
		McpSlug:             "cimd-e2e",
	}, clients[0])
	require.NoError(t, err)

	parsed, err := url.Parse(authURL)
	require.NoError(t, err)
	require.Equal(t, docURL, parsed.Query().Get("client_id"), "outbound client_id is the CIMD document URL")

	// The dev-idp /authorize fetches the document from gramSrv, validates that
	// its client_id and redirect_uris match, and redirects with a code — a 302
	// (not a 400) proves the AS dereferenced and accepted Gram's CIMD document.
	resp := httpGetNoFollow(t, authURL)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusFound, resp.StatusCode, "dev-idp must accept the CIMD URL client_id after fetching the document")

	loc, err := url.Parse(resp.Header.Get("Location"))
	require.NoError(t, err)
	code := loc.Query().Get("code")
	state := loc.Query().Get("state")
	require.NotEmpty(t, code, "upstream redirect must carry ?code")
	require.NotEmpty(t, state, "upstream redirect must carry ?state")

	// Gram exchanges the code at the dev-idp /token using the CIMD URL as
	// client_id and no secret (token_endpoint_auth_method=none), then persists
	// the remote_session.
	cbReq := httptest.NewRequest(http.MethodGet, "/mcp/remote_login_callback?code="+url.QueryEscape(code)+"&state="+url.QueryEscape(state), nil)
	cbW := httptest.NewRecorder()
	require.NoError(t, mgr.HandleRemoteLoginCallback(cbW, cbReq))
	require.Equal(t, http.StatusSeeOther, cbW.Code, "callback should redirect after a successful token exchange")

	sess, err := q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{
		SubjectUrn:            subject,
		RemoteSessionClientID: client.ID,
	})
	require.NoError(t, err)
	require.True(t, sess.AccessExpiresAt.Valid)
	require.True(t, sess.AccessExpiresAt.Time.After(time.Now()), "access_expires_at must be in the future")
}
