package assistants

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type cutoverStubKitBackend struct {
	name             string
	reapCalls        atomic.Int64
	reapedIDs        []uuid.UUID
	switchFenceCalls atomic.Int64
	switchBusy       atomic.Bool
}

func (b *cutoverStubKitBackend) Backend() string                  { return b.name }
func (b *cutoverStubKitBackend) SupportsBackend(name string) bool { return name == b.name }
func (b *cutoverStubKitBackend) ServerURL() *url.URL {
	return &url.URL{Scheme: "https", Host: "api.speakeasy.test"}
}
func (b *cutoverStubKitBackend) ImageRef() string {
	return "gcr.io/example/kit@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
}
func (b *cutoverStubKitBackend) Ensure(context.Context, assistantRuntimeRecord) (RuntimeBackendEnsureResult, error) {
	return RuntimeBackendEnsureResult{ColdStart: true, BackendMetadataJSON: []byte(`{"stub":true}`)}, nil
}
func (b *cutoverStubKitBackend) RecycleImage(context.Context, assistantRuntimeRecord) (RuntimeBackendRecycleResult, error) {
	return RuntimeBackendRecycleResult{Recycled: false, BackendMetadataJSON: nil}, nil
}
func (b *cutoverStubKitBackend) RunTurn(context.Context, assistantRuntimeRecord, runTurnRequest) error {
	return nil
}
func (b *cutoverStubKitBackend) InterruptTurn(context.Context, assistantRuntimeRecord, uuid.UUID) (bool, error) {
	return false, nil
}
func (b *cutoverStubKitBackend) Status(context.Context, assistantRuntimeRecord) (RuntimeBackendStatus, error) {
	return RuntimeBackendStatus{Configured: true, IdleSeconds: nil}, nil
}
func (b *cutoverStubKitBackend) Stop(context.Context, assistantRuntimeRecord) error { return nil }
func (b *cutoverStubKitBackend) Reap(_ context.Context, r assistantRuntimeRecord) error {
	b.reapCalls.Add(1)
	b.reapedIDs = append(b.reapedIDs, r.ID)
	return nil
}
func (b *cutoverStubKitBackend) ReapStoppedMachine(context.Context, assistantRuntimeRecord) error {
	return nil
}
func (b *cutoverStubKitBackend) RuntimeReadyForAdmission(context.Context) error { return nil }
func (b *cutoverStubKitBackend) PrepareRuntimeSwitch(_ context.Context, _ assistantRecord, _ []assistantRuntimeRecord, _ int16) (runtimeSwitchReadiness, error) {
	b.switchFenceCalls.Add(1)
	if b.switchBusy.Load() {
		return runtimeSwitchReadiness{Release: func() {}}, errRuntimeSwitchBusy
	}
	return runtimeSwitchReadiness{DestinationReady: true, Settled: true, Release: func() {}}, nil
}

func insertCutoverPendingEvent(t *testing.T, db *pgxpool.Pool, projectID, assistantID, threadID uuid.UUID, correlation string) uuid.UUID {
	t.Helper()
	id, err := assistantrepo.New(db).InsertAssistantThreadEvent(t.Context(), assistantrepo.InsertAssistantThreadEventParams{
		AssistantThreadID:     threadID,
		AssistantID:           assistantID,
		ProjectID:             projectID,
		TriggerInstanceID:     uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		EventID:               "evt-" + uuid.NewString(),
		CorrelationID:         correlation,
		Status:                eventStatusPending,
		NormalizedPayloadJson: []byte(`{"content":"hello"}`),
		SourcePayloadJson:     []byte(`{"content":"hello"}`),
	})
	require.NoError(t, err)
	return id
}

