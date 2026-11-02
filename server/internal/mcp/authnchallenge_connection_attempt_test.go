package mcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/connectionevents"
	"github.com/speakeasy-api/gram/server/internal/connectionevents/connectioneventstest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/orggateway"
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
	recorder := connectionevents.NewRecorder(testenv.NewLogger(t), testenv.NewMeterProvider(t), conn, connectioneventstest.LogsOn)
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
	attemptID, err := uuid.Parse(stored.FlowID)
	require.NoError(t, err)

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
		FROM mcp_connection_events WHERE attempt_id = ?`, attemptID))
	require.Len(t, rows, 1)
	require.Equal(t, authCtx.ActiveOrganizationID, rows[0].OrganizationID)
	require.Equal(t, *authCtx.ProjectID, rows[0].ProjectID)
	require.WithinDuration(t, stored.CreatedAt, rows[0].AttemptStartedAt, time.Microsecond)
	require.Equal(t, toolset.ID.String(), rows[0].ServerID)
	require.Equal(t, "toolset", rows[0].ServerKind)
	require.Equal(t, connectionevents.StepInboundAuthorize.String(), rows[0].Step)
	require.Equal(t, connectionevents.ResultAccepted.String(), rows[0].StepResult)
	require.False(t, rows[0].IsFinal)
	require.Equal(t, connectionevents.AppDCR.String(), rows[0].AppKey)
	require.Empty(t, rows[0].ProviderHost, "Speakeasy sign-in has no provider")
}

// The organization gateway belongs to no project, and its rows name the gateway.
func TestAuthorize_OrganizationGatewayRecordsNoProject(t *testing.T) {
	t.Parallel()

	ctx, ti, _ := newTestMCPServiceWithDevIDP(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	recorder := connectionevents.NewRecorder(testenv.NewLogger(t), testenv.NewMeterProvider(t), conn, connectioneventstest.LogsOn)
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = recorder.Shutdown(cleanupCtx)
	})
	ti.service.SetConnectionRecorder(recorder)

	router := goahttp.NewMuxer()
	mcp.Attach(router, ti.service.Service, nil)
	gateway := seedOrganizationGateway(t, ctx, ti)
	issuer := ti.serverURL.String() + "/mcp/gateway/" + gateway.organizationSlug
	redirectURI := "https://localhost:33418/callback"
	clientID := registerErrorTestClient(t, ctx, router, issuer, redirectURI)

	query := url.Values{"response_type": {"code"}, "client_id": {clientID}, "redirect_uri": {redirectURI}, "state": {"client-state"}, "code_challenge": {"challenge"}, "code_challenge_method": {"S256"}}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequestWithContext(ctx, http.MethodGet, issuer+"/authorize?"+query.Encode(), nil))
	require.Equal(t, http.StatusFound, w.Code, w.Body.String())
	require.NoError(t, recorder.Shutdown(ctx))

	serverID := orggateway.GatewayID(authCtx.ActiveOrganizationID).String()
	var rows []struct {
		ProjectID  uuid.UUID `ch:"project_id"`
		ServerKind string    `ch:"server_kind"`
		ServerID   string    `ch:"server_id"`
		AppKey     string    `ch:"app_key"`
	}
	require.NoError(t, conn.Select(ctx, &rows, `
		SELECT project_id, server_kind, server_id, app_key
		FROM mcp_connection_events WHERE organization_id = ? AND server_id = ?`, authCtx.ActiveOrganizationID, serverID))
	require.Len(t, rows, 1)
	require.Equal(t, uuid.Nil, rows[0].ProjectID)
	require.Equal(t, "meta", rows[0].ServerKind)
	require.Equal(t, connectionevents.AppDCR.String(), rows[0].AppKey)
}

// A dashboard connect and its return to consent stay on one attempt, however
// many times the person clicks Connect.
func TestConsentConnect_KeepsOneAttemptAcrossTheConsentReturn(t *testing.T) {
	t.Parallel()

	ctx, fx := seedMultiClientConsentEndpoint(t)
	state, err := fx.ti.authnChallengeCache.Get(ctx, "authnChallenge:"+fx.stateID)
	require.NoError(t, err)
	state.FlowID = uuid.NewString()
	state.CreatedAt = time.Now().Add(-time.Minute)
	require.NoError(t, fx.ti.authnChallengeCache.Store(ctx, state))

	first := mintedRemoteLoginState(t, ctx, fx, postConnectAction(t, fx, fx.clientA).Query().Get("state"))
	second := mintedRemoteLoginState(t, ctx, fx, postConnectAction(t, fx, fx.clientA).Query().Get("state"))

	require.Equal(t, state.FlowID, first.Attempt.ID().String())
	require.Equal(t, first.Attempt.ID(), second.Attempt.ID())
	require.True(t, state.CreatedAt.Equal(first.Attempt.StartedAt()))
	require.Equal(t, "age3328-a-as.example.com", first.ProviderHost)

	back, err := fx.ti.authnChallengeCache.Get(ctx, "authnChallenge:"+fx.stateID)
	require.NoError(t, err)
	require.Equal(t, first.Attempt.ID(), back.ConnectionAttempt(fx.endpoint).ID(), "the same id after returning to consent")
}

// State with no flow still connects, and has no attempt to record on.
func TestConsentConnect_StateWithoutAFlowStillWorks(t *testing.T) {
	t.Parallel()

	ctx, fx := seedMultiClientConsentEndpoint(t)
	state, err := fx.ti.authnChallengeCache.Get(ctx, "authnChallenge:"+fx.stateID)
	require.NoError(t, err)
	state.FlowID = ""
	require.NoError(t, fx.ti.authnChallengeCache.Store(ctx, state))

	minted := mintedRemoteLoginState(t, ctx, fx, postConnectAction(t, fx, fx.clientA).Query().Get("state"))
	require.True(t, minted.Attempt.IsZero())
}

// CompareAndSwap compares encoded bytes, so a field on this state would lose every
// swap on a replica without it during a rollout. The attempt is read from FlowID.
func TestChallengeState_HasNoAttemptFieldAndSwapsAfterARoundTrip(t *testing.T) {
	t.Parallel()

	_, hasField := reflect.TypeFor[mcp.AuthnChallengeState]().FieldByName("Attempt")
	require.False(t, hasField)

	ctx, fx := seedMultiClientConsentEndpoint(t)
	state, err := fx.ti.authnChallengeCache.Get(ctx, "authnChallenge:"+fx.stateID)
	require.NoError(t, err)
	latched := state
	latched.AutoConnectDone = true
	swapped, err := fx.ti.authnChallengeCache.CompareAndSwap(ctx, state, latched)
	require.NoError(t, err)
	require.True(t, swapped)
}

func TestChallengeState_AttemptNamesTheApp(t *testing.T) {
	t.Parallel()

	endpoint := &mcp.ResolvedMcpEndpoint{OrganizationID: "org-test", ProjectID: uuid.New(), McpServerID: uuid.NullUUID{UUID: uuid.New(), Valid: true}}
	base := mcp.AuthnChallengeState{FlowID: uuid.NewString(), CreatedAt: time.Now()}
	app := func(state mcp.AuthnChallengeState) string {
		t.Helper()
		row, err := connectionevents.RowAt(state.ConnectionAttempt(endpoint), connectionevents.StepInboundConsent, connectionevents.ResultAccepted, false, time.Now())
		require.NoError(t, err)
		require.Equal(t, state.FlowID, row.AttemptID.String())
		return row.AppKey
	}

	require.Equal(t, connectionevents.AppFirstParty.String(), app(base))
	workload := base
	workload.WorkloadReconnect = &mcp.WorkloadReconnectBinding{}
	require.Equal(t, connectionevents.AppWorkload.String(), app(workload))
	codeMode := base
	codeMode.CodeReconnect = &mcp.CodeReconnectBinding{}
	require.Equal(t, connectionevents.AppCodeMode.String(), app(codeMode))
	cimdClient := base
	cimdClient.ClientID = "https://client.example.com/some/path"
	require.Equal(t, "client.example.com", app(cimdClient))
	dcrClient := base
	dcrClient.ClientID = "registered-client"
	require.Equal(t, connectionevents.AppDCR.String(), app(dcrClient))
}

func TestChallengeState_FromBeforeAttemptsDecodes(t *testing.T) {
	t.Parallel()

	var state mcp.AuthnChallengeState
	require.NoError(t, json.Unmarshal([]byte(`{"id":"old","flow_id":"flow","client_id":"client","created_at":"2026-01-01T00:00:00Z"}`), &state))
	require.Equal(t, "old", state.ID)
	require.True(t, state.ConnectionAttempt(&mcp.ResolvedMcpEndpoint{}).IsZero(), "flow is not a uuid")
}
