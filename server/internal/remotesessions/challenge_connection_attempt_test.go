package remotesessions_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/mcpconnectionattemptstest"
	"github.com/speakeasy-api/gram/server/internal/oautherr"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type attemptRow struct {
	AttemptID        uuid.UUID `ch:"attempt_id"`
	AttemptStartedAt time.Time `ch:"attempt_started_at"`
	OrganizationID   string    `ch:"organization_id"`
	ServerID         string    `ch:"server_id"`
	Step             string    `ch:"step"`
	StepResult       string    `ch:"step_result"`
	IsFinal          bool      `ch:"is_final"`
	ProviderHost     string    `ch:"provider_host"`
	SubjectURN       string    `ch:"subject_urn"`
}

const selectAttemptRows = `SELECT attempt_id, attempt_started_at, organization_id, server_id, step, step_result, is_final, provider_host, subject_urn FROM mcp_connection_events `

func realRecorder(t *testing.T, fx resourceDanceFixture) (*mcpconnectionattempts.Recorder, driver.Conn) {
	t.Helper()

	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	recorder := mcpconnectionattempts.NewRecorder(testenv.NewLogger(t), testenv.NewMeterProvider(t), conn, mcpconnectionattemptstest.LogsOn)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = recorder.Shutdown(ctx)
	})
	remotesessions.WithConnectionRecorder(recorder)(fx.mgr)
	return recorder, conn
}

func loginState(t *testing.T, authURL string) string {
	t.Helper()

	parsed, err := url.Parse(authURL)
	require.NoError(t, err)
	state := parsed.Query().Get("state")
	require.NotEmpty(t, state)
	return state
}

func complete(t *testing.T, ctx context.Context, fx resourceDanceFixture, query string) remotesessions.RemoteLoginResult {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/mcp/remote_login_callback?"+query, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh) Chrome/141.0.0.0 Safari/537.36")
	result, err := fx.mgr.CompleteRemoteLogin(req.WithContext(ctx))
	require.NoError(t, err)
	return result
}

// Both flows join one attempt: the id and start time from the Speakeasy sign-in
// state reach ClickHouse from the provider's callback.
func TestCompleteRemoteLogin_CarriesTheAttemptThroughProviderSignIn(t *testing.T) {
	t.Parallel()

	var spy upstreamSpy
	ctx, fx := setupResourceDanceFixture(t, "https://mcp.example.com/mcp", "attempt-carry", &spy)
	recorder, conn := realRecorder(t, fx)

	started := time.Now().Add(-5 * time.Minute)
	serverID := uuid.New()
	attempt := mcpconnectionattempts.AttemptAt(uuid.New(), started, mcpconnectionattempts.ServerScope(fx.parent.OrganizationID, fx.parent.ProjectID, serverID, mcpconnectionattempts.AppDCR))
	parent := fx.parent
	parent.Attempt = attempt

	authURL, err := fx.mgr.BuildAuthorizationUrl(ctx, parent, fx.clients[0])
	require.NoError(t, err)
	complete(t, ctx, fx, "code=fake-code&state="+url.QueryEscape(loginState(t, authURL)))
	require.NoError(t, spy.handlerErr)
	require.NoError(t, recorder.Shutdown(ctx))

	var rows []attemptRow
	require.NoError(t, conn.Select(ctx, &rows, selectAttemptRows+`WHERE attempt_id = ? AND step_result = 'signed_in'`, attempt.ID()))
	require.Len(t, rows, 1)
	require.WithinDuration(t, started, rows[0].AttemptStartedAt, time.Microsecond, "the clock started at /authorize")
	require.Equal(t, fx.parent.OrganizationID, rows[0].OrganizationID)
	require.Equal(t, serverID.String(), rows[0].ServerID)
	require.Equal(t, mcpconnectionattempts.StepProviderSignIn.String(), rows[0].Step)
	require.Equal(t, mcpconnectionattempts.ResultSignedIn.String(), rows[0].StepResult)
	require.False(t, rows[0].IsFinal, "the login returns to consent, so the attempt goes on")
	require.NotEmpty(t, rows[0].ProviderHost)
	require.Equal(t, fx.parent.Subject.String(), rows[0].SubjectURN)
}

