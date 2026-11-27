package assistants

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	ateapiv1 "go.ate.dev/substrate/api/ateapi/v1"

	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	workloadrepo "github.com/speakeasy-api/gram/server/internal/workloadpolicy/repo"
)

type capturingATEActorDB struct {
	assistantrepo.DBTX
	query string
	args  []any
}

func (db *capturingATEActorDB) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	db.query = query
	db.args = args
	return db.DBTX.QueryRow(ctx, query, args...) //nolint:glint // notestingrawsql: wrapper forwards SQLc-generated query while capturing SQL for EXPLAIN
}

func insertATEThreadFixture(t *testing.T, db *pgxpool.Pool, projectID, assistantID uuid.UUID, correlation string) uuid.UUID {
	t.Helper()
	ctx := t.Context()
	q := assistantrepo.New(db)
	chatID := uuid.New()
	require.NoError(t, q.UpsertAssistantChat(ctx, assistantrepo.UpsertAssistantChatParams{
		ChatID:         chatID,
		ProjectID:      projectID,
		OrganizationID: "org-test",
		Title:          pgtype.Text{},
	}))
	threadID, err := q.UpsertAssistantThread(ctx, assistantrepo.UpsertAssistantThreadParams{
		AssistantID:   assistantID,
		ProjectID:     projectID,
		CorrelationID: correlation,
		ChatID:        chatID,
		SourceKind:    sourceKindSlack,
		SourceRefJson: []byte("{}"),
	})
	require.NoError(t, err)
	return threadID
}

