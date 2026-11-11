package hooks

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
)

// installFailurePayload is a report as the bootstrap script sends it.
func installFailurePayload(attemptedAt time.Time) *gen.IngestPayload {
	occurredAt := attemptedAt.Format(time.RFC3339)
	payload := canonicalIngestPayload("bootstrap", eventTypeClientInstallFailed, "")
	payload.Session = nil
	payload.Event.OccurredAt = &occurredAt
	payload.IdempotencyKey = nil
	payload.Source.Hostname = new("dev-laptop")
	payload.Data = &gen.HookIngestData{
		InstallFailure: &gen.HookInstallFailureData{
			Cause:       "checksum_mismatch",
			AttemptedAt: occurredAt,
			Version:     new("0.3.46"),
			Os:          new("darwin"),
			Arch:        new("arm64"),
			Harness:     new("claude"),
		},
	}
	return payload
}

func findInstallFailurePoint(t *testing.T, rm metricdata.ResourceMetrics) metricdata.DataPoint[int64] {
	t.Helper()

	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != meterHooksInstallFailure {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			require.Len(t, sum.DataPoints, 1)
			return sum.DataPoints[0]
		}
	}
	require.Failf(t, "metric not found", "missing metric %q", meterHooksInstallFailure)
	return metricdata.DataPoint[int64]{}
}

func TestIngest_InstallFailureRecordsDiagnosticRowAndCounter(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	chClient := enableHookTelemetryLogger(t, ctx, ti)
	reader := deliveryFailureMetrics(t, ti)
	authCtx := hookAuthContext(t, ctx)

	attemptedAt := time.Now().UTC().Add(-2 * time.Minute).Truncate(time.Second)
	result, err := ti.service.Ingest(ctx, installFailurePayload(attemptedAt))
	require.NoError(t, err)
	require.Equal(t, "allow", result.Decision)

	logs := waitForHookLogs(t, ctx, chClient, authCtx.ProjectID.String(), "", attemptedAt, 1)
	row := logs[0]
	require.Equal(t, attemptedAt.UnixNano(), row.TimeUnixNano)
	require.Empty(t, row.GramChatID)

	var stored map[string]any
	require.NoError(t, json.Unmarshal([]byte(row.Attributes), &stored))
	gram, _ := stored["gram"].(map[string]any)
	event, _ := gram["event"].(map[string]any)
	require.Equal(t, string(telemetry.EventSourceHookDiagnostic), event["source"])
	hook, _ := gram["hook"].(map[string]any)
	require.Equal(t, eventTypeClientInstallFailed, hook["event"])
	require.Equal(t, "dev-laptop", hook["hostname"])
	install, _ := hook["install"].(map[string]any)
	require.Equal(t, "checksum_mismatch", install["cause"])
	require.Equal(t, "0.3.46", install["version"])
	device, _ := hook["device"].(map[string]any)
	require.Equal(t, "darwin", device["os"])
	require.Equal(t, "arm64", device["arch"])
	require.Equal(t, "claude", device["harness"])
	_, hasBinaryVersion := device["binary_version"]
	require.False(t, hasBinaryVersion, "a binary that failed to install must not read as the machine's version")

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))
	point := findInstallFailurePoint(t, rm)
	require.Equal(t, int64(1), point.Value)
	for key, want := range map[attr.Key]string{
		attr.HookInstallCauseKey:   "checksum_mismatch",
		attr.HookInstallVersionKey: "0.3.46",
		attr.HookDeviceHarnessKey:  "claude",
		attr.HookDeviceOSKey:       "darwin",
		attr.OrganizationSlugKey:   authCtx.OrganizationSlug,
	} {
		value, ok := point.Attributes.Value(key)
		require.True(t, ok, "missing metric dimension %s", key)
		require.Equal(t, want, value.AsString(), "metric dimension %s", key)
	}
}

func TestIngest_InstallFailureValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(payload *gen.IngestPayload)
		wantErr string
	}{
		{
			name:    "missing report block",
			mutate:  func(payload *gen.IngestPayload) { payload.Data = nil },
			wantErr: "data.install_failure is required",
		},
		{
			name:    "block on another event",
			mutate:  func(payload *gen.IngestPayload) { payload.Event.Type = "tool.requested" },
			wantErr: "data.install_failure is only valid",
		},
		{
			name: "session on the envelope",
			mutate: func(payload *gen.IngestPayload) {
				payload.Session = &gen.HookIngestSession{ID: new("session-1"), TurnID: nil, Cwd: nil, Model: nil}
			},
			wantErr: "session is not valid on client.install_failed",
		},
		{
			name: "activity content",
			mutate: func(payload *gen.IngestPayload) {
				payload.Data.Prompt = &gen.HookPromptData{Text: new("hello")}
			},
			wantErr: "data.prompt is not valid on client.install_failed",
		},
		{
			name: "delivery failure block",
			mutate: func(payload *gen.IngestPayload) {
				payload.Data.DeliveryFailure = deliveryFailurePayload(time.Now()).Data.DeliveryFailure
			},
			wantErr: "data.delivery_failure is only valid on client.delivery_failed",
		},
		{
			name:    "unknown cause",
			mutate:  func(payload *gen.IngestPayload) { payload.Data.InstallFailure.Cause = "disk_full" },
			wantErr: "not a known install failure cause",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestHooksService(t)
			payload := installFailurePayload(time.Now().UTC().Add(-time.Minute))
			tt.mutate(payload)

			result, err := ti.service.Ingest(ctx, payload)
			require.Error(t, err)
			require.Nil(t, result)
			require.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestIngest_DeliveryFailureRejectsInstallFailureBlock(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	payload := deliveryFailurePayload(time.Now().UTC().Add(-time.Hour))
	payload.Data.InstallFailure = installFailurePayload(time.Now()).Data.InstallFailure

	result, err := ti.service.Ingest(ctx, payload)
	require.Error(t, err)
	require.Nil(t, result)
	require.Contains(t, err.Error(), "data.install_failure is not valid on client.delivery_failed")
}
