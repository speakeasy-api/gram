// End-to-end coverage of the per-issuer request strategy: scopes, RFC 8707 resource, RFC 9207 iss.

package remotesessions_test

import (
	"context"
	"net/http"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

const syntheticIssuerURL = "https://idp.example.com"

// scopeOf returns the scope parameter on an authorize redirect.
func scopeOf(t *testing.T, authURL string) string {
	t.Helper()
	parsed, err := url.Parse(authURL)
	require.NoError(t, err)
	return parsed.Query().Get("scope")
}

// stateOf returns the state parameter on an authorize redirect.
func stateOf(t *testing.T, authURL string) string {
	t.Helper()
	parsed, err := url.Parse(authURL)
	require.NoError(t, err)
	state := parsed.Query().Get("state")
	require.NotEmpty(t, state)
	return state
}

// scopelessToken answers every exchange with a token response that omits
// scope, so the session records what was requested.
func scopelessToken(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"access_token":"access","refresh_token":"refresh","token_type":"Bearer","expires_in":3600}`))
}

// scopedToken answers every exchange with a token response that names the
// granted scope.
func scopedToken(scope string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"access","token_type":"Bearer","expires_in":3600,"scope":"` + scope + `"}`))
	}
}

func TestRemoteLogin_OpenIDRequestedOnlyWhenAdvertised(t *testing.T) {
	t.Parallel()

	_, advertised := newSyntheticExpiryEnv(t, "openid-advertised", scopelessToken,
		withIssuerScopes("channels:history", "openid"),
		withClientScope("channels:history"),
	)
	require.Equal(t, "channels:history openid", scopeOf(t, advertised.authURL))
	// RFC 6749 §5.1: no scope in the response means the requested scope was
	// granted, and that is what the session carries for the consent page.
	require.Equal(t, []string{"channels:history", "openid"}, advertised.session.Scopes)

	_, silent := newSyntheticExpiryEnv(t, "openid-silent", scopelessToken,
		withIssuerScopes("channels:history"),
		withClientScope("channels:history"),
	)
	require.Equal(t, "channels:history", scopeOf(t, silent.authURL))
	require.Equal(t, []string{"channels:history"}, silent.session.Scopes)
}

func TestRemoteLogin_OfflineAccessRequestedOnlyWhenAdvertised(t *testing.T) {
	t.Parallel()

	_, advertised := newSyntheticExpiryEnv(t, "offline-advertised", scopelessToken,
		withIssuerScopes("read", "offline_access"),
	)
	require.Equal(t, "read offline_access", scopeOf(t, advertised.authURL))
	require.True(t, advertised.session.RefreshTokenEncrypted.Valid, "the refresh token the offline grant returns is stored")

	_, silent := newSyntheticExpiryEnv(t, "offline-silent", scopelessToken,
		withIssuerScopes("read"),
	)
	require.Equal(t, "read", scopeOf(t, silent.authURL))
}

func TestRemoteLogin_ScopeOverrideIsRequestedVerbatim(t *testing.T) {
	t.Parallel()

	_, env := newSyntheticExpiryEnv(t, "scope-override", scopelessToken,
		withIssuerScopes("channels:history", "openid", "offline_access"),
		withClientScope("channels:history"),
		withScopeOverride("custom:one", "custom:two"),
	)
	require.Equal(t, "custom:one custom:two", scopeOf(t, env.authURL))
	require.Equal(t, []string{"custom:one", "custom:two"}, env.session.Scopes)
}

// Empty scopes_supported adds nothing; the NULL case exists only on Client (TestClientRequestedScopes).
func TestRemoteLogin_EmptyIssuerScopesAddNothing(t *testing.T) {
	t.Parallel()

	_, withClient := newSyntheticExpiryEnv(t, "issuer-scopes-empty-client", scopelessToken,
		withIssuerScopes([]string{}...),
		withClientScope("read"),
	)
	require.Equal(t, "read", scopeOf(t, withClient.authURL))
	require.Equal(t, []string{"read"}, withClient.session.Scopes)

	_, alone := newSyntheticExpiryEnv(t, "issuer-scopes-empty-alone", scopelessToken, withIssuerScopes([]string{}...))
	parsed, err := url.Parse(alone.authURL)
	require.NoError(t, err)
	require.False(t, parsed.Query().Has("scope"), "no scope parameter is sent when nothing is configured")
	require.Empty(t, alone.session.Scopes)
}