func newCutoverATEBackend(
	t *testing.T,
	core *ServiceCore,
	localCfg KitLocalCompositionConfig,
	logger *slog.Logger,
	meterProvider *sdkmetric.MeterProvider,
) (*KitATERuntimeBackend, *fakeATECluster) {
	t.Helper()
	const (
		atespace = "prod-assistants"
		template = "kit-v1"
	)
	if core.encryptionClient == nil {
		cipher, err := encryption.NewWithBytes(make([]byte, 32))
		require.NoError(t, err)
		core.encryptionClient = cipher
	}
	serverURL, err := url.Parse("https://api.speakeasy.test")
	require.NoError(t, err)
	core.serverURL = serverURL
	localCfg.Environment = "production"
	localCfg.nativeIdentity = nil
	localCfg.Origin = nil
	localCfg.WorkloadIssuerURL = ""
	localCfg.WorkloadJWKSURL = ""
	localCfg.WorkloadIssuerBaseURL = ""

	cluster, client := startFakeATEStack(t, atespace)
	comp := KitATECompositionConfig{
		KitLocalCompositionConfig: localCfg,
		Enabled:                   true,
		Atespace:                  atespace,
		Template:                  template,
		WorkerSelector:            testKitATEWorkerSelector,
	}
	meter := core.meter
	if meterProvider != nil {
		meter = meterProvider.Meter("test.ate")
	}
	backend, err := NewKitATERuntimeBackend(KitATERuntimeConfig{
		Image:          testKitATEImage,
		Atespace:       atespace,
		Template:       template,
		WorkerSelector: testKitATEWorkerSelector,
		ServerURL:      serverURL,
		Client:         client,
		Bootstrap:      core.kitATECompositionBootstrap(comp),
		Token:          core.kitATEToken,
		Protocol: func(_ context.Context, _ assistantRuntimeRecord, _ uuid.UUID, _ string, endpoint *url.URL, hc *guardian.HTTPClient, token string) (runtimeProtocol, error) {
			return &fakeATEProtocol{
				client:   hc,
				endpoint: endpoint,
				token:    token,
			}, nil
		},
		Lifecycle:          core.kitATELifecycle,
		ThreadLifecycle:    core.kitATEThreadLifecycle,
		Store:              core.kitATEActorStore(),
		Revoke:             core.kitATERevoke,
		CheckSettled:       core.kitATECheckSettled,
		CheckThreadSettled: core.kitATECheckThreadSettled,
		ClearThreadSession: core.kitATEClearThreadSession,
		Now:                nil,
		HelperPollInterval: 5 * time.Millisecond,
		Logger:             logger,
		Meter:              meter,
	})
	require.NoError(t, err)
	return backend, cluster
}

func TestKitATECutover_ForwardAdmissionRetiresGKEAndStartsFreshATE(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	core, db, runtimeSeed, _, localCfg, _ := kitWorkloadDBFixture(t)
	q := assistantrepo.New(db)

	require.NoError(t, q.SetAssistantDesiredRuntimeVersionForTest(ctx, assistantrepo.SetAssistantDesiredRuntimeVersionForTestParams{
		RuntimeVersion: 3, AssistantID: runtimeSeed.AssistantID, ProjectID: runtimeSeed.ProjectID,
	}))

	// Seed an existing live Kit GKE runtime row + prior ACP session row.
	threadID := insertATEThreadFixture(t, db, runtimeSeed.ProjectID, runtimeSeed.AssistantID, "cutover-fwd")
	require.NoError(t, q.ReserveAssistantRuntimeV2(ctx, assistantrepo.ReserveAssistantRuntimeV2Params{
		AssistantThreadID: threadID, AssistantID: runtimeSeed.AssistantID, ProjectID: runtimeSeed.ProjectID,
		Backend: runtimeBackendGKE, State: runtimeStateActive,
	}))
	oldRuntime, err := q.GetAssistantRuntimeV2(ctx, assistantrepo.GetAssistantRuntimeV2Params{
		ProjectID: runtimeSeed.ProjectID, AssistantID: runtimeSeed.AssistantID,
	})
	require.NoError(t, err)
	require.Equal(t, runtimeBackendGKE, oldRuntime.Backend)
	_, err = q.InsertKitRuntimeSession(ctx, assistantrepo.InsertKitRuntimeSessionParams{
		SessionID: "legacy-gke-acp-session", RuntimeID: oldRuntime.ID, ProjectID: runtimeSeed.ProjectID,
		AssistantID: runtimeSeed.AssistantID, ThreadID: threadID,
	})
	require.NoError(t, err)

	// Flip deployment to ATE enabled, with GKE kept in retiring-only cleanup.
	gkeStub := &cutoverStubKitBackend{name: runtimeBackendGKE}
	ateBackend, _ := newCutoverATEBackend(t, core, localCfg, testenv.NewLogger(t), nil)
	require.NoError(t, core.installKitRuntime(gkeStub))
	require.NoError(t, core.installKitRuntime(ateBackend))

	// Enqueue a turn and admit: old GKE row must retire via switch_retiring -> Reap -> stopped,
	// freeing assistant_runtimes_v2_one_per_assistant for a brand-new ATE runtime row.
	insertCutoverPendingEvent(t, db, runtimeSeed.ProjectID, runtimeSeed.AssistantID, threadID, "cutover-fwd")
	admitted, err := core.AdmitPendingThreads(ctx, runtimeSeed.AssistantID)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{threadID}, admitted.ThreadIDs)

	require.EqualValues(t, 1, gkeStub.reapCalls.Load())
	require.Equal(t, []uuid.UUID{oldRuntime.ID}, gkeStub.reapedIDs)

	oldAfter, err := q.GetAssistantRuntime(ctx, assistantrepo.GetAssistantRuntimeParams{ID: oldRuntime.ID, ProjectID: runtimeSeed.ProjectID})
	require.NoError(t, err)
	require.True(t, oldAfter.Ended)
	require.Equal(t, runtimeStateStopped, oldAfter.State)

	// Active slot now belongs to a new ATE runtime with no inherited Kit session.
	newRuntime, err := q.GetAssistantRuntimeV2(ctx, assistantrepo.GetAssistantRuntimeV2Params{
		ProjectID: runtimeSeed.ProjectID, AssistantID: runtimeSeed.AssistantID,
	})
	require.NoError(t, err)
	require.NotEqual(t, oldRuntime.ID, newRuntime.ID)
	require.Equal(t, runtimeBackendATE, newRuntime.Backend)
	require.Equal(t, int16(3), newRuntime.RuntimeVersion)

	binding, err := q.GetKitRuntimeSessionBinding(ctx, assistantrepo.GetKitRuntimeSessionBindingParams{
		RuntimeID: newRuntime.ID, ProjectID: runtimeSeed.ProjectID, AssistantID: runtimeSeed.AssistantID, ThreadID: threadID,
	})
	require.NoError(t, err)
	require.False(t, binding.AcpSessionID.Valid, "replaced ATE runtime must start a fresh session")
}

