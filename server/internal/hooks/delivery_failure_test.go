package hooks

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// deliveryFailurePayload is a report as the speakeasy-hooks binary spools it:
// no session on the envelope, occurred_at equal to the attempt time, and the
// device identity inside the report because the request that carries it is a
// later drain.
func deliveryFailurePayload(attemptedAt time.Time) *gen.IngestPayload {
	occurredAt := attemptedAt.Format(time.RFC3339Nano)
	payload := canonicalIngestPayload("claude", eventTypeClientDeliveryFailed, "")
	payload.Session = nil
	payload.Event.OccurredAt = &occurredAt
	payload.IdempotencyKey = new("delivery-failure-" + uuid.NewString())
	payload.Replayed = new(true)
	payload.Source.Hostname = new("dev-laptop")
	payload.Data = &gen.HookIngestData{
		DeliveryFailure: &gen.HookDeliveryFailureData{
			EventType:            "tool.requested",
			DeliveryPhase:        "live",
			Cause:                "timeout",
			StatusCode:           new(0),
			ElapsedMs:            new(4200),
			Attempts:             new(3),
			Gating:               new(true),
			AttemptedAt:          occurredAt,
			BinaryVersion:        new("1.4.0"),
			Os:                   new("darwin"),
			Arch:                 new("arm64"),
			Harness:              new("claude"),
			HarnessVariant:       new("claude-code"),
			HarnessVersion:       new("2.1.0"),
			FailedIdempotencyKey: new("evt-" + uuid.NewString()),
			SessionID:            new("session-" + uuid.NewString()),
			SpoolDepth:           new(7),
		},
	}
	return payload
}

// deliveryFailureMetrics swaps the service meter for one a test can read.
func deliveryFailureMetrics(t *testing.T, ti *testInstance) *sdkmetric.ManualReader {
	t.Helper()

	reader := sdkmetric.NewManualReader()
	ti.service.metrics = newMetrics(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)), testenv.NewLogger(t))
	return reader
}

func findDeliveryFailurePoint(t *testing.T, reader *sdkmetric.ManualReader) metricdata.DataPoint[int64] {
	t.Helper()

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != meterHooksDeliveryFailure {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			require.Len(t, sum.DataPoints, 1)
			return sum.DataPoints[0]
		}
	}

	require.Failf(t, "metric not found", "missing metric %q", meterHooksDeliveryFailure)
	return metricdata.DataPoint[int64]{}
}

// drainRequestContext stamps the headers of the binary draining the spool,
// which is not the binary that observed the failure.
func drainRequestContext(t *testing.T, ctx context.Context) context.Context {
	t.Helper()

	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/rpc/hooks.ingest", nil)
	req.Header.Set("X-Gram-Device-Binary-Version", "9.9.9")
	req.Header.Set("X-Gram-Device-Os", "linux")
	var requestCtx context.Context
	middleware.HookDeviceTelemetry(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		requestCtx = r.Context()
	})).ServeHTTP(httptest.NewRecorder(), req)
	require.NotNil(t, requestCtx)
	return requestCtx
}