// RFC 6749 §5.1: a response scope wins; an omitted one means the request was granted as is.
func TestRemoteLoginCallback_StoresResponseScopeOverRequestedScope(t *testing.T) {
	t.Parallel()

	_, granted := newSyntheticExpiryEnv(t, "scope-response-differs", scopedToken("channels:history"),
		withIssuerScopes("channels:history", "openid"),
	)
	require.Equal(t, "channels:history openid", scopeOf(t, granted.authURL))
	require.Equal(t, []string{"channels:history"}, granted.session.Scopes, "the response's scope is what the session records")

	_, omitted := newSyntheticExpiryEnv(t, "scope-response-omitted", scopelessToken,
		withIssuerScopes("channels:history", "openid"),
	)
	require.Equal(t, []string{"channels:history", "openid"}, omitted.session.Scopes, "an omitted scope records the requested set")
}

// A token endpoint that answers invalid_target while the resource parameter
// is present, and succeeds without it.
func resourceRejectingToken(exchanges *atomic.Int64) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		exchanges.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if r.PostForm.Get("resource") != "" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_target","error_description":"resource is not recognised"}`))
			return
		}
		_, _ = w.Write([]byte(`{"access_token":"access","token_type":"Bearer","expires_in":3600}`))
	}
}

// A token endpoint that answers invalid_target to every exchange, resource or
// not.
func alwaysInvalidTargetToken(exchanges *atomic.Int64) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		exchanges.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_target","error_description":"no"}`))
	}
}

// retryLocation asserts the callback answered with a fresh authorize redirect
// that omits the resource parameter, and returns its state.
func retryLocation(t *testing.T, w interface{ Header() http.Header }) string {
	t.Helper()
	loc, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, syntheticIssuerURL+"/authorize", loc.Scheme+"://"+loc.Host+loc.Path)
	require.Empty(t, loc.Query().Get("resource"), "the retry leg must not send resource")
	state := loc.Query().Get("state")
	require.NotEmpty(t, state)
	return state
}

// resourceIndicatorFlag reads the issuer's stored resource_indicator_supported.
func resourceIndicatorFlag(t *testing.T, ctx context.Context, env syntheticExpiryEnv) pgtype.Bool {
	t.Helper()
	return issuerResourceIndicatorFlag(t, ctx, env.q, env.issuerID, env.projectID, env.organizationID)
}

func issuerResourceIndicatorFlag(t *testing.T, ctx context.Context, q *repo.Queries, issuerID, projectID uuid.UUID, organizationID string) pgtype.Bool {
	t.Helper()
	issuer, err := q.GetRemoteSessionIssuerByID(ctx, repo.GetRemoteSessionIssuerByIDParams{
		ID:                    issuerID,
		ProjectID:             conv.ToNullUUID(projectID),
		OrganizationID:        conv.ToPGText(organizationID),
		IncludeOrganizational: true,
		IncludeGlobal:         true,
	})
	require.NoError(t, err)
	return issuer.ResourceIndicatorSupported
}