func TestKitATECutover_RollbackAdmissionRetiresATEActorsAndAdmissions(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	core, db, runtimeSeed, _, localCfg, _ := kitWorkloadDBFixture(t)
	q := assistantrepo.New(db)

	require.NoError(t, q.SetAssistantDesiredRuntimeVersionForTest(ctx, assistantrepo.SetAssistantDesiredRuntimeVersionForTestParams{
		RuntimeVersion: 3, AssistantID: runtimeSeed.AssistantID, ProjectID: runtimeSeed.ProjectID,
	}))

	// Start on ATE and run one turn so an ATE actor row + ATE cluster actor + workload admission exist.
	ateBackend, cluster := newCutoverATEBackend(t, core, localCfg, testenv.NewLogger(t), nil)
	require.NoError(t, core.installKitRuntime(ateBackend))

	threadID := insertATEThreadFixture(t, db, runtimeSeed.ProjectID, runtimeSeed.AssistantID, "cutover-rollback")
	require.NoError(t, q.ReserveAssistantRuntimeV2(ctx, assistantrepo.ReserveAssistantRuntimeV2Params{
		AssistantThreadID: threadID, AssistantID: runtimeSeed.AssistantID, ProjectID: runtimeSeed.ProjectID,
		Backend: runtimeBackendATE, State: runtimeStateActive,
	}))
	ateRuntimeRow, err := q.GetAssistantRuntimeV2(ctx, assistantrepo.GetAssistantRuntimeV2Params{
		ProjectID: runtimeSeed.ProjectID, AssistantID: runtimeSeed.AssistantID,
	})
	require.NoError(t, err)
	ateRecord := assistantRuntimeRecord{
		ID: ateRuntimeRow.ID, AssistantThreadID: ateRuntimeRow.AssistantThreadID,
		AssistantID: ateRuntimeRow.AssistantID, ProjectID: ateRuntimeRow.ProjectID,
		RuntimeVersion: ateRuntimeRow.RuntimeVersion, Backend: ateRuntimeRow.Backend,
		BackendMetadataJSON: ateRuntimeRow.BackendMetadataJson, State: ateRuntimeRow.State,
		WarmUntil: ateRuntimeRow.WarmUntil,
	}
	ensureRes, err := ateBackend.Ensure(ctx, ateRecord)
	require.NoError(t, err)
	require.NoError(t, q.UpdateAssistantRuntimeMetadata(ctx, assistantrepo.UpdateAssistantRuntimeMetadataParams{
		ProjectID: runtimeSeed.ProjectID, RuntimeID: ateRuntimeRow.ID, BackendMetadataJson: ensureRes.BackendMetadataJSON,
	}))
	ateRecord.BackendMetadataJSON = ensureRes.BackendMetadataJSON
	require.NoError(t, ateBackend.RunTurn(ctx, ateRecord, runTurnRequest{
		ThreadID: threadID, Model: "openai/gpt-4.1-mini", ReasoningEffort: "medium",
	}))

	// Confirm actor row + ATE actor + live credential-mint eligibility before rollback.
	actorsBefore, err := q.ListAssistantATEActorsForRuntime(ctx, assistantrepo.ListAssistantATEActorsForRuntimeParams{
		ProjectID: runtimeSeed.ProjectID, RuntimeID: ateRuntimeRow.ID,
	})
	require.NoError(t, err)
	require.Len(t, actorsBefore, 1)
	var wb KitWorkloadBinding
	require.NoError(t, json.Unmarshal(actorsBefore[0].WorkloadBindingJson, &wb))
	require.NotEmpty(t, wb.Subject)
	require.Equal(t, uuid.Nil, wb.IssuerID)

	_, err = q.GetLiveAssistantATEActorByUID(ctx, assistantrepo.GetLiveAssistantATEActorByUIDParams{
		ActorUid:              actorsBefore[0].ActorUid,
		RunningState:          ateActorStateRunning,
		RuntimeStartingState:  runtimeStateStarting,
		RuntimeActiveState:    runtimeStateActive,
		AssistantActiveStatus: StatusActive,
	})
	require.NoError(t, err)

	// Roll back to GKE with ATE retained strictly in retiring-only cleanup.
	gkeStub := &cutoverStubKitBackend{name: runtimeBackendGKE}
	require.NoError(t, core.installKitRuntime(gkeStub))

	insertCutoverPendingEvent(t, db, runtimeSeed.ProjectID, runtimeSeed.AssistantID, threadID, "cutover-rollback")
	admitted, err := core.AdmitPendingThreads(ctx, runtimeSeed.AssistantID)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{threadID}, admitted.ThreadIDs)

	// Old ATE runtime ended & stopped; active runtime is now GKE.
	ateAfter, err := q.GetAssistantRuntime(ctx, assistantrepo.GetAssistantRuntimeParams{ID: ateRuntimeRow.ID, ProjectID: runtimeSeed.ProjectID})
	require.NoError(t, err)
	require.True(t, ateAfter.Ended)
	require.Equal(t, runtimeStateStopped, ateAfter.State)

	newRuntime, err := q.GetAssistantRuntimeV2(ctx, assistantrepo.GetAssistantRuntimeV2Params{
		ProjectID: runtimeSeed.ProjectID, AssistantID: runtimeSeed.AssistantID,
	})
	require.NoError(t, err)
	require.Equal(t, runtimeBackendGKE, newRuntime.Backend)
	require.NotEqual(t, ateRuntimeRow.ID, newRuntime.ID)

	// ATE actor deleted in ATE, row soft-deleted, and live credential-mint lookup rejected.
	actorsAfter, err := q.ListAssistantATEActorsForRuntime(ctx, assistantrepo.ListAssistantATEActorsForRuntimeParams{
		ProjectID: runtimeSeed.ProjectID, RuntimeID: ateRuntimeRow.ID,
	})
	require.NoError(t, err)
	require.Empty(t, actorsAfter)

	cluster.mu.Lock()
	_, existsInATE := cluster.actors[actorsBefore[0].ActorName]
	cluster.mu.Unlock()
	require.False(t, existsInATE, "retired ATE actor must be deleted in ATE on rollback")

	_, err = q.GetLiveAssistantATEActorByUID(ctx, assistantrepo.GetLiveAssistantATEActorByUIDParams{
		ActorUid:              actorsBefore[0].ActorUid,
		RunningState:          ateActorStateRunning,
		RuntimeStartingState:  runtimeStateStarting,
		RuntimeActiveState:    runtimeStateActive,
		AssistantActiveStatus: StatusActive,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows, "retired ATE actor must not be eligible for credential minting after rollback")
}

