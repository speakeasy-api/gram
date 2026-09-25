package aiintegrations

import (
	"context"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/ai_integrations"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

func newUpsertTestService(t *testing.T, baseURL string) (context.Context, *pgxpool.Pool, *Service, *fakeConfigPoller, string) {
	t.Helper()

	ctx, conn, store, orgID := newStoreTestDB(t)
	logger := testenv.NewLogger(t)
	poller := &fakeConfigPoller{calls: nil, returnErr: nil}
	service := &Service{
		tracer:       nil,
		logger:       logger,
		db:           conn,
		auth:         nil,
		authz:        authz.NewEngine(logger, conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient()),
		audit:        audit.NewLogger(),
		store:        store,
		credentials:  newTestCredentialVerifier(t, baseURL),
		configPoller: poller,
	}

	ctx = contextvalues.SetAuthContext(ctx, &contextvalues.AuthContext{
		ActiveOrganizationID: orgID,
		OrganizationSlug:     orgID,
		UserID:               "user_credentials_test",
		Email:                conv.PtrEmpty("admin@example.com"),
		SessionID:            conv.PtrEmpty("test_session"),
		AccountType:          "enterprise",
	})
	ctx = authz.GrantsToContext(ctx, []authz.Grant{authz.NewGrant(authz.ScopeOrgAdmin, orgID)})
	return ctx, conn, service, poller, orgID
}

// A key every one of the provider's feeds refuses never becomes a config:
// with nothing stored, no schedule is ever enqueued for it, so the poll loop
// that turned one bad key into a burst of activity failures never starts.
func TestUpsertConfigRefusesAKeyTheProviderRejectsEverywhere(t *testing.T) {
	t.Parallel()

	stub := newProviderStub(t, map[string]stubRoute{
		"/workspaces/" + testWorkspaceID + "/logs?event_type=CONVERSATION_MESSAGE": {status: http.StatusForbidden, body: `{"detail":"API key not authorized for enterprise logs"}`},
		"/workspaces/" + testWorkspaceID + "/logs?event_type=CODEX_LOG":            {status: http.StatusForbidden, body: `{"detail":"API key not authorized for enterprise logs"}`},
	})
	ctx, conn, service, poller, orgID := newUpsertTestService(t, stub.server.URL)

	_, err := service.UpsertConfig(ctx, &gen.UpsertConfigPayload{
		SessionToken:           nil,
		ApikeyToken:            nil,
		Provider:               ProviderChatGPTCompliance,
		APIKey:                 "chatgpt-key",
		Enabled:                true,
		ExternalOrganizationID: conv.PtrEmpty(testWorkspaceID),
		BillingMode:            nil,
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "API key not authorized for enterprise logs")
	require.Equal(t, int64(0), countAIIntegrationConfigs(t, ctx, conn, orgID, true))
	require.Empty(t, poller.calls)
}

// Providers entitle their feeds separately, so a key that reads some of them
// still saves. The refused feed is paused with the provider's message up
// front instead of learning the same answer over three failed polls.
func TestUpsertConfigPausesOnlyTheFeedTheProviderRefuses(t *testing.T) {
	t.Parallel()

	stub := newProviderStub(t, map[string]stubRoute{
		"/v1/compliance/activities": {status: http.StatusForbidden, body: `{"error":{"message":"compliance api not enabled"}}`},
	})
	ctx, _, service, poller, _ := newUpsertTestService(t, stub.server.URL)

	view, err := service.UpsertConfig(ctx, &gen.UpsertConfigPayload{
		SessionToken:           nil,
		ApikeyToken:            nil,
		Provider:               ProviderAnthropicCompliance,
		APIKey:                 "anthropic-key",
		Enabled:                true,
		ExternalOrganizationID: conv.PtrEmpty(testExternalOrgID),
		BillingMode:            nil,
	})
	require.NoError(t, err)
	require.True(t, view.HasAPIKey)

	schedules, err := service.ListSchedules(ctx, &gen.ListSchedulesPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		Provider:         ProviderAnthropicCompliance,
	})
	require.NoError(t, err)

	statuses := map[string]string{}
	for _, schedule := range schedules.Schedules {
		statuses[schedule.Schedule] = schedule.Status
		if schedule.Schedule == ScheduleAnthropicCompliance {
			require.NotNil(t, schedule.LastPollError)
			require.Contains(t, *schedule.LastPollError, "compliance api not enabled")
		}
	}
	require.Equal(t, map[string]string{
		ScheduleAnthropicCompliance:     "auto_paused",
		ScheduleAnthropicAnalyticsUsage: "pending",
		ScheduleAnthropicAnalyticsCost:  "pending",
	}, statuses)

	// The paused feed is not started either: only the two analytics
	// schedules get an immediate poll.
	require.Len(t, poller.calls, 2)
}

// The connection toggle and other settings-only saves reuse a key the
// provider already answered for, so they must not spend a round trip (or
// risk a provider outage) on re-verifying it.
func TestUpsertConfigSkipsVerificationForSettingsOnlySaves(t *testing.T) {
	t.Parallel()

	stub := newProviderStub(t, nil)
	ctx, _, service, _, _ := newUpsertTestService(t, stub.server.URL)

	_, err := service.UpsertConfig(ctx, &gen.UpsertConfigPayload{
		SessionToken:           nil,
		ApikeyToken:            nil,
		Provider:               ProviderCursor,
		APIKey:                 "cursor-key",
		Enabled:                true,
		ExternalOrganizationID: nil,
		BillingMode:            nil,
	})
	require.NoError(t, err)
	require.Len(t, stub.requested(), 1)

	_, err = service.UpsertConfig(ctx, &gen.UpsertConfigPayload{
		SessionToken:           nil,
		ApikeyToken:            nil,
		Provider:               ProviderCursor,
		APIKey:                 "",
		Enabled:                false,
		ExternalOrganizationID: nil,
		BillingMode:            nil,
	})
	require.NoError(t, err)
	require.Len(t, stub.requested(), 1)
}
