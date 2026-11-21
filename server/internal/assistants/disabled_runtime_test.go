package assistants

import (
	"context"
	"maps"
	"reflect"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/assistants"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	bgtriggers "github.com/speakeasy-api/gram/server/internal/background/triggers"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// runtimeRequiredMethods start, drive, or switch an assistant runtime, so they
// refuse with 501 when the runtime provider is disabled.
var runtimeRequiredMethods = []string{
	"CreateAssistant",
	"EnsureManagedAssistant",
	"InterruptTurn",
	"SendMessage",
}

// configurationMethods only read or change stored configuration, so they keep
// working with the disabled provider. UpdateAssistant refuses only when it
// switches runtime versions; DeleteAssistant reaps through any backend that is
// still configured.
var configurationMethods = []string{
	"DeleteAssistant",
	"GetAssistant",
	"GetManagedAssistant",
	"ListAssistants",
	"ListExecutionProfiles",
	"SetExecutionProfileChoice",
	"UpdateAssistant",
}

func TestDisabledRuntimeEveryServiceMethodIsClassified(t *testing.T) {
	t.Parallel()

	serviceType := reflect.TypeFor[gen.Service]()
	methods := make([]string, 0, serviceType.NumMethod())
	for method := range serviceType.Methods() {
		methods = append(methods, method.Name)
	}
	classified := slices.Concat(runtimeRequiredMethods, configurationMethods)
	slices.Sort(methods)
	slices.Sort(classified)
	// A new assistants method must decide whether it needs a runtime.
	require.Equal(t, methods, classified)
}

func newDisabledRuntimeService(t *testing.T) (*Service, context.Context, uuid.UUID) {
	t.Helper()

	svc, ctx, projectID, conn := newRBACServiceWithConn(t, "assistants_disabled_runtime")
	router, err := newDisabledRuntimeBackend(map[string]RuntimeBackend{})
	require.NoError(t, err)
	logger := testenv.NewLogger(t)
	svc.core = NewServiceCore(logger, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, nil, nil, router, nil, nil, nil, telemetry.NewStub(logger), nil, newTestAuditLogger(), testIdentityService, newTestAuthzEngine(t, conn))
	return svc, ctx, projectID
}

func TestDisabledRuntimeRefusesRuntimeRequiredMethods(t *testing.T) {
	t.Parallel()

	svc, ctx, projectID := newDisabledRuntimeService(t)
	ctx = authztest.WithExactGrants(t, ctx, assistantWriteGrant(projectID), assistantReadGrant(projectID))
	assistantID := uuid.NewString()
	chatID := uuid.NewString()

	calls := map[string]func(context.Context) error{
		"CreateAssistant": func(ctx context.Context) error {
			_, err := svc.CreateAssistant(ctx, &gen.CreateAssistantPayload{Name: "Assistant", Model: "openai/gpt-4o-mini"})
			return err
		},
		"EnsureManagedAssistant": func(ctx context.Context) error {
			_, err := svc.EnsureManagedAssistant(ctx, &gen.EnsureManagedAssistantPayload{})
			return err
		},
		"InterruptTurn": func(ctx context.Context) error {
			_, err := svc.InterruptTurn(ctx, &gen.InterruptTurnPayload{AssistantID: assistantID, ChatID: chatID})
			return err
		},
		"SendMessage": func(ctx context.Context) error {
			_, err := svc.SendMessage(ctx, &gen.SendMessagePayload{AssistantID: assistantID, Message: "hello"})
			return err
		},
	}
	require.ElementsMatch(t, runtimeRequiredMethods, slices.Collect(maps.Keys(calls)))
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			requireOopsCode(t, call(ctx), oops.CodeNotImplemented)
		})
	}
}