func TestRemoteLoginCallback_InvalidTargetAtTokenEndpointRetriesWithoutResource(t *testing.T) {
	t.Parallel()

	const resource = "https://member.example.com/mcp"
	var exchanges atomic.Int64
	ctx, env, first, err := driveSyntheticLogin(t, "invalid-target-token", resourceRejectingToken(&exchanges), withResource(resource))
	require.NoError(t, err, "an invalid_target answer is recovered, not surfaced")
	require.Equal(t, http.StatusSeeOther, first.Code)
	require.Equal(t, int64(1), exchanges.Load())
	state := retryLocation(t, first)
	require.False(t, resourceIndicatorFlag(t, ctx, env).Valid, "nothing is recorded until the retry leg succeeds")

	second, err := env.callback(t, "code=upstream-code-2&state="+url.QueryEscape(state))
	require.NoError(t, err)
	require.Equal(t, http.StatusSeeOther, second.Code)
	require.Contains(t, second.Header().Get("Location"), "/connect?state=", "the retry leg completes the login")
	require.Equal(t, int64(2), exchanges.Load())

	flag := resourceIndicatorFlag(t, ctx, env)
	require.True(t, flag.Valid)
	require.False(t, flag.Bool, "the token endpoint's own invalid_target is what gets recorded")

	session, err := env.q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{
		SubjectUrn:            env.subject,
		RemoteSessionClientID: env.clientID,
	})
	require.NoError(t, err)
	require.Equal(t, resource, session.Resource.String, "the resource is still recorded so the grant stays routable")
}

// A second invalid_target fails the login rather than minting a third leg, and records nothing.
func TestRemoteLoginCallback_InvalidTargetOnRetryLegIsRefused(t *testing.T) {
	t.Parallel()

	const resource = "https://member.example.com/mcp"
	var exchanges atomic.Int64
	ctx, env, first, err := driveSyntheticLogin(t, "invalid-target-retry-leg", alwaysInvalidTargetToken(&exchanges), withResource(resource))
	require.NoError(t, err)
	require.Equal(t, http.StatusSeeOther, first.Code)
	require.Equal(t, int64(1), exchanges.Load())
	state := retryLocation(t, first)

	second, err := env.callback(t, "code=upstream-code-2&state="+url.QueryEscape(state))
	require.Error(t, err)
	require.Empty(t, second.Header().Get("Location"), "no second retry is minted")
	require.Equal(t, int64(2), exchanges.Load())
	require.False(t, resourceIndicatorFlag(t, ctx, env).Valid)

	_, err = env.q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{
		SubjectUrn:            env.subject,
		RemoteSessionClientID: env.clientID,
	})
	require.Error(t, err, "no session is stored for a login that failed")
}

// A leg that never sent a resource has nothing to drop: invalid_target is then
// a denial like any other, from either endpoint.
func TestRemoteLoginCallback_InvalidTargetWithoutResourceIsDenied(t *testing.T) {
	t.Parallel()

	var exchanges atomic.Int64
	ctx, env, w, err := driveSyntheticLogin(t, "invalid-target-no-resource-token", alwaysInvalidTargetToken(&exchanges))
	require.Error(t, err)
	require.Empty(t, w.Header().Get("Location"))
	require.Equal(t, int64(1), exchanges.Load())
	require.False(t, resourceIndicatorFlag(t, ctx, env).Valid)

	ctx, env, w, err = driveSyntheticLogin(t, "invalid-target-no-resource-authorize", alwaysInvalidTargetToken(&exchanges),
		withCallbackQuery(func(q url.Values) {
			q.Del("code")
			q.Set("error", "invalid_target")
		}),
	)
	require.Error(t, err)
	require.Empty(t, w.Header().Get("Location"))
	require.Equal(t, int64(1), exchanges.Load(), "no code was exchanged")
	require.False(t, resourceIndicatorFlag(t, ctx, env).Valid)
}

func TestRemoteLoginCallback_InvalidTargetOnAuthorizeRedirectRetriesOnce(t *testing.T) {
	t.Parallel()

	const resource = "https://member.example.com/mcp"
	var exchanges atomic.Int64
	ctx, env, first, err := driveSyntheticLogin(t, "invalid-target-authorize", resourceRejectingToken(&exchanges),
		withResource(resource),
		withCallbackQuery(func(q url.Values) {
			q.Del("code")
			q.Set("error", "invalid_target")
		}),
	)
	require.NoError(t, err)
	require.Equal(t, http.StatusSeeOther, first.Code)
	require.Equal(t, int64(0), exchanges.Load(), "no code was exchanged")
	state := retryLocation(t, first)
	// A query-string denial is anyone's to craft, so it teaches nothing.
	require.False(t, resourceIndicatorFlag(t, ctx, env).Valid)

	// The retry leg is refused too: that is a denial like any other, never a
	// second retry, and still nothing is recorded.
	second, err := env.callback(t, "error=invalid_target&state="+url.QueryEscape(state))
	require.Error(t, err)
	require.Empty(t, second.Header().Get("Location"))
	require.Equal(t, int64(0), exchanges.Load())
	require.False(t, resourceIndicatorFlag(t, ctx, env).Valid)
}

