package triggers_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/stretchr/testify/require"

	bgtriggers "github.com/speakeasy-api/gram/server/internal/background/triggers"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
	triggerrepo "github.com/speakeasy-api/gram/server/internal/triggers/repo"
)

func TestGoogleChatRenewalScansOnlyActiveOptedInTriggers(t *testing.T) {
	t.Parallel()
	fixture := newIdentityFixture(t)
	queries := triggerrepo.New(fixture.db)
	for _, spec := range []struct {
		name, status                    string
		autoRenew, environment, deleted bool
	}{
		{"active", "active", true, true, false},
		{"paused", "paused", true, true, false},
		{"manual", "active", false, true, false},
		{"unconfigured", "active", true, false, false},
		{"deleted", "active", true, true, true},
	} {
		config, err := json.Marshal(map[string]any{
			"space_names": []string{"spaces/test"},
			"workspace_events": map[string]any{
				"auto_renew": spec.autoRenew, "subscription": "projects/example/subscriptions/push",
				"workspace_subscription": "subscriptions/" + spec.name, "audience": "https://example.test/push",
				"service_account_email": "push@example.iam.gserviceaccount.com", "user_names": []string{"users/test"}, "text_prefix": "assistant:",
			},
		})
		require.NoError(t, err)
		env := uuid.NullUUID{UUID: fixture.environmentID, Valid: spec.environment}
		instance, err := queries.CreateTriggerInstance(t.Context(), triggerrepo.CreateTriggerInstanceParams{
			OrganizationID: "org-trigger-test", ProjectID: fixture.projectID, DefinitionSlug: "googlechat", Name: spec.name,
			EnvironmentID: env, TargetKind: "assistant", TargetRef: fixture.assistantID.String(), TargetDisplay: "Test assistant", ConfigJson: config, Status: spec.status,
		})
		require.NoError(t, err)
		if spec.deleted {
			_, err = queries.DeleteTriggerInstance(t.Context(), triggerrepo.DeleteTriggerInstanceParams{ID: instance.ID, ProjectID: fixture.projectID})
			require.NoError(t, err)
		}
	}
	now := time.Now()
	var calls atomic.Int32
	err := fixture.app.RenewGoogleChatSubscriptions(t.Context(), now, func(_ context.Context, env toolconfig.ToolCallEnv, subscription, space string, supplied time.Time) (bool, error) {
		calls.Add(1)
		require.Equal(t, "subscriptions/active", subscription)
		require.Equal(t, "spaces/test", space)
		require.Equal(t, now, supplied)
		require.Equal(t, "test-secret", env.SystemEnv.Get("GITHUB_WEBHOOK_SECRET"), "loads the trigger's existing environment")
		return true, nil
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, calls.Load())
}

func TestGoogleChatRenewalPaginatesAndContinuesAfterFailure(t *testing.T) {
	t.Parallel()
	fixture := newIdentityFixture(t)
	queries := triggerrepo.New(fixture.db)
	const count = 105
	for i := range count {
		config, err := json.Marshal(map[string]any{
			"space_names": []string{"spaces/test"},
			"workspace_events": map[string]any{
				"auto_renew": true, "subscription": "projects/example/subscriptions/push",
				"workspace_subscription": fmt.Sprintf("subscriptions/sub-%d", i), "audience": "https://example.test/push",
				"service_account_email": "push@example.iam.gserviceaccount.com", "user_names": []string{"users/test"}, "text_prefix": "assistant:",
			},
		})
		require.NoError(t, err)
		_, err = queries.CreateTriggerInstance(t.Context(), triggerrepo.CreateTriggerInstanceParams{
			OrganizationID: "org-trigger-test", ProjectID: fixture.projectID, DefinitionSlug: "googlechat", Name: fmt.Sprint(i),
			EnvironmentID: uuid.NullUUID{UUID: fixture.environmentID, Valid: true}, TargetKind: "assistant", TargetRef: fixture.assistantID.String(), TargetDisplay: "Test assistant", ConfigJson: config, Status: "active",
		})
		require.NoError(t, err)
	}
	base, err := url.Parse("https://example.test")
	require.NoError(t, err)
	var mu sync.Mutex
	var failures, successes int
	logger := bgtriggers.NewTriggerDeliveryLogger(func(_ context.Context, event bgtriggers.TriggerDeliveryLog) {
		mu.Lock()
		defer mu.Unlock()
		if event.Attributes[attr.TriggerDeliveryStatusKey] == "failed" {
			failures++
		} else {
			successes++
		}
	})
	app := bgtriggers.NewApp(testenv.NewLogger(t), fixture.db, nil, identityEnvironmentLoader{}, logger, nil, base, base, nil, nil, cache.NoopCache, testIdentityService)
	var calls atomic.Int32
	err = app.RenewGoogleChatSubscriptions(t.Context(), time.Now(), func(_ context.Context, _ toolconfig.ToolCallEnv, subscription, _ string, _ time.Time) (bool, error) {
		calls.Add(1)
		if subscription == "subscriptions/sub-0" {
			return false, errors.New("provider unavailable")
		}
		return true, nil
	})
	require.ErrorContains(t, err, "1 Google Chat subscription renewals failed")
	require.EqualValues(t, count, calls.Load())
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, 1, failures)
	require.Equal(t, count-1, successes)
}
