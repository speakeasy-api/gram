package mcp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authenforcement"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcproute"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/mcpconnectionattemptstest"
	"github.com/speakeasy-api/gram/server/internal/oauthui"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/remotesessionmetrics"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// The row's code and the page's explanation come from one decision, per failure.
func TestClassifyCheckFailure(t *testing.T) {
	t.Parallel()

	invariant := oops.E(oops.CodeUnexpected, fmt.Errorf("invariant: %w", errors.New("at most one remote_session_client")), "resolve upstream tokens for validation")
	cases := []struct {
		name string
		err  error
		code mcpconnectionattempts.Code
		page oauthui.Reason
	}{
		{name: "token resolution invariant", err: invariant, code: mcpconnectionattempts.CodeInternalError, page: oauthui.ReasonCheckFailed},
		{name: "binding check bug", err: oops.E(oops.CodeUnexpected, errors.New("bad query"), "check remote session client binding for validation"), code: mcpconnectionattempts.CodeInternalError, page: oauthui.ReasonCheckFailed},
		{name: "store outage", err: oops.E(oops.CodeUnexpected, redis.ErrClosed, "resolve upstream token projects for validation"), code: mcpconnectionattempts.CodeAuthCheckUnavailable, page: oauthui.ReasonCheckUnavailable},
		{name: "unroutable", err: oops.E(oops.CodeBadRequest, errRemoteSessionUnroutable, "This connection can't be checked here."), code: mcpconnectionattempts.CodeCredentialMisconfigured, page: oauthui.ReasonCheckNeedsAdmin},
		{name: "client misconfigured", err: fmt.Errorf("resolve: %w", remotesessions.ErrRemoteSessionMisconfigured), code: mcpconnectionattempts.CodeCredentialMisconfigured, page: oauthui.ReasonCheckNeedsAdmin},
		{name: "card not connected or expired", err: oops.E(oops.CodeBadRequest, noUsableCredentialError{cause: nil}, "Reconnect this service."), code: mcpconnectionattempts.CodeInvalidRemoteSession, page: oauthui.ReasonDefault},
		{name: "card grant lapsed", err: oops.E(oops.CodeBadRequest, noUsableCredentialError{cause: remotesessions.ErrNoValidToken}, "x"), code: mcpconnectionattempts.CodeInvalidRemoteSession, page: oauthui.ReasonDefault},
		{name: "card status unreadable", err: oops.E(oops.CodeBadRequest, noUsableCredentialError{cause: redis.ErrClosed}, "x"), code: mcpconnectionattempts.CodeAuthCheckUnavailable, page: oauthui.ReasonDefault},
		{name: "card client misconfigured", err: oops.E(oops.CodeBadRequest, noUsableCredentialError{cause: remotesessions.ErrRemoteSessionMisconfigured}, "x"), code: mcpconnectionattempts.CodeCredentialMisconfigured, page: oauthui.ReasonDefault},
		{name: "card token unobtainable", err: oops.E(oops.CodeBadRequest, noUsableCredentialError{cause: errors.New("grant is stored but its token could not be obtained")}, "x"), code: mcpconnectionattempts.CodeInternalError, page: oauthui.ReasonDefault},
		{name: "provider outage", err: memberConnectionError{errors.New("down"), mcpconnectionattempts.CodeProviderUnavailable}, code: mcpconnectionattempts.CodeProviderUnavailable, page: oauthui.ReasonCheckProviderUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			failure, ok := classifyCheckFailure(t.Context(), tc.err)
			require.True(t, ok)
			require.Equal(t, tc.code, failure.code)
			require.Equal(t, tc.page, failure.page)
		})
	}
}

func TestClassifyCheckFailure_RecordsNothingWithoutAFailedCheck(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		ended bool
		err   error
	}{
		"success":           {false, nil},
		"client canceled":   {true, oops.E(oops.CodeUnexpected, context.Canceled, "resolve upstream tokens for validation")},
		"deadline passed":   {true, oops.E(oops.CodeUnexpected, context.DeadlineExceeded, "resolve upstream tokens for validation")},
		"rate limited":      {false, errValidationRateLimited},
		"verdict has a row": {false, fmt.Errorf("%w: %w", errRemoteSessionVerdictNotRecorded, errors.New("write"))},
		"member offline":    {false, fmt.Errorf("%w: linear", errRemoteSessionMemberOffline)},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tc.ended {
				cancel()
			}
			_, ok := classifyCheckFailure(ctx, tc.err)
			require.False(t, ok)
		})
	}
}

// probeOnOrganizationGateway probes a card on an organization gateway; a client
// on another project fails before any lookup, one on no project looks up its projects.
func probeOnOrganizationGateway(t *testing.T, ctx context.Context, service *Service, clientProject uuid.NullUUID, trigger remotesessionmetrics.ValidationTrigger) error {
	t.Helper()
	subject := urn.NewUserSubject("user_1")
	endpoint := &ResolvedMcpEndpoint{RouteBase: mcproute.OrganizationGateway, ProjectID: uuid.New(), OrganizationID: "org_1", MetaMcpServerID: uuid.NullUUID{UUID: uuid.New(), Valid: true}}
	client := remotesessions.Client{ID: uuid.New(), ProjectID: clientProject, IssuerURL: "https://auth.example.com"}
	state := AuthnChallengeState{Subject: &subject, FlowID: uuid.NewString(), CreatedAt: time.Now()}
	return service.probeRemoteSession(ctx, remotesessions.RefreshAttribution{}, testenv.NewLogger(t), mcpconnectionattempts.Option{}, endpoint, state, client, nil, trigger)
}

