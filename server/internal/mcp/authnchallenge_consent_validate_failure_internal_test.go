package mcp

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

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
		{name: "no usable credential shows on the card", err: oops.E(oops.CodeBadRequest, errRemoteSessionNoUsableCredential, "Reconnect this service."), code: mcpconnectionattempts.CodeInvalidRemoteSession, page: oauthui.ReasonDefault},
		{name: "provider outage", err: memberConnectionError{errors.New("down"), mcpconnectionattempts.CodeProviderUnavailable}, code: mcpconnectionattempts.CodeProviderUnavailable, page: oauthui.ReasonCheckUnavailable},
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
		canceled bool
		err      error
	}{
		"success":           {false, nil},
		"client canceled":   {true, oops.E(oops.CodeUnexpected, context.Canceled, "resolve upstream tokens for validation")},
		"rate limited":      {false, errValidationRateLimited},
		"verdict has a row": {false, fmt.Errorf("%w: %w", errRemoteSessionVerdictNotRecorded, errors.New("write"))},
		"member offline":    {false, fmt.Errorf("%w: linear", errRemoteSessionMemberOffline)},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tc.canceled {
				cancel()
			}
			_, ok := classifyCheckFailure(ctx, tc.err)
			require.False(t, ok)
		})
	}
}

// probeOnNoProject probes a client this organization gateway can't reach, which fails before any lookup.
func probeOnNoProject(t *testing.T, ctx context.Context, rec *mcpconnectionattemptstest.Recording, trigger remotesessionmetrics.ValidationTrigger) error {
	t.Helper()
	service := &Service{connections: rec.Recorder}
	subject := urn.NewUserSubject("user_1")
	endpoint := &ResolvedMcpEndpoint{RouteBase: mcproute.OrganizationGateway, ProjectID: uuid.New(), OrganizationID: "org_1", MetaMcpServerID: uuid.NullUUID{UUID: uuid.New(), Valid: true}}
	client := remotesessions.Client{ID: uuid.New(), ProjectID: uuid.NullUUID{UUID: uuid.New(), Valid: true}, IssuerURL: "https://auth.example.com"}
	state := AuthnChallengeState{Subject: &subject, FlowID: uuid.NewString(), CreatedAt: time.Now()}
	return service.probeRemoteSession(ctx, remotesessions.RefreshAttribution{}, testenv.NewLogger(t), mcpconnectionattempts.Option{}, endpoint, state, client, nil, trigger)
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