func TestKitATEConcurrentThreadsDoNotWaitOnEachOther(t *testing.T) {
	t.Parallel()
	const (
		atespace = "prod-assistants"
		template = "kit-v1"
	)

	core, db, runtimeSeed, identity, localCfg, _ := kitWorkloadDBFixture(t)
	cipher, err := encryption.NewWithBytes(make([]byte, 32))
	require.NoError(t, err)
	core.encryptionClient = cipher

	origin, err := url.Parse("https://ai.speakeasy.com")
	require.NoError(t, err)
	localCfg.Environment = "production"
	localCfg.nativeIdentity = nil
	localCfg.Origin = origin
	localCfg.WorkloadIssuerURL = "https://ate-issuer.example.com"
	localCfg.WorkloadJWKSURL = "https://ate-issuer.example.com/jwks"
	localCfg.WorkloadIssuerBaseURL = "https://auth.example.com"

	q := assistantrepo.New(db)
	require.NoError(t, q.SetAssistantDesiredRuntimeVersionForTest(t.Context(), assistantrepo.SetAssistantDesiredRuntimeVersionForTestParams{
		RuntimeVersion: 3,
		AssistantID:    runtimeSeed.AssistantID,
		ProjectID:      runtimeSeed.ProjectID,
	}))

	thread1 := insertATEThreadFixture(t, db, runtimeSeed.ProjectID, runtimeSeed.AssistantID, "corr-ate-1")
	thread2 := insertATEThreadFixture(t, db, runtimeSeed.ProjectID, runtimeSeed.AssistantID, "corr-ate-2")

	runtimeID := insertActiveV2RuntimeRow(t, db, runtimeSeed.ProjectID, runtimeSeed.AssistantID, uuid.Nil, runtimeBackendATE, runtimeStateActive, `{}`)
	require.NoError(t, q.SetKitRuntimeMetadataFixture(t.Context(), assistantrepo.SetKitRuntimeMetadataFixtureParams{
		RuntimeVersion: 3,
		Metadata:       []byte(`{}`),
		ProjectID:      runtimeSeed.ProjectID,
		RuntimeID:      runtimeID,
	}))

	cluster, client := startFakeATEStack(t, atespace)
	compCfg := KitATECompositionConfig{
		KitLocalCompositionConfig: localCfg,
		Enabled:                   true,
		Atespace:                  atespace,
		Template:                  template,
		WorkerSelector:            testKitATEWorkerSelector,
		EgressHosts:               []string{"ai.speakeasy.com"},
	}

	var protoMu sync.Mutex
	protoTurns := map[uuid.UUID]int{}
	protoTokens := map[uuid.UUID]string{}

	backend, err := NewKitATERuntimeBackend(KitATERuntimeConfig{
		Image:           testKitATEImage,
		Atespace:        atespace,
		Template:        template,
		WorkerSelector:  testKitATEWorkerSelector,
		ServerURL:       core.serverURL,
		Client:          client,
		EgressHosts:     []string{"ai.speakeasy.com"},
		Bootstrap:       core.kitATECompositionBootstrap(compCfg),
		Token:           core.kitATEToken,
		Lifecycle:       core.kitATELifecycle,
		ThreadLifecycle: core.kitATEThreadLifecycle,
		Store:           core.kitATEActorStore(),
		Revoke:          core.kitATERevoke,
		CheckSettled:    core.kitATECheckSettled,
		Protocol: func(_ context.Context, _ assistantRuntimeRecord, thread uuid.UUID, _ string, endpoint *url.URL, hc *guardian.HTTPClient, token string) (runtimeProtocol, error) {
			protoMu.Lock()
			protoTokens[thread] = token
			protoMu.Unlock()
			return &fakeATEProtocol{
				client:   hc,
				endpoint: endpoint,
				token:    token,
			}, nil
		},
		HelperPollInterval: 5 * time.Millisecond,
	})
	require.NoError(t, err)

	record := assistantRuntimeRecord{
		ID:                  runtimeID,
		AssistantID:         runtimeSeed.AssistantID,
		ProjectID:           runtimeSeed.ProjectID,
		RuntimeVersion:      3,
		Backend:             runtimeBackendATE,
		BackendMetadataJSON: []byte("{}"),
	}

	ensureRes, err := backend.Ensure(t.Context(), record)
	require.NoError(t, err)
	require.True(t, ensureRes.ColdStart)
	require.NoError(t, q.UpdateAssistantRuntimeMetadata(t.Context(), assistantrepo.UpdateAssistantRuntimeMetadataParams{
		BackendMetadataJson: ensureRes.BackendMetadataJSON,
		RuntimeID:           runtimeID,
		ProjectID:           runtimeSeed.ProjectID,
	}))
	record.BackendMetadataJSON = ensureRes.BackendMetadataJSON

	// 1. Thread 1 runs its first turn, creating its row in assistant_ate_actors
	// and initializing its workload admission and derived ACP token.
	require.NoError(t, backend.RunTurn(t.Context(), record, runTurnRequest{
		ThreadID:        thread1,
		Model:           "openai/gpt-4o-mini",
		ReasoningEffort: "medium",
	}))
	protoTurns[thread1]++

	// 2. Acquire and HOLD the FOR UPDATE row lock on thread 1's actor row in tx1.
	tx1 := testenv.BeginTx(t, t.Context(), db)
	lockedRow1, err := assistantrepo.New(tx1).LockAssistantATEActorForThread(t.Context(), assistantrepo.LockAssistantATEActorForThreadParams{
		ProjectID: runtimeSeed.ProjectID,
		RuntimeID: runtimeID,
		ThreadID:  thread1,
	})
	require.NoError(t, err)
	require.Equal(t, thread1, lockedRow1.AssistantThreadID)

	// Confirm thread 1's row lock is genuinely held by tx1.
	probeCtx, probeCancel := context.WithTimeout(t.Context(), 150*time.Millisecond)
	probeTx := testenv.BeginTx(t, probeCtx, db)
	_, lockErr := assistantrepo.New(probeTx).LockAssistantATEActorForThread(probeCtx, assistantrepo.LockAssistantATEActorForThreadParams{
		ProjectID: runtimeSeed.ProjectID,
		RuntimeID: runtimeID,
		ThreadID:  thread1,
	})
	probeCancel()
	_ = probeTx.Rollback(t.Context())
	require.Error(t, lockErr, "thread 1 row lock must block concurrent writers on thread 1")

	// 3. While tx1 still holds thread 1's row lock, thread 2 must prepare,
	// authenticate (provision workload admission + derive token), and execute
	// turns without waiting on thread 1.
	t2Ctx, t2Cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer t2Cancel()
	require.NoError(t, backend.RunTurn(t2Ctx, record, runTurnRequest{
		ThreadID:        thread2,
		Model:           "openai/gpt-4o-mini",
		ReasoningEffort: "medium",
	}))
	// Also verify a warm second turn on thread 2 completes while tx1 still holds thread 1's lock.
	require.NoError(t, backend.RunTurn(t2Ctx, record, runTurnRequest{
		ThreadID:        thread2,
		Model:           "openai/gpt-4o-mini",
		ReasoningEffort: "medium",
	}))

	// Both actors are directly addressable by actor_uid while tx1 is still open.
	row2, err := q.GetAssistantATEActorForThread(t.Context(), assistantrepo.GetAssistantATEActorForThreadParams{
		ProjectID: runtimeSeed.ProjectID,
		RuntimeID: runtimeID,
		ThreadID:  thread2,
	})
	require.NoError(t, err)
	require.NotEqual(t, lockedRow1.ActorUid, row2.ActorUid)

	byUID1, err := q.GetAssistantATEActorByUID(t.Context(), assistantrepo.GetAssistantATEActorByUIDParams{
		OrganizationID: identity.OrganizationID,
		ProjectID:      runtimeSeed.ProjectID,
		ActorUid:       lockedRow1.ActorUid,
	})
	require.NoError(t, err)
	require.Equal(t, lockedRow1.ID, byUID1.ID)

	byUID2, err := q.GetAssistantATEActorByUID(t.Context(), assistantrepo.GetAssistantATEActorByUIDParams{
		OrganizationID: identity.OrganizationID,
		ProjectID:      runtimeSeed.ProjectID,
		ActorUid:       row2.ActorUid,
	})
	require.NoError(t, err)
	require.Equal(t, row2.ID, byUID2.ID)

	// Cross-tenant UID lookup is rejected.
	_, err = q.GetAssistantATEActorByUID(t.Context(), assistantrepo.GetAssistantATEActorByUIDParams{
		OrganizationID: "org-other",
		ProjectID:      runtimeSeed.ProjectID,
		ActorUid:       row2.ActorUid,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)

	require.NoError(t, tx1.Commit(t.Context()))

	// Verify distinct tokens and workload admissions exist for both threads, and
	// assistant_runtimes.backend_metadata_json never holds per-thread JSON state.
	protoMu.Lock()
	require.NotEmpty(t, protoTokens[thread1])
	require.NotEmpty(t, protoTokens[thread2])
	require.NotEqual(t, protoTokens[thread1], protoTokens[thread2])
	protoMu.Unlock()

	runtimeRow, err := q.GetAssistantRuntime(t.Context(), assistantrepo.GetAssistantRuntimeParams{
		ProjectID: runtimeSeed.ProjectID,
		ID:        runtimeID,
	})
	require.NoError(t, err)
	require.NotContains(t, string(runtimeRow.BackendMetadataJson), `"threads"`)

	var binding1, binding2 KitWorkloadBinding
	require.NoError(t, json.Unmarshal(lockedRow1.WorkloadBindingJson, &binding1))
	require.NoError(t, json.Unmarshal(row2.WorkloadBindingJson, &binding2))
	require.Equal(t, "actor/"+atespace+"/"+lockedRow1.ActorName, binding1.Subject)
	require.Equal(t, "actor/"+atespace+"/"+row2.ActorName, binding2.Subject)

	admissions, err := workloadrepo.New(db).ListWorkloadAdmissions(t.Context(), workloadrepo.ListWorkloadAdmissionsParams{
		OrganizationID: identity.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: runtimeSeed.ProjectID, Valid: true},
	})
	require.NoError(t, err)
	subjects := make([]string, 0, len(admissions))
	for _, a := range admissions {
		subjects = append(subjects, a.Subject)
	}
	require.Contains(t, subjects, binding1.Subject)
	require.Contains(t, subjects, binding2.Subject)
	require.Equal(t, int32(3), cluster.acpRequestCount.Load())
}