func TestIngest_DeliveryFailureRecordsDiagnosticRowAndCounter(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	chClient := enableHookTelemetryLogger(t, ctx, ti)
	reader := deliveryFailureMetrics(t, ti)
	authCtx := hookAuthContext(t, ctx)

	attemptedAt := time.Now().UTC().Add(-3 * time.Hour).Truncate(time.Second)
	payload := deliveryFailurePayload(attemptedAt)
	report := payload.Data.DeliveryFailure

	result, err := ti.service.Ingest(drainRequestContext(t, ctx), payload)
	require.NoError(t, err)
	require.Equal(t, "allow", result.Decision)

	logs := waitForHookLogs(t, ctx, chClient, authCtx.ProjectID.String(), "", attemptedAt, 1)
	row := logs[0]
	require.Equal(t, attemptedAt.UnixNano(), row.TimeUnixNano, "the row carries the device-side attempt time, not arrival time")
	require.Empty(t, row.GramChatID, "a report must never resolve to a chat")

	var stored map[string]any
	require.NoError(t, json.Unmarshal([]byte(row.Attributes), &stored))
	gram, _ := stored["gram"].(map[string]any)
	event, _ := gram["event"].(map[string]any)
	require.Equal(t, string(telemetry.EventSourceHookDiagnostic), event["source"])
	hook, _ := gram["hook"].(map[string]any)
	require.Equal(t, eventTypeClientDeliveryFailed, hook["event"])
	require.Equal(t, true, hook["replayed"])
	require.Equal(t, "dev-laptop", hook["hostname"])
	delivery, _ := hook["delivery"].(map[string]any)
	require.Equal(t, "tool.requested", delivery["event_type"])
	require.Equal(t, "live", delivery["phase"])
	require.Equal(t, "timeout", delivery["cause"])
	require.InDelta(t, 0, delivery["status_code"], 0)
	require.InDelta(t, 4200, delivery["elapsed_ms"], 0)
	require.InDelta(t, 3, delivery["attempts"], 0)
	require.Equal(t, true, delivery["gating"])
	require.InDelta(t, 7, delivery["spool_depth"], 0)
	require.Equal(t, *report.FailedIdempotencyKey, delivery["failed_idempotency_key"])
	require.Equal(t, *report.SessionID, delivery["session_id"], "the failed event's session rides as a plain attribute")
	device, _ := hook["device"].(map[string]any)
	require.Equal(t, "1.4.0", device["binary_version"], "device identity comes from the report, not the draining binary's headers")
	require.Equal(t, "darwin", device["os"])
	require.Equal(t, "arm64", device["arch"])
	require.Equal(t, "claude", device["harness"])
	require.Equal(t, "claude-code", device["harness_variant"])
	require.Equal(t, "2.1.0", device["harness_version"])
	genAI, _ := stored["gen_ai"].(map[string]any)
	_, hasConversation := genAI["conversation"]
	require.False(t, hasConversation, "no gen_ai.conversation.id: the row must not feed session materialized views")

	point := findDeliveryFailurePoint(t, reader)
	require.Equal(t, int64(1), point.Value)
	for key, want := range map[attr.Key]string{
		attr.HookDeliveryCauseKey:       "timeout",
		attr.HookDeliveryPhaseKey:       "live",
		attr.HookDeliveryEventTypeKey:   "tool.requested",
		attr.HookDeviceHarnessKey:       "claude",
		attr.HookDeviceOSKey:            "darwin",
		attr.HookDeviceBinaryVersionKey: "1.4.0",
		attr.OrganizationSlugKey:        authCtx.OrganizationSlug,
	} {
		value, ok := point.Attributes.Value(key)
		require.True(t, ok, "missing metric dimension %s", key)
		require.Equal(t, want, value.AsString(), "metric dimension %s", key)
	}
}

func TestIngest_DeliveryFailureDuplicateDeliveryCountsOnce(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	reader := deliveryFailureMetrics(t, ti)
	payload := deliveryFailurePayload(time.Now().UTC().Add(-time.Hour))

	for range 2 {
		result, err := ti.service.Ingest(ctx, payload)
		require.NoError(t, err)
		require.Equal(t, "allow", result.Decision)
	}

	point := findDeliveryFailurePoint(t, reader)
	require.Equal(t, int64(1), point.Value, "a redelivered report under the same idempotency key is not a second failure")
}

func TestIngest_DeliveryFailureRequiresReportBlock(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	payload := deliveryFailurePayload(time.Now().UTC().Add(-time.Hour))
	payload.Data = nil

	result, err := ti.service.Ingest(ctx, payload)
	require.Error(t, err)
	require.Nil(t, result)
	require.Contains(t, err.Error(), "data.delivery_failure is required")
}

func TestIngest_DeliveryFailureBlockRejectedOnOtherEvents(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	payload := deliveryFailurePayload(time.Now().UTC().Add(-time.Hour))
	payload.Event.Type = "tool.requested"

	result, err := ti.service.Ingest(ctx, payload)
	require.Error(t, err)
	require.Nil(t, result)
	require.Contains(t, err.Error(), "data.delivery_failure is only valid")
}

func TestDeliveryFailureTime_PrefersOccurredAtAndClampsAttemptedAt(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 21, 12, 0, 0, 0, time.UTC)
	occurredAt := now.Add(-2 * time.Hour)
	attemptedAt := now.Add(-20 * 24 * time.Hour)

	payload := deliveryFailurePayload(attemptedAt)
	payload.Event.OccurredAt = new(occurredAt.Format(time.RFC3339Nano))
	require.Equal(t, occurredAt, deliveryFailureTime(payload, now), "the envelope time wins when present")

	payload.Event.OccurredAt = nil
	require.Equal(t, now.Add(-maxEventBackdate), deliveryFailureTime(payload, now), "attempted_at is clamped like any other event time")

	payload.Data.DeliveryFailure.AttemptedAt = now.Add(time.Hour).Format(time.RFC3339Nano)
	require.Equal(t, now, deliveryFailureTime(payload, now), "a future device clock is pinned to now")

	payload.Data.DeliveryFailure.AttemptedAt = "not-a-time"
	require.Equal(t, now, deliveryFailureTime(payload, now))
}