func TestKitATECutover_SwitchReaperConvergesIdleOldBackendRuntimesBothDirections(t *testing.T) {
	t.Parallel()

	t.Run("ATE enabled reaps idle GKE runtime without a turn and skips busy ones", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()
		core, db, runtimeSeed, _, localCfg, _ := kitWorkloadDBFixture(t)
		q := assistantrepo.New(db)
		require.NoError(t, q.SetAssistantDesiredRuntimeVersionForTest(ctx, assistantrepo.SetAssistantDesiredRuntimeVersionForTestParams{
			RuntimeVersion: 3, AssistantID: runtimeSeed.AssistantID, ProjectID: runtimeSeed.ProjectID,
		}))

		threadID := insertATEThreadFixture(t, db, runtimeSeed.ProjectID, runtimeSeed.AssistantID, "idle-gke")
		require.NoError(t, q.ReserveAssistantRuntimeV2(ctx, assistantrepo.ReserveAssistantRuntimeV2Params{
			AssistantThreadID: threadID, AssistantID: runtimeSeed.AssistantID, ProjectID: runtimeSeed.ProjectID,
			Backend: runtimeBackendGKE, State: runtimeStateActive,
		}))
		oldGKE, err := q.GetAssistantRuntimeV2(ctx, assistantrepo.GetAssistantRuntimeV2Params{
			ProjectID: runtimeSeed.ProjectID, AssistantID: runtimeSeed.AssistantID,
		})
		require.NoError(t, err)

		gkeStub := &cutoverStubKitBackend{name: runtimeBackendGKE}
		ateBackend, _ := newCutoverATEBackend(t, core, localCfg, testenv.NewLogger(t), nil)
		require.NoError(t, core.installKitRuntime(gkeStub))
		require.NoError(t, core.installKitRuntime(ateBackend))

		// First: with an unsettled event, the switch reaper must NOT retire it mid-turn.
		evtID := insertCutoverPendingEvent(t, db, runtimeSeed.ProjectID, runtimeSeed.AssistantID, threadID, "idle-gke")
		res, err := core.ReapStoppedAssistantRuntimes(ctx, ReapStoppedAssistantRuntimesParams{StoppedTTL: 24 * time.Hour, BatchSize: 50})
		require.NoError(t, err)
		require.Zero(t, res.Errors)
		require.Zero(t, gkeStub.reapCalls.Load(), "busy old-backend runtime must not be retired mid-turn")

		// Settle the event -> now the GKE runtime is idle and never receives another turn.
		require.NoError(t, q.FailAssistantThreadEvent(ctx, assistantrepo.FailAssistantThreadEventParams{
			FailedStatus: "failed", LastError: pgtype.Text{String: "settled", Valid: true},
			EventID: evtID, ProjectID: runtimeSeed.ProjectID,
		}))

		res, err = core.ReapStoppedAssistantRuntimes(ctx, ReapStoppedAssistantRuntimesParams{StoppedTTL: 24 * time.Hour, BatchSize: 50})
		require.NoError(t, err)
		require.Zero(t, res.Errors)
		require.EqualValues(t, 1, gkeStub.reapCalls.Load())

		after, err := q.GetAssistantRuntime(ctx, assistantrepo.GetAssistantRuntimeParams{ID: oldGKE.ID, ProjectID: runtimeSeed.ProjectID})
		require.NoError(t, err)
		require.True(t, after.Ended)
		require.Equal(t, runtimeStateStopped, after.State)
	})

	t.Run("ATE disabled preserves GKE and reaps idle ATE runtimes on rollback", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()
		core, db, runtimeSeed, _, localCfg, _ := kitWorkloadDBFixture(t)
		q := assistantrepo.New(db)
		require.NoError(t, q.SetAssistantDesiredRuntimeVersionForTest(ctx, assistantrepo.SetAssistantDesiredRuntimeVersionForTestParams{
			RuntimeVersion: 3, AssistantID: runtimeSeed.AssistantID, ProjectID: runtimeSeed.ProjectID,
		}))

		ateBackend, _ := newCutoverATEBackend(t, core, localCfg, testenv.NewLogger(t), nil)
		require.NoError(t, core.installKitRuntime(ateBackend))

		threadID := insertATEThreadFixture(t, db, runtimeSeed.ProjectID, runtimeSeed.AssistantID, "idle-ate-rollback")
		require.NoError(t, q.ReserveAssistantRuntimeV2(ctx, assistantrepo.ReserveAssistantRuntimeV2Params{
			AssistantThreadID: threadID, AssistantID: runtimeSeed.AssistantID, ProjectID: runtimeSeed.ProjectID,
			Backend: runtimeBackendATE, State: runtimeStateActive,
		}))
		ateRow, err := q.GetAssistantRuntimeV2(ctx, assistantrepo.GetAssistantRuntimeV2Params{
			ProjectID: runtimeSeed.ProjectID, AssistantID: runtimeSeed.AssistantID,
		})
		require.NoError(t, err)
		ateRec := assistantRuntimeRecord{
			ID: ateRow.ID, AssistantThreadID: ateRow.AssistantThreadID,
			AssistantID: ateRow.AssistantID, ProjectID: ateRow.ProjectID,
			RuntimeVersion: ateRow.RuntimeVersion, Backend: ateRow.Backend,
			BackendMetadataJSON: ateRow.BackendMetadataJson, State: ateRow.State, WarmUntil: ateRow.WarmUntil,
		}
		ensureRes, err := ateBackend.Ensure(ctx, ateRec)
		require.NoError(t, err)
		require.NoError(t, q.UpdateAssistantRuntimeMetadata(ctx, assistantrepo.UpdateAssistantRuntimeMetadataParams{
			ProjectID: runtimeSeed.ProjectID, RuntimeID: ateRow.ID, BackendMetadataJson: ensureRes.BackendMetadataJSON,
		}))
		ateRec.BackendMetadataJSON = ensureRes.BackendMetadataJSON
		require.NoError(t, ateBackend.RunTurn(ctx, ateRec, runTurnRequest{
			ThreadID: threadID, Model: "openai/gpt-4.1-mini", ReasoningEffort: "medium",
		}))

		// Flip back to GKE (ATE disabled, ATE kept in retiring-only cleanup).
		gkeStub := &cutoverStubKitBackend{name: runtimeBackendGKE}
		require.NoError(t, core.installKitRuntime(gkeStub))

		// Idle ATE runtime converges without receiving another turn.
		res, err := core.ReapStoppedAssistantRuntimes(ctx, ReapStoppedAssistantRuntimesParams{StoppedTTL: 24 * time.Hour, BatchSize: 50})
		require.NoError(t, err)
		require.Zero(t, res.Errors)

		after, err := q.GetAssistantRuntime(ctx, assistantrepo.GetAssistantRuntimeParams{ID: ateRow.ID, ProjectID: runtimeSeed.ProjectID})
		require.NoError(t, err)
		require.True(t, after.Ended)
		require.Equal(t, runtimeStateStopped, after.State)

		remainingActors, err := q.ListAssistantATEActorsForRuntime(ctx, assistantrepo.ListAssistantATEActorsForRuntimeParams{
			ProjectID: runtimeSeed.ProjectID, RuntimeID: ateRow.ID,
		})
		require.NoError(t, err)
		require.Empty(t, remainingActors)

		// Now reserve a GKE runtime while ATE is disabled: reaper must leave it untouched.
		require.NoError(t, q.ReserveAssistantRuntimeV2(ctx, assistantrepo.ReserveAssistantRuntimeV2Params{
			AssistantThreadID: threadID, AssistantID: runtimeSeed.AssistantID, ProjectID: runtimeSeed.ProjectID,
			Backend: runtimeBackendGKE, State: runtimeStateActive,
		}))
		res, err = core.ReapStoppedAssistantRuntimes(ctx, ReapStoppedAssistantRuntimesParams{StoppedTTL: 24 * time.Hour, BatchSize: 50})
		require.NoError(t, err)
		require.Zero(t, res.Errors)
		require.Zero(t, gkeStub.reapCalls.Load(), "active GKE runtime must remain untouched when ATE is disabled")
	})
}