// A redirect invalid_target is forgeable, so a successful retry leaves the flag unlearned.
func TestRemoteLoginCallback_InvalidTargetOnAuthorizeRedirectLeavesFlagUnlearnedAfterRetrySucceeds(t *testing.T) {
	t.Parallel()

	const resource = "https://member.example.com/mcp"
	var exchanges atomic.Int64
	ctx, env, first, err := driveSyntheticLogin(t, "invalid-target-authorize-ok", resourceRejectingToken(&exchanges),
		withResource(resource),
		withCallbackQuery(func(q url.Values) {
			q.Del("code")
			q.Set("error", "invalid_target")
		}),
	)
	require.NoError(t, err)
	require.Equal(t, http.StatusSeeOther, first.Code)
	state := retryLocation(t, first)
	require.False(t, resourceIndicatorFlag(t, ctx, env).Valid)

	second, err := env.callback(t, "code=upstream-code-2&state="+url.QueryEscape(state))
	require.NoError(t, err)
	require.Equal(t, http.StatusSeeOther, second.Code)
	require.Contains(t, second.Header().Get("Location"), "/connect?state=", "the retry leg completes the login")
	require.Equal(t, int64(1), exchanges.Load())
	require.False(t, resourceIndicatorFlag(t, ctx, env).Valid, "a forged redirect denial never teaches the issuer's flag")

	session, err := env.q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{
		SubjectUrn:            env.subject,
		RemoteSessionClientID: env.clientID,
	})
	require.NoError(t, err)
	require.Equal(t, resource, session.Resource.String)
}

// A platform-catalog issuer is shared across tenants, so a login never writes
// its flag: the retry still completes, and the row stays unlearned.
func TestRemoteLoginCallback_InvalidTargetOnCatalogIssuerRetriesWithoutRecording(t *testing.T) {
	t.Parallel()

	const resource = "https://member.example.com/mcp"
	var exchanges atomic.Int64
	ctx, env, first, err := driveSyntheticLogin(t, "invalid-target-global", resourceRejectingToken(&exchanges),
		withResource(resource),
		withGlobalIssuer(),
	)
	require.NoError(t, err)
	require.Equal(t, http.StatusSeeOther, first.Code)
	state := retryLocation(t, first)

	second, err := env.callback(t, "code=upstream-code-2&state="+url.QueryEscape(state))
	require.NoError(t, err)
	require.Equal(t, http.StatusSeeOther, second.Code)
	require.Contains(t, second.Header().Get("Location"), "/connect?state=")
	require.Equal(t, int64(2), exchanges.Load())
	require.False(t, resourceIndicatorFlag(t, ctx, env).Valid, "catalog rows are never written from a login")

	session, err := env.q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{
		SubjectUrn:            env.subject,
		RemoteSessionClientID: env.clientID,
	})
	require.NoError(t, err)
	require.Equal(t, resource, session.Resource.String)
}