// probeOnNoProject probes a client this organization gateway can't reach, which fails before any lookup.
func probeOnNoProject(t *testing.T, ctx context.Context, rec *mcpconnectionattemptstest.Recording, trigger remotesessionmetrics.ValidationTrigger) error {
	t.Helper()
	return probeOnOrganizationGateway(t, ctx, &Service{connections: rec.Recorder}, uuid.NullUUID{UUID: uuid.New(), Valid: true}, trigger)
}

func TestProbeRemoteSession_FailedCheckWritesOneRow(t *testing.T) {
	t.Parallel()

	rec := mcpconnectionattemptstest.New(t)
	require.Error(t, probeOnNoProject(t, t.Context(), rec, remotesessionmetrics.ValidationTriggerVerify))
	require.Len(t, rec.Rows(), 1)
	row := rec.RequireStep(mcpconnectionattempts.StepProviderSignIn, mcpconnectionattempts.CodeCredentialMisconfigured)
	require.Equal(t, string(mcpconnectionattempts.WhoAdmin), row.WhoFixes)
	require.Equal(t, string(remotesessionmetrics.ValidationTriggerVerify), row.SubReason)
}

func TestProbeRemoteSession_CanceledCheckWritesNothing(t *testing.T) {
	t.Parallel()

	rec := mcpconnectionattemptstest.New(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.Error(t, probeOnNoProject(t, ctx, rec, remotesessionmetrics.ValidationTriggerVerify))
	rec.RequireNothing()
}

func TestProbeRemoteSession_KeepaliveFailureWritesNothing(t *testing.T) {
	t.Parallel()

	rec := mcpconnectionattemptstest.New(t)
	require.Error(t, probeOnNoProject(t, t.Context(), rec, remotesessionmetrics.ValidationTriggerKeepalive))
	rec.RequireNothing()
}

func TestProbeRemoteSession_ConnectCheckFailureWritesOneRow(t *testing.T) {
	t.Parallel()

	rec := mcpconnectionattemptstest.New(t)
	require.Error(t, probeOnNoProject(t, t.Context(), rec, remotesessionmetrics.ValidationTriggerConnect))
	require.Len(t, rec.Rows(), 1)
	row := rec.RequireStep(mcpconnectionattempts.StepProviderSignIn, mcpconnectionattempts.CodeCredentialMisconfigured)
	require.Equal(t, string(remotesessionmetrics.ValidationTriggerConnect), row.SubReason)
}

func TestProbeRemoteSession_TimedOutCheckWritesNothing(t *testing.T) {
	t.Parallel()

	rec := mcpconnectionattemptstest.New(t)
	ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer cancel()
	require.Error(t, probeOnNoProject(t, ctx, rec, remotesessionmetrics.ValidationTriggerConnect))
	rec.RequireNothing()
}

// A store outage on Verify: one auth_check_unavailable row, and a 503 page that
// says try again and names Speakeasy support, after the outage handler rewrites the error.
func TestProbeRemoteSession_StoreOutageSaysRetryWithSpeakeasySupport(t *testing.T) {
	t.Parallel()

	// Nothing listens on port 1, so every query is a refused connection.
	pool, err := pgxpool.New(t.Context(), "postgres://127.0.0.1:1/unused?connect_timeout=1")
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	rec := mcpconnectionattemptstest.New(t)
	service := &Service{connections: rec.Recorder, db: pool, authEnforcement: authenforcement.NewGate(pool)}

	probeErr := probeOnOrganizationGateway(t, t.Context(), service, uuid.NullUUID{UUID: uuid.Nil, Valid: false}, remotesessionmetrics.ValidationTriggerVerify)
	require.Error(t, probeErr)
	require.Len(t, rec.Rows(), 1)
	row := rec.RequireStep(mcpconnectionattempts.StepProviderSignIn, mcpconnectionattempts.CodeAuthCheckUnavailable)
	require.Equal(t, string(mcpconnectionattempts.WhoSpeakeasy), row.WhoFixes)

	response := httptest.NewRecorder()
	oops.BrowserErrHandle(testenv.NewLogger(t), func(http.ResponseWriter, *http.Request) error {
		return checkFailureBrowserError(t.Context(), probeErr)
	}).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/connect", nil))
	require.Equal(t, http.StatusServiceUnavailable, response.Code)
	page := strings.ReplaceAll(strings.Join(strings.Fields(response.Body.String()), " "), "&#39;", "'")
	require.Contains(t, page, "Checking the server's connection")
	require.Contains(t, page, "Try again shortly. If it keeps happening, share the technical details below with Speakeasy support.")
	require.NotContains(t, page, "below with your IT administrators")
}