func TestKitATE_RecordsColdStartAndResumeDurationMetricsAndLogs(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	core, db, runtimeSeed, _, localCfg, _ := kitWorkloadDBFixture(t)
	q := assistantrepo.New(db)
	require.NoError(t, q.SetAssistantDesiredRuntimeVersionForTest(ctx, assistantrepo.SetAssistantDesiredRuntimeVersionForTestParams{
		RuntimeVersion: 3, AssistantID: runtimeSeed.AssistantID, ProjectID: runtimeSeed.ProjectID,
	}))

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = meterProvider.Shutdown(context.Background()) })

	ateBackend, _ := newCutoverATEBackend(t, core, localCfg, logger, meterProvider)
	require.NoError(t, core.installKitRuntime(ateBackend))

	threadID := insertATEThreadFixture(t, db, runtimeSeed.ProjectID, runtimeSeed.AssistantID, "metrics-thread")
	require.NoError(t, q.ReserveAssistantRuntimeV2(ctx, assistantrepo.ReserveAssistantRuntimeV2Params{
		AssistantThreadID: threadID, AssistantID: runtimeSeed.AssistantID, ProjectID: runtimeSeed.ProjectID,
		Backend: runtimeBackendATE, State: runtimeStateActive,
	}))
	row, err := q.GetAssistantRuntimeV2(ctx, assistantrepo.GetAssistantRuntimeV2Params{
		ProjectID: runtimeSeed.ProjectID, AssistantID: runtimeSeed.AssistantID,
	})
	require.NoError(t, err)
	rec := assistantRuntimeRecord{
		ID: row.ID, AssistantThreadID: row.AssistantThreadID,
		AssistantID: row.AssistantID, ProjectID: row.ProjectID,
		RuntimeVersion: row.RuntimeVersion, Backend: row.Backend,
		BackendMetadataJSON: row.BackendMetadataJson, State: row.State, WarmUntil: row.WarmUntil,
	}
	ensureRes, err := ateBackend.Ensure(ctx, rec)
	require.NoError(t, err)
	require.NoError(t, q.UpdateAssistantRuntimeMetadata(ctx, assistantrepo.UpdateAssistantRuntimeMetadataParams{
		ProjectID: runtimeSeed.ProjectID, RuntimeID: row.ID, BackendMetadataJson: ensureRes.BackendMetadataJSON,
	}))
	rec.BackendMetadataJSON = ensureRes.BackendMetadataJSON

	// 1. Cold start turn.
	require.NoError(t, ateBackend.RunTurn(ctx, rec, runTurnRequest{
		ThreadID: threadID, Model: "openai/gpt-4.1-mini", ReasoningEffort: "medium",
	}))
	// 2. Suspend then resume turn.
	require.NoError(t, ateBackend.Stop(ctx, rec))
	require.NoError(t, ateBackend.RunTurn(ctx, rec, runTurnRequest{
		ThreadID: threadID, Model: "openai/gpt-4.1-mini", ReasoningEffort: "medium",
	}))

	// Verify structured logs carry both cold_start (create_to_ready_ms) and resume (resume_to_ready_ms).
	logs := logBuf.String()
	require.Contains(t, logs, `"msg":"ATE thread actor ready"`)
	require.Contains(t, logs, `"gram.assistant.ate.startup_mode":"cold_start"`)
	require.Contains(t, logs, `"gram.assistant.ate.create_to_ready_ms":`)
	require.Contains(t, logs, `"gram.assistant.ate.startup_mode":"resume"`)
	require.Contains(t, logs, `"gram.assistant.ate.resume_to_ready_ms":`)

	// Verify OTel histogram recorded both startup_mode labels.
	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(ctx, &rm))
	seenModes := map[string]bool{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != meterATEActorReadyDuration {
				continue
			}
			hist, ok := m.Data.(metricdata.Histogram[float64])
			require.True(t, ok)
			for _, dp := range hist.DataPoints {
				if val, exists := dp.Attributes.Value("gram.assistant.ate.startup_mode"); exists {
					seenModes[val.AsString()] = true
				}
			}
		}
	}
	require.True(t, seenModes[ateStartupModeColdStart], "expected cold_start histogram data point")
	require.True(t, seenModes[ateStartupModeResume], "expected resume histogram data point")

	// Also verify KitATERuntimeBackend supports runtime-version switch readiness checks
	// and requires the caller's LifecycleTx advisory lock fence.
	require.NoError(t, ateBackend.RuntimeReadyForAdmission(ctx))
	rec.LifecycleTx = nil
	_, err = ateBackend.PrepareRuntimeSwitch(ctx, assistantRecord{
		ID: runtimeSeed.AssistantID, ProjectID: runtimeSeed.ProjectID,
	}, []assistantRuntimeRecord{rec}, 2)
	require.ErrorIs(t, err, ErrKitRuntimeBinding)

	tx := testenv.BeginTx(t, ctx, db)
	require.NoError(t, assistantrepo.New(tx).AcquireAssistantAdvisoryLock(ctx, runtimeSeed.AssistantID.String()))
	rec.LifecycleTx = tx
	readiness, err := ateBackend.PrepareRuntimeSwitch(ctx, assistantRecord{
		ID: runtimeSeed.AssistantID, ProjectID: runtimeSeed.ProjectID,
	}, []assistantRuntimeRecord{rec}, 2)
	require.NoError(t, err)
	require.True(t, readiness.Settled)
	require.NotNil(t, readiness.Release)
	require.NoError(t, tx.Rollback(ctx))
	readiness.Release()
}

