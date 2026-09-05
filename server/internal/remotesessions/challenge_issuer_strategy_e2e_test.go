// challenge_issuer_strategy_e2e_test.go drives BuildAuthorizationUrl →
// HandleRemoteLoginCallback against a real ChallengeManager + database for the
// per-issuer request strategy: which scopes are requested, whether the RFC
// 8707 resource parameter is sent and what happens when the issuer rejects
// it, and the RFC 9207 iss check on the callback.

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

// scopelessToken answers every exchange with a token response that omits
// scope, so the session records what was requested.
func scopelessToken(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"access_token":"access","refresh_token":"refresh","token_type":"Bearer","expires_in":3600}`))
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
	issuer, err := env.q.GetRemoteSessionIssuerByID(ctx, repo.GetRemoteSessionIssuerByIDParams{
		ID:                    env.issuerID,
		ProjectID:             conv.ToNullUUID(env.projectID),
		OrganizationID:        conv.ToPGText(env.organizationID),
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
	require.False(t, flag.Bool, "the issuer now records that it rejects RFC 8707")

	session, err := env.q.GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{
		SubjectUrn:            env.subject,
		RemoteSessionClientID: env.clientID,
	})
	require.NoError(t, err)
	require.Equal(t, resource, session.Resource.String, "the resource is still recorded so the grant stays routable")
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

func TestRemoteLoginCallback_InvalidTargetOnAuthorizeRedirectRecordsOnlyAfterRetrySucceeds(t *testing.T) {
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
	require.Equal(t, int64(1), exchanges.Load())
	flag := resourceIndicatorFlag(t, ctx, env)
	require.True(t, flag.Valid)
	require.False(t, flag.Bool, "a successful resource-less exchange is the evidence that gets recorded")
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

func TestRemoteLoginCallback_IssIgnoredWhenIssuerDoesNotAdvertiseIt(t *testing.T) {
	t.Parallel()

	wrongIss := withCallbackQuery(func(q url.Values) { q.Set("iss", "https://attacker.example.com") })

	_, unknown := newSyntheticExpiryEnv(t, "iss-unknown", scopelessToken, wrongIss)
	require.NotEqual(t, uuid.Nil, unknown.session.ID)

	_, unsupported := newSyntheticExpiryEnv(t, "iss-unsupported", scopelessToken, withIssParameterSupported(false), wrongIss)
	require.NotEqual(t, uuid.Nil, unsupported.session.ID)
}
