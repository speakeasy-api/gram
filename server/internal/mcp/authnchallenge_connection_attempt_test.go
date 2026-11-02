package mcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/connectionevents"
	"github.com/speakeasy-api/gram/server/internal/connectionevents/connectioneventstest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// /authorize starts the clock: the attempt lands in ClickHouse and in the
// sign-in state that every later step reads it from.
func TestAuthorize_BeginsTheConnectionAttempt(t *testing.T) {
	t.Parallel()

	ctx, ti, _ := newTestMCPServiceWithDevIDP(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	recorder := connectionevents.NewRecorder(testenv.NewLogger(t), testenv.NewMeterProvider(t), conn, ti.cacheAdapter, connectioneventstest.LogsOn{})
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = recorder.Shutdown(cleanupCtx)
	})
	ti.service.SetConnectionRecorder(recorder)

	slug := "attempt-authorize-" + uuid.New().String()[:8]
	toolset, issuer := createPrivateIssuerGatedToolset(t, ctx, ti, authCtx, slug)
	clientID := "attempt-authorize-client"
	insertUserSessionClient(t, ctx, ti.conn, issuer.ID, clientID)

	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", clientID)
	q.Set("redirect_uri", "http://example.com/cb")
	q.Set("state", "state-123")
	q.Set("code_challenge", "challenge")
	q.Set("code_challenge_method", "S256")
	req := httptest.NewRequest(http.MethodGet, "/mcp/"+slug+"/authorize?"+q.Encode(), nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("mcpSlug", slug)
	req = req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	require.NoError(t, ti.service.HandleAuthorize(w, req))
	require.Equal(t, http.StatusFound, w.Code)

	loc, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	_, authnCache := buildChallengeManagerForTest(t, ti)
	stored, err := authnCache.Get(ctx, "authnChallenge:"+loc.Query().Get("state"))
	require.NoError(t, err)
	require.False(t, stored.Attempt.IsZero())

	require.NoError(t, recorder.Shutdown(ctx))

	var rows []struct {
		OrganizationID   string    `ch:"organization_id"`
		ProjectID        uuid.UUID `ch:"project_id"`
		AttemptStartedAt time.Time `ch:"attempt_started_at"`
		ServerKind       string    `ch:"server_kind"`
		ServerID         string    `ch:"server_id"`
		Step             string    `ch:"step"`
		StepResult       string    `ch:"step_result"`
		IsFinal          bool      `ch:"is_final"`
		AppKey           string    `ch:"app_key"`
		ProviderHost     string    `ch:"provider_host"`
	}
	require.NoError(t, conn.Select(ctx, &rows, `
		SELECT organization_id, project_id, attempt_started_at, server_kind, server_id, step, step_result, is_final, app_key, provider_host
		FROM mcp_connection_events WHERE attempt_id = ?`, stored.Attempt.ID()))
	require.Len(t, rows, 1)
	require.Equal(t, authCtx.ActiveOrganizationID, rows[0].OrganizationID)
	require.Equal(t, *authCtx.ProjectID, rows[0].ProjectID)
	require.WithinDuration(t, stored.Attempt.StartedAt(), rows[0].AttemptStartedAt, time.Microsecond)
	require.Equal(t, toolset.ID.String(), rows[0].ServerID)
	require.Equal(t, "toolset", rows[0].ServerKind)
	require.Equal(t, connectionevents.StepInboundAuthorize.String(), rows[0].Step)
	require.Equal(t, connectionevents.ResultAccepted.String(), rows[0].StepResult)
	require.False(t, rows[0].IsFinal)
	require.Equal(t, connectionevents.AppDCR.String(), rows[0].AppKey)
	require.Empty(t, rows[0].ProviderHost, "Speakeasy sign-in has no provider")
}

// Consent hands the sign-in's attempt to the provider sign-in, so both flows
// join one attempt.
func TestConsentConnect_CarriesTheAttemptIntoProviderSignIn(t *testing.T) {
	t.Parallel()

	ctx, fx := seedMultiClientConsentEndpoint(t)
	state, err := fx.ti.authnChallengeCache.Get(ctx, "authnChallenge:"+fx.stateID)
	require.NoError(t, err)
	attempt := connectionevents.Begin(connectionevents.ServerScope(fx.orgID, fx.projectID, uuid.New(), connectionevents.AppDCR))
	state.Attempt = attempt
	require.NoError(t, fx.ti.authnChallengeCache.Store(ctx, state))

	loc := postConnectAction(t, fx, fx.clientA)

	minted := mintedRemoteLoginState(t, ctx, fx, loc.Query().Get("state"))
	require.Equal(t, attempt.ID(), minted.Attempt.ID())
	require.True(t, attempt.StartedAt().Equal(minted.Attempt.StartedAt()))
	require.Equal(t, "age3328-a-as.example.com", minted.ProviderHost)
}

// Sign-in state from before attempts existed still connects, on a new attempt.
func TestConsentConnect_OldStateWithoutAnAttemptStillWorks(t *testing.T) {
	t.Parallel()

	ctx, fx := seedMultiClientConsentEndpoint(t)
	state, err := fx.ti.authnChallengeCache.Get(ctx, "authnChallenge:"+fx.stateID)
	require.NoError(t, err)
	require.True(t, state.Attempt.IsZero())

	loc := postConnectAction(t, fx, fx.clientA)

	minted := mintedRemoteLoginState(t, ctx, fx, loc.Query().Get("state"))
	require.False(t, minted.Attempt.IsZero())
}

func TestChallengeState_FromBeforeAttemptsDecodes(t *testing.T) {
	t.Parallel()

	var state mcp.AuthnChallengeState
	require.NoError(t, json.Unmarshal([]byte(`{"id":"old","flow_id":"flow","client_id":"client","created_at":"2026-01-01T00:00:00Z"}`), &state))
	require.Equal(t, "old", state.ID)
	require.True(t, state.Attempt.IsZero())
}