func TestKitATECutover_BusySupersededRuntimeDefersAdmissionAndWarmup(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	core, db, runtimeSeed, _, localCfg, _ := kitWorkloadDBFixture(t)
	q := assistantrepo.New(db)

	require.NoError(t, q.SetAssistantDesiredRuntimeVersionForTest(ctx, assistantrepo.SetAssistantDesiredRuntimeVersionForTestParams{
		RuntimeVersion: 3, AssistantID: runtimeSeed.AssistantID, ProjectID: runtimeSeed.ProjectID,
	}))

	busyThread := insertATEThreadFixture(t, db, runtimeSeed.ProjectID, runtimeSeed.AssistantID, "cutover-busy-1")
	pendingThread := insertATEThreadFixture(t, db, runtimeSeed.ProjectID, runtimeSeed.AssistantID, "cutover-busy-2")
	require.NoError(t, q.ReserveAssistantRuntimeV2(ctx, assistantrepo.ReserveAssistantRuntimeV2Params{
		AssistantThreadID: busyThread, AssistantID: runtimeSeed.AssistantID, ProjectID: runtimeSeed.ProjectID,
		Backend: runtimeBackendGKE, State: runtimeStateActive,
	}))
	oldRuntime, err := q.GetAssistantRuntimeV2(ctx, assistantrepo.GetAssistantRuntimeV2Params{
		ProjectID: runtimeSeed.ProjectID, AssistantID: runtimeSeed.AssistantID,
	})
	require.NoError(t, err)

	// Mark an in-flight processing event on busyThread and a pending event on pendingThread.
	busyEvtID := insertCutoverPendingEvent(t, db, runtimeSeed.ProjectID, runtimeSeed.AssistantID, busyThread, "cutover-busy-1")
	require.NoError(t, q.SetAssistantThreadEventStatus(ctx, assistantrepo.SetAssistantThreadEventStatusParams{
		Status: eventStatusProcessing, UpdatedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true}, ID: busyEvtID, ProjectID: runtimeSeed.ProjectID,
	}))
	insertCutoverPendingEvent(t, db, runtimeSeed.ProjectID, runtimeSeed.AssistantID, pendingThread, "cutover-busy-2")

	gkeStub := &cutoverStubKitBackend{name: runtimeBackendGKE}
	ateBackend, _ := newCutoverATEBackend(t, core, localCfg, testenv.NewLogger(t), nil)
	require.NoError(t, core.installKitRuntime(gkeStub))
	require.NoError(t, core.installKitRuntime(ateBackend))

	// Both admission and warmup must defer without retiring the busy GKE runtime.
	admitted, err := core.AdmitPendingThreads(ctx, runtimeSeed.AssistantID)
	require.NoError(t, err)
	require.Empty(t, admitted.ThreadIDs)
	require.Zero(t, gkeStub.reapCalls.Load())

	warmup, err := core.EnsureWarmupThread(ctx, runtimeSeed.AssistantID)
	require.NoError(t, err)
	require.False(t, warmup.ShouldSignal)
	require.Zero(t, gkeStub.reapCalls.Load())

	stillOld, err := q.GetAssistantRuntimeV2(ctx, assistantrepo.GetAssistantRuntimeV2Params{
		ProjectID: runtimeSeed.ProjectID, AssistantID: runtimeSeed.AssistantID,
	})
	require.NoError(t, err)
	require.Equal(t, oldRuntime.ID, stillOld.ID)

	// Also verify versionedRuntimeBackend.PrepareRuntimeSwitch reaches retiring GKE rows.
	tx := testenv.BeginTx(t, ctx, db)
	require.NoError(t, assistantrepo.New(tx).AcquireAssistantAdvisoryLock(ctx, runtimeSeed.AssistantID.String()))
	coord, ok := core.runtime.(runtimeSwitchCoordinator)
	require.True(t, ok)
	ready, err := coord.PrepareRuntimeSwitch(ctx, assistantRecord{
		ID: runtimeSeed.AssistantID, ProjectID: runtimeSeed.ProjectID,
	}, []assistantRuntimeRecord{{
		LifecycleTx: tx, ID: oldRuntime.ID, AssistantThreadID: oldRuntime.AssistantThreadID,
		AssistantID: oldRuntime.AssistantID, ProjectID: oldRuntime.ProjectID,
		RuntimeVersion: 3, Backend: runtimeBackendGKE, State: runtimeStateActive,
	}}, 2)
	require.NoError(t, err)
	require.True(t, ready.Settled)
	require.EqualValues(t, 1, gkeStub.switchFenceCalls.Load())
	require.NoError(t, tx.Rollback(ctx))
	ready.Release()

	// Once the in-flight event settles, pendingThread admission cuts over cleanly.
	require.NoError(t, q.SetAssistantThreadEventStatus(ctx, assistantrepo.SetAssistantThreadEventStatusParams{
		Status: eventStatusCompleted, UpdatedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true}, ID: busyEvtID, ProjectID: runtimeSeed.ProjectID,
	}))
	admitted, err = core.AdmitPendingThreads(ctx, runtimeSeed.AssistantID)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{pendingThread}, admitted.ThreadIDs)
	require.EqualValues(t, 1, gkeStub.reapCalls.Load())
	require.EqualValues(t, 2, gkeStub.switchFenceCalls.Load())
}