// The callback's flag write reaches the login's own project and its
// organization's rows, and nothing shared or foreign.
func TestSetRemoteSessionIssuerResourceIndicatorSupported_WritesTenantRowsOnly(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	q := repo.New(ti.conn)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	write := func(issuerID uuid.UUID) int64 {
		n, err := q.SetRemoteSessionIssuerResourceIndicatorSupported(ctx, repo.SetRemoteSessionIssuerResourceIndicatorSupportedParams{
			ResourceIndicatorSupported: false,
			ID:                         issuerID,
			ProjectID:                  projectID,
			OrganizationID:             orgID,
		})
		require.NoError(t, err)
		return n
	}

	orgIssuer := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, orgID, "rif-org-tier")
	require.Equal(t, int64(1), write(orgIssuer), "the organization's own row is written")
	flag := issuerResourceIndicatorFlag(t, ctx, q, orgIssuer, projectID, orgID)
	require.True(t, flag.Valid)
	require.False(t, flag.Bool)

	globalIssuer := seedGlobalRemoteIssuer(t, ctx, ti.conn, "rif-global")
	require.Equal(t, int64(0), write(globalIssuer), "a platform-catalog row is refused")
	require.False(t, issuerResourceIndicatorFlag(t, ctx, q, globalIssuer, projectID, orgID).Valid)

	foreignOrg := createOrganization(t, ctx, ti.conn, "rif-foreign-org")
	foreignIssuer := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, foreignOrg, "rif-foreign-tier")
	require.Equal(t, int64(0), write(foreignIssuer), "another organization's row is refused")
	require.False(t, issuerResourceIndicatorFlag(t, ctx, q, foreignIssuer, projectID, foreignOrg).Valid)
}

func TestRemoteLogin_KnownUnsupportedIssuerOmitsResourceButRecordsIt(t *testing.T) {
	t.Parallel()

	const resource = "https://member.example.com/mcp"
	var exchanges atomic.Int64
	_, env := newSyntheticExpiryEnv(t, "resource-unsupported", resourceRejectingToken(&exchanges),
		withResource(resource),
		withResourceIndicatorSupported(false),
	)
	parsed, err := url.Parse(env.authURL)
	require.NoError(t, err)
	require.Empty(t, parsed.Query().Get("resource"))
	require.Equal(t, int64(1), exchanges.Load(), "the first exchange already succeeds without resource")
	require.Equal(t, resource, env.session.Resource.String)
}

func TestRemoteLoginCallback_IssRequiredWhenIssuerAdvertisesIt(t *testing.T) {
	t.Parallel()

	var exchanges atomic.Int64
	counting := func(w http.ResponseWriter, r *http.Request) {
		exchanges.Add(1)
		scopelessToken(w, r)
	}

	_, _, w, err := driveSyntheticLogin(t, "iss-absent", counting, withIssParameterSupported(true))
	require.Error(t, err)
	require.NotEqual(t, http.StatusSeeOther, w.Code)

	_, _, w, err = driveSyntheticLogin(t, "iss-mismatch", counting,
		withIssParameterSupported(true),
		withCallbackQuery(func(q url.Values) { q.Set("iss", "https://attacker.example.com") }),
	)
	require.Error(t, err)
	require.NotEqual(t, http.StatusSeeOther, w.Code)
	require.Equal(t, int64(0), exchanges.Load(), "a rejected callback never burns the code")

	_, env := newSyntheticExpiryEnv(t, "iss-match", counting,
		withIssParameterSupported(true),
		withCallbackQuery(func(q url.Values) { q.Set("iss", syntheticIssuerURL) }),
	)
	require.Equal(t, int64(1), exchanges.Load())
	require.NotEqual(t, uuid.Nil, env.session.ID)
}

// RFC 9207 compares against the issuer identifier the authorization server
// advertises, which can differ from the URL an operator typed.
func TestRemoteLoginCallback_IssComparesAgainstAdvertisedIssuerIdentifier(t *testing.T) {
	t.Parallel()

	advertised := withIssuerMetadata(`{"issuer":"` + syntheticIssuerURL + `/"}`)

	_, env := newSyntheticExpiryEnv(t, "iss-advertised-match", scopelessToken,
		withIssParameterSupported(true),
		advertised,
		withCallbackQuery(func(q url.Values) { q.Set("iss", syntheticIssuerURL+"/") }),
	)
	require.NotEqual(t, uuid.Nil, env.session.ID)

	_, _, w, err := driveSyntheticLogin(t, "iss-advertised-mismatch", scopelessToken,
		withIssParameterSupported(true),
		advertised,
		withCallbackQuery(func(q url.Values) { q.Set("iss", syntheticIssuerURL) }),
	)
	require.Error(t, err, "the stored URL is not the identifier once a document names one")
	require.NotEqual(t, http.StatusSeeOther, w.Code)
}

