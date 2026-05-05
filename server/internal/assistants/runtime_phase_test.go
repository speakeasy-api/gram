package assistants

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/superfly/fly-go"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

const phaseSpanPrefix = "assistants.runtime."

func TestFlyRuntimeBackendEmitsPhaseSpansOnColdCreate(t *testing.T) {
	t.Parallel()

	server := newTestAssistantRuntimeServer(t, false)
	backend, apiClient, flapsClient := newTestFlyRuntimeBackend(t, server)
	recorder := installRecordingTracer(t, backend)

	apiClient.getAppErr = errors.New("not found")
	apiClient.organization = &fly.Organization{ID: "org-123"}
	flapsClient.listMachines = []*fly.Machine{}
	flapsClient.launchMachine = &fly.Machine{
		ID:         "machine-1",
		State:      "started",
		Region:     "ord",
		InstanceID: "boot-1",
	}

	_, err := backend.Ensure(t.Context(), assistantRuntimeRecord{
		AssistantThreadID: uuid.New(),
		AssistantID:       uuid.New(),
		ProjectID:         uuid.New(),
		Backend:           runtimeBackendFlyIO,
	})
	require.NoError(t, err)

	spans := phaseSpansFrom(recorder)
	require.Equal(t, []string{
		"ensureApp",
		"resolveMachine",
		"launchMachine",
		"waitStarted",
		"waitHealth",
		"runtimeState",
	}, phaseNamesFrom(spans), "cold-create must open one span per setup phase, in order")

	for _, sp := range spans {
		require.Equal(t, codes.Unset, sp.Status().Code, "phase %s must not be marked errored", sp.Name())
	}

	require.True(t, boolAttr(t, spanByName(spans, "ensureApp"), attr.AssistantAppCreatedKey),
		"ensureApp must report app_created=true on a fresh app")
	require.True(t, boolAttr(t, spanByName(spans, "launchMachine"), attr.AssistantColdStartKey),
		"launchMachine must report cold_start=true")
	require.True(t, boolAttr(t, spanByName(spans, "waitStarted"), attr.AssistantColdStartKey),
		"waitStarted must report cold_start=true")
	require.True(t, boolAttr(t, spanByName(spans, "waitHealth"), attr.AssistantColdStartKey),
		"waitHealth must report cold_start=true once a launch happened")
	require.True(t, boolAttr(t, spanByName(spans, "runtimeState"), attr.AssistantColdStartKey),
		"runtimeState must report cold_start=true on cold create")
}

func TestFlyRuntimeBackendEmitsPhaseSpansOnWarmReuse(t *testing.T) {
	t.Parallel()

	server := newTestAssistantRuntimeServer(t, true)
	backend, apiClient, flapsClient := newTestFlyRuntimeBackend(t, server)
	recorder := installRecordingTracer(t, backend)

	threadID := uuid.New()
	apiClient.app = &fly.App{ID: "app-1", Name: "gram-asst-test"}
	flapsClient.machine = &fly.Machine{
		ID:         "machine-1",
		State:      "started",
		Region:     "iad",
		InstanceID: "boot-1",
		Config: &fly.MachineConfig{
			Metadata: map[string]string{flyMachineMetadataThreadID: threadID.String()},
		},
	}
	rawMetadata, err := json.Marshal(flyRuntimeMetadata{
		AppName:    "gram-asst-test",
		AppID:      "app-1",
		AppURL:     server.URL,
		AppIP:      "",
		MachineID:  "machine-1",
		Region:     "iad",
		LastBootID: "boot-1",
	})
	require.NoError(t, err)

	_, err = backend.Ensure(t.Context(), assistantRuntimeRecord{
		AssistantThreadID:   threadID,
		AssistantID:         uuid.New(),
		ProjectID:           uuid.New(),
		Backend:             runtimeBackendFlyIO,
		BackendMetadataJSON: rawMetadata,
	})
	require.NoError(t, err)

	spans := phaseSpansFrom(recorder)
	require.Equal(t, []string{
		"ensureApp",
		"resolveMachine",
		"waitHealth",
		"runtimeState",
	}, phaseNamesFrom(spans), "warm reuse must skip launchMachine and waitStarted")
	require.False(t, boolAttr(t, spanByName(spans, "runtimeState"), attr.AssistantColdStartKey),
		"warm reuse must report cold_start=false")
	require.False(t, boolAttr(t, spanByName(spans, "ensureApp"), attr.AssistantAppCreatedKey),
		"warm reuse must report app_created=false")
}