// Sign-in state with no attempt still completes, and records nothing.
func TestCompleteRemoteLogin_StateWithoutAnAttemptStillWorks(t *testing.T) {
	t.Parallel()

	var spy upstreamSpy
	ctx, fx := setupResourceDanceFixture(t, "https://mcp.example.com/mcp", "attempt-old", &spy)
	rec := mcpconnectionattemptstest.New(t)
	t.Cleanup(rec.RequireNoUnclassifiedExit)
	remotesessions.WithConnectionRecorder(rec.Recorder)(fx.mgr)

	authURL, err := fx.mgr.BuildAuthorizationUrl(ctx, fx.parent, fx.clients[0])
	require.NoError(t, err)

	result := complete(t, ctx, fx, "code=fake-code&state="+url.QueryEscape(loginState(t, authURL)))
	require.NoError(t, spy.handlerErr)
	require.NotNil(t, result.Grant, "the login completes")
	rec.RequireNothing()
}

// The invalid_target retry leg is the same login, so it keeps the attempt.
func TestCompleteRemoteLogin_InvalidTargetRetryKeepsTheAttempt(t *testing.T) {
	t.Parallel()

	var spy upstreamSpy
	ctx, fx := setupResourceDanceFixture(t, "https://mcp.example.com/mcp", "attempt-retry", &spy)
	started := time.Now().Add(-time.Minute)
	attempt := mcpconnectionattempts.AttemptAt(uuid.New(), started, mcpconnectionattempts.ServerScope(fx.parent.OrganizationID, fx.parent.ProjectID, uuid.New(), mcpconnectionattempts.AppDCR))
	member := uuid.NullUUID{UUID: uuid.New(), Valid: true}
	parent := fx.parent
	parent.Attempt = attempt
	parent.ResourceMcpServerID = member

	authURL, err := fx.mgr.BuildAuthorizationUrl(ctx, parent, fx.clients[0])
	require.NoError(t, err)
	result := complete(t, ctx, fx, "error=invalid_target&state="+url.QueryEscape(loginState(t, authURL)))
	require.Nil(t, result.Grant)

	states := cache.NewTypedObjectCache[remotesessions.RemoteLoginState](testenv.NewLogger(t), fx.ti.redisCache, cache.SuffixNone)
	retried, err := states.Get(ctx, "remoteLogin:"+loginState(t, result.RedirectURL))
	require.NoError(t, err)
	require.True(t, retried.ResourceRetried)
	require.Equal(t, attempt.ID(), retried.Attempt.ID())
	require.True(t, attempt.StartedAt().Equal(retried.Attempt.StartedAt()))
	require.Equal(t, member, retried.ResourceMcpServerID)
}

// A gateway sign-in names the member it was for, so the rollup can tie it to a server.
func TestCompleteRemoteLogin_RecordsTheGatewayMember(t *testing.T) {
	t.Parallel()

	var spy upstreamSpy
	ctx, fx := setupResourceDanceFixture(t, "https://mcp.example.com/mcp", "attempt-member", &spy)
	rec := mcpconnectionattemptstest.New(t)
	t.Cleanup(rec.RequireNoUnclassifiedExit)
	remotesessions.WithConnectionRecorder(rec.Recorder)(fx.mgr)
	member := uuid.NullUUID{UUID: uuid.New(), Valid: true}
	parent := fx.parent
	parent.Attempt = mcpconnectionattempts.Begin(mcpconnectionattempts.GatewayScope(fx.parent.OrganizationID, fx.parent.ProjectID, uuid.New(), mcpconnectionattempts.AppDCR))
	parent.ResourceMcpServerID = member

	authURL, err := fx.mgr.BuildAuthorizationUrl(ctx, parent, fx.clients[0])
	require.NoError(t, err)
	complete(t, ctx, fx, "code=fake-code&state="+url.QueryEscape(loginState(t, authURL)))

	row := rec.RequireStep(mcpconnectionattempts.StepProviderSignIn, mcpconnectionattempts.ResultSignedIn)
	require.Equal(t, member.UUID.String(), row.MemberID)
	require.Equal(t, mcpconnectionattempts.FamilyBrowser, row.ClientFamily, "the callback says which client the person used")
}