func TestKitATEActorLookupByUIDUsesUniqueIndex(t *testing.T) {
	t.Parallel()
	db, err := assistantsInfra.CloneTestDatabase(t, "ate_actor_uid_index")
	require.NoError(t, err)

	projectID, assistantID, _, thread1 := insertAssistantFixture(t, db)
	q := assistantrepo.New(db)
	require.NoError(t, q.SetAssistantDesiredRuntimeVersionForTest(t.Context(), assistantrepo.SetAssistantDesiredRuntimeVersionForTestParams{
		RuntimeVersion: 3,
		AssistantID:    assistantID,
		ProjectID:      projectID,
	}))
	runtimeID := insertActiveV2RuntimeRow(t, db, projectID, assistantID, uuid.Nil, runtimeBackendATE, runtimeStateActive, `{}`)
	require.NoError(t, q.SetKitRuntimeMetadataFixture(t.Context(), assistantrepo.SetKitRuntimeMetadataFixtureParams{
		RuntimeVersion: 3,
		Metadata:       []byte(`{}`),
		ProjectID:      projectID,
		RuntimeID:      runtimeID,
	}))

	thread2 := insertATEThreadFixture(t, db, projectID, assistantID, "corr-uid-2")

	actor1, err := q.InsertAssistantATEActor(t.Context(), assistantrepo.InsertAssistantATEActorParams{
		ActorName:           "spk-kit-actor-one",
		ActorUid:            "uid-actor-one",
		State:               ateActorStateRunning,
		Configured:          true,
		WorkloadBindingJson: nil,
		WarmUntil:           pgtype.Timestamptz{},
		RuntimeID:           runtimeID,
		ProjectID:           projectID,
		AssistantID:         assistantID,
		ThreadID:            thread1,
	})
	require.NoError(t, err)

	// Duplicate live (runtime_id, assistant_thread_id) is rejected by unique index.
	_, err = q.InsertAssistantATEActor(t.Context(), assistantrepo.InsertAssistantATEActorParams{
		ActorName:   "spk-kit-actor-dup-thread",
		ActorUid:    "uid-actor-dup-thread",
		State:       ateActorStateRunning,
		Configured:  false,
		RuntimeID:   runtimeID,
		ProjectID:   projectID,
		AssistantID: assistantID,
		ThreadID:    thread1,
	})
	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	require.Equal(t, pgerrcode.UniqueViolation, pgErr.Code)

	// Duplicate live actor_uid is rejected by unique index.
	_, err = q.InsertAssistantATEActor(t.Context(), assistantrepo.InsertAssistantATEActorParams{
		ActorName:   "spk-kit-actor-dup-uid",
		ActorUid:    "uid-actor-one",
		State:       ateActorStateRunning,
		Configured:  false,
		RuntimeID:   runtimeID,
		ProjectID:   projectID,
		AssistantID: assistantID,
		ThreadID:    thread2,
	})
	require.ErrorAs(t, err, &pgErr)
	require.Equal(t, pgerrcode.UniqueViolation, pgErr.Code)

	// Soft-deleting actor1 hides it from GetAssistantATEActorByUID and allows
	// inserting a replacement live actor for thread1.
	rowsAffected, err := q.SoftDeleteAssistantATEActor(t.Context(), assistantrepo.SoftDeleteAssistantATEActorParams{
		RetiredState: ateActorStateRetired,
		ProjectID:    projectID,
		ID:           actor1.ID,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), rowsAffected)

	_, err = q.GetAssistantATEActorByUID(t.Context(), assistantrepo.GetAssistantATEActorByUIDParams{
		OrganizationID: "org-test",
		ProjectID:      projectID,
		ActorUid:       "uid-actor-one",
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)

	replacement, err := q.InsertAssistantATEActor(t.Context(), assistantrepo.InsertAssistantATEActorParams{
		ActorName:   "spk-kit-actor-one-v2",
		ActorUid:    "uid-actor-one-v2",
		State:       ateActorStateRunning,
		Configured:  true,
		RuntimeID:   runtimeID,
		ProjectID:   projectID,
		AssistantID: assistantID,
		ThreadID:    thread1,
	})
	require.NoError(t, err)

	// Capture the actual SQLc-generated query and verify PostgreSQL uses the
	// unique index assistant_ate_actors_actor_uid_key.
	tx := testenv.BeginTx(t, t.Context(), db)

	capture := &capturingATEActorDB{DBTX: tx}
	found, err := assistantrepo.New(capture).GetAssistantATEActorByUID(t.Context(), assistantrepo.GetAssistantATEActorByUIDParams{
		OrganizationID: "org-test",
		ProjectID:      projectID,
		ActorUid:       replacement.ActorUid,
	})
	require.NoError(t, err)
	require.Equal(t, replacement.ID, found.ID)
	require.Equal(t, replacement.ActorUid, found.ActorUid)

	//nolint:glint // notestingrawsql: Session-level planner control must apply to the same connection as EXPLAIN.
	_, err = tx.Exec(t.Context(), "SET LOCAL enable_seqscan = off")
	require.NoError(t, err)

	query := capture.query
	if newline := strings.IndexByte(query, '\n'); newline >= 0 && strings.HasPrefix(query, "--") {
		query = query[newline+1:]
	}
	//nolint:glint // notestingrawsql: EXPLAINs the captured SQLc-generated UID lookup query
	rows, err := tx.Query(t.Context(), "EXPLAIN (COSTS OFF) "+query, capture.args...)
	require.NoError(t, err)
	defer rows.Close()

	var planLines []string
	for rows.Next() {
		var line string
		require.NoError(t, rows.Scan(&line))
		planLines = append(planLines, line)
	}
	require.NoError(t, rows.Err())
	plan := strings.Join(planLines, "\n")
	require.Contains(t, plan, "Index Scan using assistant_ate_actors_actor_uid_key")
}

func TestKitATEPerThreadIdleAndDrainSweep(t *testing.T) {
	t.Parallel()
	const (
		atespace = "prod-assistants"
		template = "kit-v1"
	)

	core, db, runtimeSeed, _, localCfg, _ := kitWorkloadDBFixture(t)
	cipher, err := encryption.NewWithBytes(make([]byte, 32))
	require.NoError(t, err)
	core.encryptionClient = cipher

	origin, err := url.Parse("https://ai.speakeasy.com")
	require.NoError(t, err)
	localCfg.Environment = "production"
	localCfg.nativeIdentity = nil
	localCfg.Origin = origin
	localCfg.WorkloadIssuerURL = "https://ate-issuer.example.com"
	localCfg.WorkloadJWKSURL = "https://ate-issuer.example.com/jwks"
	localCfg.WorkloadIssuerBaseURL = "https://auth.example.com"

	q := assistantrepo.New(db)
	require.NoError(t, q.SetAssistantDesiredRuntimeVersionForTest(t.Context(), assistantrepo.SetAssistantDesiredRuntimeVersionForTestParams{
		RuntimeVersion: 3,
		AssistantID:    runtimeSeed.AssistantID,
		ProjectID:      runtimeSeed.ProjectID,
	}))

	threadIdle := insertATEThreadFixture(t, db, runtimeSeed.ProjectID, runtimeSeed.AssistantID, "corr-sweep-idle")
	threadWarm := insertATEThreadFixture(t, db, runtimeSeed.ProjectID, runtimeSeed.AssistantID, "corr-sweep-warm")

	runtimeID := insertActiveV2RuntimeRow(t, db, runtimeSeed.ProjectID, runtimeSeed.AssistantID, uuid.Nil, runtimeBackendATE, runtimeStateActive, `{}`)
	require.NoError(t, q.SetKitRuntimeMetadataFixture(t.Context(), assistantrepo.SetKitRuntimeMetadataFixtureParams{
		RuntimeVersion: 3,
		Metadata:       []byte(`{}`),
		ProjectID:      runtimeSeed.ProjectID,
		RuntimeID:      runtimeID,
	}))

	cluster, client := startFakeATEStack(t, atespace)
	compCfg := KitATECompositionConfig{
		KitLocalCompositionConfig: localCfg,
		Enabled:                   true,
		Atespace:                  atespace,
		Template:                  template,
		WorkerSelector:            testKitATEWorkerSelector,
		EgressHosts:               []string{"ai.speakeasy.com"},
	}

	now := time.Now().UTC().Truncate(time.Second)
	currentTime := now
	backend, err := NewKitATERuntimeBackend(KitATERuntimeConfig{
		Image:              testKitATEImage,
		Atespace:           atespace,
		Template:           template,
		WorkerSelector:     testKitATEWorkerSelector,
		ServerURL:          core.serverURL,
		Client:             client,
		EgressHosts:        []string{"ai.speakeasy.com"},
		Bootstrap:          core.kitATECompositionBootstrap(compCfg),
		Token:              core.kitATEToken,
		Lifecycle:          core.kitATELifecycle,
		ThreadLifecycle:    core.kitATEThreadLifecycle,
		Store:              core.kitATEActorStore(),
		Revoke:             core.kitATERevoke,
		CheckSettled:       core.kitATECheckSettled,
		CheckThreadSettled: core.kitATECheckThreadSettled,
		ClearThreadSession: core.kitATEClearThreadSession,
		Now:                func() time.Time { return currentTime },
		Protocol: func(_ context.Context, _ assistantRuntimeRecord, _ uuid.UUID, _ string, endpoint *url.URL, hc *guardian.HTTPClient, token string) (runtimeProtocol, error) {
			return &fakeATEProtocol{client: hc, endpoint: endpoint, token: token}, nil
		},
		HelperPollInterval: 5 * time.Millisecond,
	})
	require.NoError(t, err)
	core.runtime = backend

	record := assistantRuntimeRecord{
		ID:                  runtimeID,
		AssistantID:         runtimeSeed.AssistantID,
		ProjectID:           runtimeSeed.ProjectID,
		RuntimeVersion:      3,
		Backend:             runtimeBackendATE,
		BackendMetadataJSON: []byte("{}"),
	}
	ensureRes, err := backend.Ensure(t.Context(), record)
	require.NoError(t, err)
	require.NoError(t, q.UpdateAssistantRuntimeMetadata(t.Context(), assistantrepo.UpdateAssistantRuntimeMetadataParams{
		BackendMetadataJson: ensureRes.BackendMetadataJSON,
		RuntimeID:           runtimeID,
		ProjectID:           runtimeSeed.ProjectID,
	}))
	record.BackendMetadataJSON = ensureRes.BackendMetadataJSON

	// Start threadIdle in the past so its warm window has elapsed past grace,
	// and start threadWarm right now so it remains warm.
	currentTime = now.Add(-(ateActorWarmWindow + runtimeIdleSweepGrace + time.Minute))
	require.NoError(t, backend.RunTurn(t.Context(), record, runTurnRequest{
		ThreadID:        threadIdle,
		Model:           "openai/gpt-4o-mini",
		ReasoningEffort: "medium",
	}))
	currentTime = now
	require.NoError(t, backend.RunTurn(t.Context(), record, runTurnRequest{
		ThreadID:        threadWarm,
		Model:           "openai/gpt-4o-mini",
		ReasoningEffort: "medium",
	}))

	// Sweep suspends only the idle thread actor, leaving the warm sibling running.
	stopped := core.sweepIdleKitRuntimes(t.Context(), now)
	require.EqualValues(t, 1, stopped)

	idleRow, err := q.GetAssistantATEActorForThread(t.Context(), assistantrepo.GetAssistantATEActorForThreadParams{
		ProjectID: runtimeSeed.ProjectID,
		RuntimeID: runtimeID,
		ThreadID:  threadIdle,
	})
	require.NoError(t, err)
	require.Equal(t, ateActorStateSuspended, idleRow.State)
	require.False(t, idleRow.WarmUntil.Valid)

	warmRow, err := q.GetAssistantATEActorForThread(t.Context(), assistantrepo.GetAssistantATEActorForThreadParams{
		ProjectID: runtimeSeed.ProjectID,
		RuntimeID: runtimeID,
		ThreadID:  threadWarm,
	})
	require.NoError(t, err)
	require.Equal(t, ateActorStateRunning, warmRow.State)
	require.True(t, warmRow.WarmUntil.Valid)

	// Marking the warm thread as draining causes the next sweep to synchronously
	// suspend it even though its warm window has not elapsed.
	cluster.mu.Lock()
	cluster.actors[atespace+"/"+warmRow.ActorName].draining = true
	cluster.mu.Unlock()

	stopped = core.sweepIdleKitRuntimes(t.Context(), now)
	require.EqualValues(t, 1, stopped)
	require.Equal(t, int32(2), cluster.suspendCalls.Load(), "both idle and draining threads must invoke synchronous SuspendActor")

	warmRow, err = q.GetAssistantATEActorForThread(t.Context(), assistantrepo.GetAssistantATEActorForThreadParams{
		ProjectID: runtimeSeed.ProjectID,
		RuntimeID: runtimeID,
		ThreadID:  threadWarm,
	})
	require.NoError(t, err)
	require.Equal(t, ateActorStateSuspended, warmRow.State)

	cluster.mu.Lock()
	remoteActor := cluster.actors[atespace+"/"+warmRow.ActorName].actor
	cluster.mu.Unlock()
	require.Equal(t, ateapiv1.ActorState_ACTOR_STATE_SUSPENDED, remoteActor.GetStatus().GetState())
	require.True(t, hasCompletedDurableRootfsSnapshot(remoteActor, warmRow.ActorUid))
}