// Without a document the stored URL stands in for the identifier, and a
// trailing slash an operator typed does not make a correct iss mismatch.
func TestRemoteLoginCallback_IssFallbackIgnoresTrailingSlashOnStoredURL(t *testing.T) {
	t.Parallel()

	_, env := newSyntheticExpiryEnv(t, "iss-fallback-slash", scopelessToken,
		withIssuerURL(syntheticIssuerURL+"/"),
		withIssParameterSupported(true),
		withCallbackQuery(func(q url.Values) { q.Set("iss", syntheticIssuerURL) }),
	)
	require.NotEqual(t, uuid.Nil, env.session.ID)

	_, _, w, err := driveSyntheticLogin(t, "iss-fallback-slash-mismatch", scopelessToken,
		withIssuerURL(syntheticIssuerURL+"/"),
		withIssParameterSupported(true),
		withCallbackQuery(func(q url.Values) { q.Set("iss", "https://attacker.example.com") }),
	)
	require.Error(t, err)
	require.NotEqual(t, http.StatusSeeOther, w.Code)
}

func TestRemoteLoginCallback_IssIgnoredWhenIssuerDoesNotAdvertiseIt(t *testing.T) {
	t.Parallel()

	wrongIss := withCallbackQuery(func(q url.Values) { q.Set("iss", "https://attacker.example.com") })

	_, unknown := newSyntheticExpiryEnv(t, "iss-unknown", scopelessToken, wrongIss)
	require.NotEqual(t, uuid.Nil, unknown.session.ID)

	_, unsupported := newSyntheticExpiryEnv(t, "iss-unsupported", scopelessToken, withIssParameterSupported(false), wrongIss)
	require.NotEqual(t, uuid.Nil, unsupported.session.ID)
}

// The error code on a denial redirect is browser-controlled text: only a code
// an IETF RFC registered is echoed into the public message.
func TestRemoteLoginCallback_DeniedEchoesOnlyRegisteredErrorCodes(t *testing.T) {
	t.Parallel()

	_, _, w, err := driveSyntheticLogin(t, "denied-registered", scopelessToken,
		withCallbackQuery(func(q url.Values) {
			q.Del("code")
			q.Set("error", "access_denied")
		}),
	)
	require.Error(t, err)
	require.NotEqual(t, http.StatusSeeOther, w.Code)
	require.Contains(t, err.Error(), "remote authn challenge denied: access_denied")

	_, _, w, err = driveSyntheticLogin(t, "denied-unregistered", scopelessToken,
		withCallbackQuery(func(q url.Values) {
			q.Del("code")
			q.Set("error", "vendor_custom_failure <script>")
		}),
	)
	require.Error(t, err)
	require.NotEqual(t, http.StatusSeeOther, w.Code)
	require.Contains(t, err.Error(), "remote authn challenge denied")
	require.NotContains(t, err.Error(), "vendor_custom_failure")
}

// A callback carrying neither code nor error is rejected before the login
// state is consumed, so a prefetch of the bare URL cannot burn a pending login.
func TestRemoteLoginCallback_BareStateDoesNotConsumeLogin(t *testing.T) {
	t.Parallel()

	var exchanges atomic.Int64
	counting := func(w http.ResponseWriter, r *http.Request) {
		exchanges.Add(1)
		scopelessToken(w, r)
	}

	ctx, env, w, err := driveSyntheticLogin(t, "bare-state", counting,
		withCallbackQuery(func(q url.Values) { q.Del("code") }),
	)
	require.Error(t, err)
	require.NotEqual(t, http.StatusSeeOther, w.Code)
	require.Equal(t, int64(0), exchanges.Load())

	second, err := env.callback(t, "code=upstream-code&state="+url.QueryEscape(stateOf(t, env.authURL)))
	require.NoError(t, err, "the login state survives the bare request")
	require.Equal(t, http.StatusSeeOther, second.Code)
	require.Equal(t, int64(1), exchanges.Load())

	session, err := env.q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{
		SubjectUrn:            env.subject,
		RemoteSessionClientID: env.clientID,
	})
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, session.ID)
}