// The login state holds the attempt, so the invalid_target retry leg keeps it.
func TestConnectionAttempt_IsKeptInTheLoginState(t *testing.T) {
	t.Parallel()

	var spy upstreamSpy
	ctx, fx := setupResourceDanceFixture(t, "https://mcp.example.com/mcp", "attempt-state", &spy)
	attempt := mcpconnectionattempts.Begin(mcpconnectionattempts.ServerScope(fx.parent.OrganizationID, fx.parent.ProjectID, uuid.New(), mcpconnectionattempts.AppDCR))
	parent := fx.parent
	parent.Attempt = attempt

	authURL, err := fx.mgr.BuildAuthorizationUrl(ctx, parent, fx.clients[0])
	require.NoError(t, err)

	states := cache.NewTypedObjectCache[remotesessions.RemoteLoginState](testenv.NewLogger(t), fx.ti.redisCache, cache.SuffixNone)
	state, err := states.Get(ctx, "remoteLogin:"+loginState(t, authURL))
	require.NoError(t, err)
	require.Equal(t, attempt.ID(), state.Attempt.ID())
	require.NotEmpty(t, state.ProviderHost)
	require.Equal(t, fx.issuerID, state.RemoteSessionIssuerID)
}

// Back at consent the person can still finish, so a cancel does not end the attempt.
func TestCompleteRemoteLogin_CancelReturningToConsentDoesNotEndTheAttempt(t *testing.T) {
	t.Parallel()

	var spy upstreamSpy
	ctx, fx := setupResourceDanceFixture(t, "https://mcp.example.com/mcp", "attempt-cancel", &spy)
	rec := mcpconnectionattemptstest.New(t)
	t.Cleanup(rec.RequireNoUnclassifiedExit)
	remotesessions.WithConnectionRecorder(rec.Recorder)(fx.mgr)

	parent := fx.parent
	parent.Attempt = mcpconnectionattempts.Begin(mcpconnectionattempts.ServerScope(fx.parent.OrganizationID, fx.parent.ProjectID, uuid.New(), mcpconnectionattempts.AppDCR))
	authURL, err := fx.mgr.BuildAuthorizationUrl(ctx, parent, fx.clients[0])
	require.NoError(t, err)
	complete(t, ctx, fx, "error=access_denied&state="+url.QueryEscape(loginState(t, authURL)))

	row := rec.RequireStep(mcpconnectionattempts.StepProviderSignIn, mcpconnectionattempts.CodeProviderDenied)
	require.Equal(t, string(mcpconnectionattempts.WhoPerson), row.WhoFixes)
	require.Empty(t, row.Outcome)
	rec.RequireNotEnded()
}

// A login with its own final destination has no consent page to return to, so
// a cancel ends the attempt there.
func TestCompleteRemoteLogin_CancelWithAFinalDestinationEndsTheAttempt(t *testing.T) {
	t.Parallel()

	var spy upstreamSpy
	ctx, fx := setupResourceDanceFixture(t, "https://mcp.example.com/mcp", "attempt-cancel-final", &spy)
	rec := mcpconnectionattemptstest.New(t)
	t.Cleanup(rec.RequireNoUnclassifiedExit)
	remotesessions.WithConnectionRecorder(rec.Recorder)(fx.mgr)
	parent := fx.parent
	parent.FinalRedirectURI = "https://app.example.com/done"
	parent.Attempt = mcpconnectionattempts.Begin(mcpconnectionattempts.ServerScope(fx.parent.OrganizationID, fx.parent.ProjectID, uuid.New(), mcpconnectionattempts.AppDCR))

	authURL, err := fx.mgr.BuildAuthorizationUrl(ctx, parent, fx.clients[0])
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodGet, "/mcp/remote_login_callback?error=access_denied&state="+url.QueryEscape(loginState(t, authURL)), nil)
	_, err = fx.mgr.CompleteRemoteLogin(req.WithContext(ctx))
	require.Error(t, err, "a canceled login stays on the callback page")

	row := rec.RequireEnded(mcpconnectionattempts.StepProviderSignIn, mcpconnectionattempts.CodeProviderDenied)
	require.Equal(t, string(mcpconnectionattempts.OutcomeDeclined), row.Outcome)
}