func TestFlyRuntimeBackendInstanceIDDriftMarksColdStartOnWaitAndStateSpans(t *testing.T) {
	t.Parallel()

	server := newTestAssistantRuntimeServer(t, true)
	backend, apiClient, flapsClient := newTestFlyRuntimeBackend(t, server)
	recorder := installRecordingTracer(t, backend)

	threadID := uuid.New()
	apiClient.app = &fly.App{ID: "app-1", Name: "gram-asst-test"}
	flapsClient.machine = &fly.Machine{
		ID:         "machine-1",
		State:      "started",
		Region:     "iad",
		InstanceID: "boot-2",
		Config: &fly.MachineConfig{
			Metadata: map[string]string{flyMachineMetadataThreadID: threadID.String()},
		},
	}
	rawMetadata, err := json.Marshal(flyRuntimeMetadata{
		AppName:    "gram-asst-test",
		AppID:      "app-1",
		AppURL:     server.URL,
		AppIP:      "",
		MachineID:  "machine-1",
		Region:     "iad",
		LastBootID: "boot-1",
	})
	require.NoError(t, err)

	result, err := backend.Ensure(t.Context(), assistantRuntimeRecord{
		AssistantThreadID:   threadID,
		AssistantID:         uuid.New(),
		ProjectID:           uuid.New(),
		Backend:             runtimeBackendFlyIO,
		BackendMetadataJSON: rawMetadata,
	})
	require.NoError(t, err)
	require.True(t, result.ColdStart, "instance-id drift must surface as a cold start in the ensure result")

	spans := phaseSpansFrom(recorder)
	require.True(t, boolAttr(t, spanByName(spans, "waitHealth"), attr.AssistantColdStartKey),
		"waitHealth must report cold_start=true once instance-id drift is known")
	require.True(t, boolAttr(t, spanByName(spans, "runtimeState"), attr.AssistantColdStartKey),
		"runtimeState must report cold_start=true once instance-id drift is known")
}

func TestFlyRuntimeBackendPhaseSpanOnLaunchFailureCarriesErrorAndClass(t *testing.T) {
	t.Parallel()

	server := newTestAssistantRuntimeServer(t, false)
	backend, apiClient, flapsClient := newTestFlyRuntimeBackend(t, server)
	recorder := installRecordingTracer(t, backend)

	apiClient.getAppErr = errors.New("not found")
	apiClient.organization = &fly.Organization{ID: "org-123"}
	flapsClient.listMachines = []*fly.Machine{}
	flapsClient.launchErr = errors.New("flaps: capacity exhausted")

	_, err := backend.Ensure(t.Context(), assistantRuntimeRecord{
		AssistantThreadID: uuid.New(),
		AssistantID:       uuid.New(),
		ProjectID:         uuid.New(),
		Backend:           runtimeBackendFlyIO,
	})
	require.Error(t, err)

	spans := phaseSpansFrom(recorder)
	require.NotEmpty(t, spans)
	last := spans[len(spans)-1]
	require.Equal(t, "launchMachine", phaseName(last), "failure must surface on the failing phase")
	require.Equal(t, codes.Error, last.Status().Code)
	require.Equal(t, "unknown", stringAttr(t, last, attr.AssistantSetupFailureClassKey),
		"unmapped flaps error falls into the unknown bucket")
}

func TestClassifySetupErrorBuckets(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want string
	}{
		{name: "nil", err: nil, want: ""},
		{name: "corrupted_app", err: errFlyAppCorrupted, want: "app_corrupted"},
		{name: "context_deadline", err: context.DeadlineExceeded, want: "timeout"},
		{name: "context_canceled", err: context.Canceled, want: "canceled"},
		{name: "explicit_timeout_message", err: errors.New("runtime health check timed out"), want: "timeout"},
		{name: "app_propagation", err: errors.New("failed to get app: no rows in result set"), want: "app_propagation"},
		{name: "not_found", err: errors.New("machine not found"), want: "not_found"},
		{name: "conflict", err: errors.New("name has already been taken"), want: "conflict"},
		{name: "ip_assigned", err: errors.New("ip already allocated"), want: "ip_already_assigned"},
		{name: "network", err: errors.New("dial tcp: connection refused"), want: "network"},
		{name: "unmapped", err: errors.New("flaps: capacity exhausted"), want: "unknown"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, classifySetupError(tc.err))
		})
	}
}

func installRecordingTracer(t *testing.T, backend *FlyRuntimeBackend) *tracetest.SpanRecorder {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	backend.tracer = tp.Tracer("assistants-test")
	return recorder
}

// phaseSpansFrom returns ended phase spans (those whose name carries the
// "assistants.runtime." prefix) in completion order.
func phaseSpansFrom(recorder *tracetest.SpanRecorder) []sdktrace.ReadOnlySpan {
	all := recorder.Ended()
	out := make([]sdktrace.ReadOnlySpan, 0, len(all))
	for _, sp := range all {
		if strings.HasPrefix(sp.Name(), phaseSpanPrefix) {
			out = append(out, sp)
		}
	}
	return out
}

func phaseNamesFrom(spans []sdktrace.ReadOnlySpan) []string {
	names := make([]string, 0, len(spans))
	for _, sp := range spans {
		names = append(names, phaseName(sp))
	}
	return names
}

func phaseName(span sdktrace.ReadOnlySpan) string {
	return span.Name()[len(phaseSpanPrefix):]
}

func spanByName(spans []sdktrace.ReadOnlySpan, name string) sdktrace.ReadOnlySpan {
	for i := len(spans) - 1; i >= 0; i-- {
		if phaseName(spans[i]) == name {
			return spans[i]
		}
	}
	return nil
}

func boolAttr(t *testing.T, span sdktrace.ReadOnlySpan, key attribute.Key) bool {
	t.Helper()
	require.NotNil(t, span, "missing span for attr lookup %q", key)
	for _, kv := range span.Attributes() {
		if kv.Key == key {
			return kv.Value.AsBool()
		}
	}
	return false
}

func stringAttr(t *testing.T, span sdktrace.ReadOnlySpan, key attribute.Key) string {
	t.Helper()
	require.NotNil(t, span, "missing span for attr lookup %q", key)
	for _, kv := range span.Attributes() {
		if kv.Key == key {
			return kv.Value.AsString()
		}
	}
	return ""
}
