package triggers_test

import (
	"context"
	"net/url"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/background/triggers"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	triggerrepo "github.com/speakeasy-api/gram/server/internal/triggers/repo"
)

// deliveryRecord is one LogTriggerDelivery call.
type deliveryRecord struct {
	status triggers.DeliveryStatus
	reason string
}

// recordingDeliveryLogger keeps every delivery outcome the app reports.
type recordingDeliveryLogger struct {
	mu      sync.Mutex
	records []deliveryRecord
}

func (r *recordingDeliveryLogger) LogTriggerDelivery(_ context.Context, _ triggerrepo.TriggerInstance, _ triggers.EventEnvelope, status triggers.DeliveryStatus, reason string, _ error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = append(r.records, deliveryRecord{status: status, reason: reason})
}

func (r *recordingDeliveryLogger) last(t *testing.T) deliveryRecord {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	require.NotEmpty(t, r.records, "the event must report a delivery outcome")
	return r.records[len(r.records)-1]
}

func TestProcessEventSkipsDisabledOrganization(t *testing.T) {
	t.Parallel()

	f := newIdentityFixture(t)
	recorder := &recordingDeliveryLogger{mu: sync.Mutex{}, records: nil}
	baseURL, err := url.Parse("https://example.invalid")
	require.NoError(t, err)
	app := triggers.NewApp(testenv.NewLogger(t), f.db, nil, identityEnvironmentLoader{}, recorder, nil, baseURL, baseURL, nil, nil, cache.NoopCache, testIdentityService)

	instance, err := f.app.Create(t.Context(), f.createParams())
	require.NoError(t, err)
	envelope := triggers.EventEnvelope{EventID: "evt-disabled-org"}

	_, _ = app.ProcessEvent(t.Context(), instance, envelope)
	require.NotEqual(t, "organization is disabled", recorder.last(t).reason, "an enabled organization must get past the organization check")

	require.NoError(t, testrepo.New(f.db).DisableDelegationOrganizationFixture(t.Context(), "org-trigger-test"))

	task, err := app.ProcessEvent(t.Context(), instance, envelope)
	require.NoError(t, err)
	require.Nil(t, task, "a disabled organization's trigger must not dispatch")
	require.Equal(t, deliveryRecord{status: triggers.DeliveryStatusSkipped, reason: "organization is disabled"}, recorder.last(t))
}