// Reaching the provider is a row, so an attempt that stops there is not invisible.
func TestBuildAuthorizationUrl_RecordsReachingTheProvider(t *testing.T) {
	t.Parallel()

	var spy upstreamSpy
	ctx, fx := setupResourceDanceFixture(t, "https://mcp.example.com/mcp", "attempt-accepted", &spy)
	rec := mcpconnectionattemptstest.New(t)
	t.Cleanup(rec.RequireNoUnclassifiedExit)
	remotesessions.WithConnectionRecorder(rec.Recorder)(fx.mgr)
	parent := fx.parent
	parent.Attempt = mcpconnectionattempts.Begin(mcpconnectionattempts.ServerScope(fx.parent.OrganizationID, fx.parent.ProjectID, uuid.New(), mcpconnectionattempts.AppRecoveryLink))

	_, err := fx.mgr.BuildAuthorizationUrl(ctx, parent, fx.clients[0])
	require.NoError(t, err)

	row := rec.RequireStep(mcpconnectionattempts.StepProviderSignIn, mcpconnectionattempts.ResultAccepted)
	require.NotEmpty(t, row.ProviderHost)
	require.Equal(t, fx.clients[0].ID.String(), row.RemoteSessionClientID)
	rec.RequireNotEnded()
}

// A provider error ends a login with its own destination. One from consent stays
// open, because the person can go back and connect again on the same attempt.
func TestCompleteRemoteLogin_ProviderErrorsAreRecorded(t *testing.T) {
	t.Parallel()

	for name, final := range map[string]string{"returns to consent": "", "final destination": "https://app.example.com/done"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var spy upstreamSpy
			ctx, fx := setupResourceDanceFixture(t, "https://mcp.example.com/mcp", "attempt-provider-error", &spy)
			rec := mcpconnectionattemptstest.New(t)
			t.Cleanup(rec.RequireNoUnclassifiedExit)
			remotesessions.WithConnectionRecorder(rec.Recorder)(fx.mgr)
			parent := fx.parent
			parent.FinalRedirectURI = final
			parent.Attempt = mcpconnectionattempts.Begin(mcpconnectionattempts.ServerScope(fx.parent.OrganizationID, fx.parent.ProjectID, uuid.New(), mcpconnectionattempts.AppDCR))

			authURL, err := fx.mgr.BuildAuthorizationUrl(ctx, parent, fx.clients[0])
			require.NoError(t, err)
			req := httptest.NewRequest(http.MethodGet, "/mcp/remote_login_callback?error=invalid_scope&state="+url.QueryEscape(loginState(t, authURL)), nil)
			_, err = fx.mgr.CompleteRemoteLogin(req.WithContext(ctx))
			require.Error(t, err)

			require.Equal(t, final != "", rowFor(t, rec, mcpconnectionattempts.CodeProviderOAuthInvalidScope).IsFinal)
			require.Equal(t, "invalid_scope", rowFor(t, rec, mcpconnectionattempts.CodeProviderOAuthInvalidScope).SubReason)
			rec.RequireOneAttempt()
		})
	}
}

// A value from the query never reaches a stored label.
func TestCompleteRemoteLogin_UnregisteredProviderErrorIsNotStored(t *testing.T) {
	t.Parallel()

	var spy upstreamSpy
	ctx, fx := setupResourceDanceFixture(t, "https://mcp.example.com/mcp", "attempt-unregistered", &spy)
	rec := mcpconnectionattemptstest.New(t)
	t.Cleanup(rec.RequireNoUnclassifiedExit)
	remotesessions.WithConnectionRecorder(rec.Recorder)(fx.mgr)
	parent := fx.parent
	parent.Attempt = mcpconnectionattempts.Begin(mcpconnectionattempts.ServerScope(fx.parent.OrganizationID, fx.parent.ProjectID, uuid.New(), mcpconnectionattempts.AppDCR))
	authURL, err := fx.mgr.BuildAuthorizationUrl(ctx, parent, fx.clients[0])
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodGet, "/mcp/remote_login_callback?error=made_up&state="+url.QueryEscape(loginState(t, authURL)), nil)
	_, err = fx.mgr.CompleteRemoteLogin(req.WithContext(ctx))
	require.Error(t, err)

	require.Equal(t, "unregistered", rowFor(t, rec, mcpconnectionattempts.CodeSignInProviderError).SubReason)
}

func rowFor(t *testing.T, rec *mcpconnectionattemptstest.Recording, what mcpconnectionattempts.Code) mcpconnectionattempts.Row {
	t.Helper()
	for _, row := range rec.Rows() {
		if row.Reason == what.String() {
			return row
		}
	}
	require.Failf(t, "not recorded", "%s in %+v", what, rec.Rows())
	return mcpconnectionattempts.Row{}
}