func TestDisabledRuntimeAllowsConfigurationChanges(t *testing.T) {
	t.Parallel()

	svc, ctx, projectID, conn := newRBACServiceWithConn(t, "assistants_disabled_runtime_config")
	ctx = authztest.WithExactGrants(t, ctx, assistantWriteGrant(projectID), assistantReadGrant(projectID))

	// The assistant predates the switch to disabled.
	created, err := svc.CreateAssistant(ctx, &gen.CreateAssistantPayload{Name: "Assistant", Model: "openai/gpt-4o-mini"})
	require.NoError(t, err)

	router, err := newDisabledRuntimeBackend(map[string]RuntimeBackend{})
	require.NoError(t, err)
	logger := testenv.NewLogger(t)
	svc.core = NewServiceCore(logger, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, nil, nil, router, nil, nil, nil, telemetry.NewStub(logger), nil, newTestAuditLogger(), testIdentityService, newTestAuthzEngine(t, conn))

	listed, err := svc.ListAssistants(ctx, &gen.ListAssistantsPayload{})
	require.NoError(t, err)
	require.Len(t, listed.Assistants, 1)

	updated, err := svc.UpdateAssistant(ctx, &gen.UpdateAssistantPayload{ID: created.ID, Name: new("Renamed")})
	require.NoError(t, err)
	require.Equal(t, "Renamed", updated.Name)

	// Keeping the expected version is not a switch.
	_, err = svc.UpdateAssistant(ctx, &gen.UpdateAssistantPayload{ID: created.ID, RuntimeVersion: new(updated.RuntimeVersion), ExpectedRuntimeVersion: new(updated.RuntimeVersion)})
	require.NoError(t, err)

	_, err = svc.UpdateAssistant(ctx, &gen.UpdateAssistantPayload{ID: created.ID, RuntimeVersion: new(3), ExpectedRuntimeVersion: new(updated.RuntimeVersion)})
	requireOopsCode(t, err, oops.CodeNotImplemented)
}

func TestDisabledRuntimeSkipsBackgroundWork(t *testing.T) {
	t.Parallel()

	svc, _, _ := newDisabledRuntimeService(t)
	core := svc.core
	require.False(t, core.RuntimeAvailable())
	require.False(t, core.RuntimeHasBackends())
	require.Empty(t, core.RuntimeImageRef())

	enqueued, err := core.EnqueueTriggerTask(t.Context(), bgtriggers.Task{TargetRef: uuid.NewString()})
	require.NoError(t, err)
	require.False(t, enqueued.ShouldSignal)

	reaped, err := core.ReapInactiveAssistantRuntimes(t.Context(), ReapInactiveAssistantRuntimesParams{})
	require.NoError(t, err)
	require.Zero(t, reaped.Reaped)

	stopped, err := core.ReapStoppedAssistantRuntimes(t.Context(), ReapStoppedAssistantRuntimesParams{})
	require.NoError(t, err)
	require.Zero(t, stopped.Reaped)

	stuck, err := core.ReapStuckRuntimes(t.Context())
	require.NoError(t, err)
	require.Empty(t, stuck.AffectedAssistantIDs)

	recycled, err := core.RecycleActiveRuntimeImages(t.Context(), RecycleAssistantRuntimeImagesParams{})
	require.NoError(t, err)
	require.Zero(t, recycled.Recycled)

	require.False(t, svc.TargetAvailable())
}

func TestDisabledRuntimeWithConfiguredBackendReportsBackends(t *testing.T) {
	t.Parallel()

	router, err := newDisabledRuntimeBackend(map[string]RuntimeBackend{runtimeBackendGKE: &stubRuntimeBackend{name: runtimeBackendGKE}})
	require.NoError(t, err)
	logger := testenv.NewLogger(t)
	core := NewServiceCore(logger, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), nil, nil, nil, router, nil, nil, nil, telemetry.NewStub(logger), nil, newTestAuditLogger(), testIdentityService, newTestAuthzEngine(t, nil))

	// New runtimes are refused, while the still-configured backend keeps the
	// reaper and janitor registered.
	require.False(t, core.RuntimeAvailable())
	require.True(t, core.RuntimeHasBackends())
}