// A sign-in with its own destination is the whole attempt, so success ends it Connected.
func TestCompleteRemoteLogin_SignInWithAFinalDestinationEndsConnected(t *testing.T) {
	t.Parallel()

	var spy upstreamSpy
	ctx, fx := setupResourceDanceFixture(t, "https://mcp.example.com/mcp", "attempt-final", &spy)
	rec := mcpconnectionattemptstest.New(t)
	t.Cleanup(rec.RequireNoUnclassifiedExit)
	remotesessions.WithConnectionRecorder(rec.Recorder)(fx.mgr)
	parent := fx.parent
	parent.FinalRedirectURI = "https://app.example.com/done"
	parent.Attempt = mcpconnectionattempts.Begin(mcpconnectionattempts.ServerScope(fx.parent.OrganizationID, fx.parent.ProjectID, uuid.New(), mcpconnectionattempts.AppRecoveryLink))

	authURL, err := fx.mgr.BuildAuthorizationUrl(ctx, parent, fx.clients[0])
	require.NoError(t, err)
	complete(t, ctx, fx, "code=fake-code&state="+url.QueryEscape(loginState(t, authURL)))
	require.NoError(t, spy.handlerErr)

	row := rec.RequireEnded(mcpconnectionattempts.StepProviderSignIn, mcpconnectionattempts.ResultSignedIn)
	require.Equal(t, string(mcpconnectionattempts.OutcomeConnected), row.Outcome)
	rec.RequireOneAttempt()
}

// A second invalid_target is the provider's refusal. Anything else on the retry is not.
func TestCompleteRemoteLogin_RetryFailuresKeepTheirOwnCode(t *testing.T) {
	t.Parallel()

	begin := func(t *testing.T, tag string) (context.Context, resourceDanceFixture, *mcpconnectionattemptstest.Recording, string) {
		t.Helper()
		var spy upstreamSpy
		ctx, fx := setupResourceDanceFixture(t, "https://mcp.example.com/mcp", tag, &spy)
		rec := mcpconnectionattemptstest.New(t)
		t.Cleanup(rec.RequireNoUnclassifiedExit)
		remotesessions.WithConnectionRecorder(rec.Recorder)(fx.mgr)
		parent := fx.parent
		parent.Attempt = mcpconnectionattempts.Begin(mcpconnectionattempts.ServerScope(fx.parent.OrganizationID, fx.parent.ProjectID, uuid.New(), mcpconnectionattempts.AppDCR))
		authURL, err := fx.mgr.BuildAuthorizationUrl(ctx, parent, fx.clients[0])
		require.NoError(t, err)
		return ctx, fx, rec, loginState(t, authURL)
	}
	callback := func(ctx context.Context, fx resourceDanceFixture, query string) error {
		req := httptest.NewRequest(http.MethodGet, "/mcp/remote_login_callback?"+query, nil)
		_, err := fx.mgr.CompleteRemoteLogin(req.WithContext(ctx))
		return err //nolint:wrapcheck // compared by the caller
	}

	t.Run("a second refusal is the provider's", func(t *testing.T) {
		t.Parallel()
		ctx, fx, rec, login := begin(t, "attempt-retry-refused")
		result := complete(t, ctx, fx, "error=invalid_target&state="+url.QueryEscape(login))
		require.Error(t, callback(ctx, fx, "error=invalid_target&state="+url.QueryEscape(loginState(t, result.RedirectURL))))
		require.Equal(t, oautherr.CodeInvalidTarget, rowFor(t, rec, mcpconnectionattempts.CodeProviderOAuthInvalidScope).SubReason)
	})
	t.Run("a removed connection is not a refusal", func(t *testing.T) {
		t.Parallel()
		ctx, fx, rec, login := begin(t, "attempt-retry-removed")
		_, err := repo.New(fx.ti.conn).DeleteRemoteSessionClient(ctx, repo.DeleteRemoteSessionClientParams{ID: fx.clients[0].ID, ProjectID: uuid.NullUUID{UUID: fx.parent.ProjectID, Valid: true}})
		require.NoError(t, err)
		require.Error(t, callback(ctx, fx, "error=invalid_target&state="+url.QueryEscape(login)))
		rowFor(t, rec, mcpconnectionattempts.CodeConnectionChanged)
	})
}
