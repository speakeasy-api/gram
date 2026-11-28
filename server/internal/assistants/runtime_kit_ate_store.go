package assistants

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
)

const (
	// ateActorStateRunning indicates the thread's ATE actor is active on a worker.
	ateActorStateRunning = "running"

	// ateActorStateSuspended indicates the thread's ATE actor is durably suspended.
	ateActorStateSuspended = "suspended"

	// ateActorStateCrashed indicates the thread's ATE actor entered ACTOR_STATE_CRASHED.
	ateActorStateCrashed = "crashed"

	// ateActorStateRetired indicates the thread's ATE actor row was soft-deleted after reap or replacement.
	ateActorStateRetired = "retired"
)

type kitATEPhasePublisherKey struct{}

type kitATEPhasePublisher func(context.Context) error

// kitATEThreadActor represents one durable row in assistant_ate_actors.
type kitATEThreadActor struct {
	ID         uuid.UUID
	RuntimeID  uuid.UUID
	ThreadID   uuid.UUID
	ActorName  string
	ActorUID   string
	State      string
	Configured bool
	Workload   *KitWorkloadBinding
	WarmUntil  *time.Time
}

type kitATEActorStore interface {
	GetForThread(ctx context.Context, r assistantRuntimeRecord, threadID uuid.UUID) (kitATEThreadActor, bool, error)
	Insert(ctx context.Context, r assistantRuntimeRecord, actor kitATEThreadActor) (kitATEThreadActor, error)
	UpdateBootstrap(ctx context.Context, r assistantRuntimeRecord, actor kitATEThreadActor) (kitATEThreadActor, error)
	UpdateState(ctx context.Context, r assistantRuntimeRecord, actorID uuid.UUID, state string, warmUntil *time.Time) (kitATEThreadActor, error)
	SoftDelete(ctx context.Context, r assistantRuntimeRecord, actorID uuid.UUID) error
	ListForRuntime(ctx context.Context, r assistantRuntimeRecord) ([]kitATEThreadActor, error)
	CountForRuntime(ctx context.Context, r assistantRuntimeRecord) (int64, error)
}

type pgKitATEActorStore struct {
	core *ServiceCore
}

func (s *ServiceCore) kitATEActorStore() kitATEActorStore {
	return &pgKitATEActorStore{core: s}
}

func (p *pgKitATEActorStore) GetForThread(ctx context.Context, r assistantRuntimeRecord, threadID uuid.UUID) (kitATEThreadActor, bool, error) {
	var zero kitATEThreadActor
	q := assistantrepo.New(p.core.kitLifecycleDB(r))
	var (
		row assistantrepo.AssistantAteActor
		err error
	)
	if r.LifecycleTx != nil {
		row, err = q.LockAssistantATEActorForThread(ctx, assistantrepo.LockAssistantATEActorForThreadParams{
			ProjectID: r.ProjectID,
			RuntimeID: r.ID,
			ThreadID:  threadID,
		})
	} else {
		row, err = q.GetAssistantATEActorForThread(ctx, assistantrepo.GetAssistantATEActorForThreadParams{
			ProjectID: r.ProjectID,
			RuntimeID: r.ID,
			ThreadID:  threadID,
		})
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, false, nil
	}
	if err != nil {
		return zero, false, fmt.Errorf("load ATE thread actor: %w", err)
	}
	actor, err := kitATEThreadActorFromRow(row)
	if err != nil {
		return zero, false, err
	}
	return actor, true, nil
}

func (p *pgKitATEActorStore) Insert(ctx context.Context, r assistantRuntimeRecord, actor kitATEThreadActor) (kitATEThreadActor, error) {
	var zero kitATEThreadActor
	if r.LifecycleTx == nil {
		return zero, errors.New("ATE actor creation requires a thread lifecycle transaction")
	}
	var workloadRaw []byte
	if actor.Workload != nil {
		var err error
		workloadRaw, err = json.Marshal(actor.Workload)
		if err != nil {
			return zero, fmt.Errorf("encode ATE thread workload binding: %w", err)
		}
	}
	row, err := assistantrepo.New(r.LifecycleTx).InsertAssistantATEActor(ctx, assistantrepo.InsertAssistantATEActorParams{
		ActorName:           actor.ActorName,
		ActorUid:            actor.ActorUID,
		State:               actor.State,
		Configured:          actor.Configured,
		WorkloadBindingJson: workloadRaw,
		WarmUntil:           ateWarmUntilTimestamptz(actor.WarmUntil),
		RuntimeID:           r.ID,
		ProjectID:           r.ProjectID,
		AssistantID:         r.AssistantID,
		ThreadID:            actor.ThreadID,
	})
	if err != nil {
		return zero, fmt.Errorf("insert ATE thread actor: %w", err)
	}
	if err := publishKitATEPhase(ctx); err != nil {
		return zero, err
	}
	return kitATEThreadActorFromRow(row)
}

func (p *pgKitATEActorStore) UpdateBootstrap(ctx context.Context, r assistantRuntimeRecord, actor kitATEThreadActor) (kitATEThreadActor, error) {
	var zero kitATEThreadActor
	if r.LifecycleTx == nil {
		return zero, errors.New("ATE actor bootstrap update requires a thread lifecycle transaction")
	}
	var workloadRaw []byte
	if actor.Workload != nil {
		var err error
		workloadRaw, err = json.Marshal(actor.Workload)
		if err != nil {
			return zero, fmt.Errorf("encode ATE thread workload binding: %w", err)
		}
	}
	row, err := assistantrepo.New(r.LifecycleTx).UpdateAssistantATEActorBootstrap(ctx, assistantrepo.UpdateAssistantATEActorBootstrapParams{
		WorkloadBindingJson: workloadRaw,
		Configured:          actor.Configured,
		State:               actor.State,
		WarmUntil:           ateWarmUntilTimestamptz(actor.WarmUntil),
		ProjectID:           r.ProjectID,
		ID:                  actor.ID,
	})
	if err != nil {
		return zero, fmt.Errorf("update ATE thread actor bootstrap: %w", err)
	}
	if err := publishKitATEPhase(ctx); err != nil {
		return zero, err
	}
	return kitATEThreadActorFromRow(row)
}

func (p *pgKitATEActorStore) UpdateState(ctx context.Context, r assistantRuntimeRecord, actorID uuid.UUID, state string, warmUntil *time.Time) (kitATEThreadActor, error) {
	var zero kitATEThreadActor
	if r.LifecycleTx == nil {
		return zero, errors.New("ATE actor state update requires a lifecycle transaction")
	}
	row, err := assistantrepo.New(r.LifecycleTx).UpdateAssistantATEActorState(ctx, assistantrepo.UpdateAssistantATEActorStateParams{
		State:     state,
		WarmUntil: ateWarmUntilTimestamptz(warmUntil),
		ProjectID: r.ProjectID,
		ID:        actorID,
	})
	if err != nil {
		return zero, fmt.Errorf("update ATE thread actor state: %w", err)
	}
	if err := publishKitATEPhase(ctx); err != nil {
		return zero, err
	}
	return kitATEThreadActorFromRow(row)
}

func (p *pgKitATEActorStore) SoftDelete(ctx context.Context, r assistantRuntimeRecord, actorID uuid.UUID) error {
	if r.LifecycleTx == nil {
		return errors.New("ATE actor retirement requires a lifecycle transaction")
	}
	if _, err := assistantrepo.New(r.LifecycleTx).SoftDeleteAssistantATEActor(ctx, assistantrepo.SoftDeleteAssistantATEActorParams{
		RetiredState: ateActorStateRetired,
		ProjectID:    r.ProjectID,
		ID:           actorID,
	}); err != nil {
		return fmt.Errorf("soft-delete ATE thread actor: %w", err)
	}
	return publishKitATEPhase(ctx)
}

func (p *pgKitATEActorStore) ListForRuntime(ctx context.Context, r assistantRuntimeRecord) ([]kitATEThreadActor, error) {
	rows, err := assistantrepo.New(p.core.kitLifecycleDB(r)).ListAssistantATEActorsForRuntime(ctx, assistantrepo.ListAssistantATEActorsForRuntimeParams{
		ProjectID: r.ProjectID,
		RuntimeID: r.ID,
	})
	if err != nil {
		return nil, fmt.Errorf("list ATE thread actors: %w", err)
	}
	actors := make([]kitATEThreadActor, 0, len(rows))
	for _, row := range rows {
		actor, err := kitATEThreadActorFromRow(row)
		if err != nil {
			return nil, err
		}
		actors = append(actors, actor)
	}
	return actors, nil
}

func (p *pgKitATEActorStore) CountForRuntime(ctx context.Context, r assistantRuntimeRecord) (int64, error) {
	count, err := assistantrepo.New(p.core.kitLifecycleDB(r)).CountAssistantATEActorsForRuntime(ctx, assistantrepo.CountAssistantATEActorsForRuntimeParams{
		ProjectID: r.ProjectID,
		RuntimeID: r.ID,
	})
	if err != nil {
		return 0, fmt.Errorf("count ATE thread actors: %w", err)
	}
	return count, nil
}

func kitATEThreadActorFromRow(row assistantrepo.AssistantAteActor) (kitATEThreadActor, error) {
	var zero kitATEThreadActor
	var workload *KitWorkloadBinding
	if len(row.WorkloadBindingJson) > 0 {
		var parsed KitWorkloadBinding
		if err := json.Unmarshal(row.WorkloadBindingJson, &parsed); err != nil {
			return zero, fmt.Errorf("decode ATE thread workload binding: %w", err)
		}
		workload = &parsed
	}
	var warmUntil *time.Time
	if row.WarmUntil.Valid {
		t := row.WarmUntil.Time
		warmUntil = &t
	}
	return kitATEThreadActor{
		ID:         row.ID,
		RuntimeID:  row.RuntimeID,
		ThreadID:   row.AssistantThreadID,
		ActorName:  row.ActorName,
		ActorUID:   row.ActorUid,
		State:      row.State,
		Configured: row.Configured,
		Workload:   workload,
		WarmUntil:  warmUntil,
	}, nil
}

func ateWarmUntilTimestamptz(t *time.Time) pgtype.Timestamptz {
	if t == nil {
		return pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false}
	}
	return pgtype.Timestamptz{Time: *t, InfinityModifier: pgtype.Finite, Valid: true}
}

func publishKitATEPhase(ctx context.Context) error {
	publish, ok := ctx.Value(kitATEPhasePublisherKey{}).(kitATEPhasePublisher)
	if !ok {
		return nil
	}
	return publish(ctx)
}

// kitATEThreadLifecycleTransaction pins a dedicated connection with a
// per-thread session advisory lock so sibling threads of the same assistant
// never serialize on each other or lock the shared assistant_runtimes row.
type kitATEThreadLifecycleTransaction struct {
	pgx.Tx
	conn *pgxpool.Conn
}

func (t *kitATEThreadLifecycleTransaction) Publish(ctx context.Context) error {
	if err := t.Commit(ctx); err != nil {
		return fmt.Errorf("publish ATE thread lifecycle phase: %w", err)
	}
	next, err := t.conn.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:       pgx.ReadCommitted,
		AccessMode:     pgx.ReadWrite,
		DeferrableMode: pgx.NotDeferrable,
		BeginQuery:     "",
		CommitQuery:    "",
	})
	if err != nil {
		return fmt.Errorf("resume ATE thread lifecycle transaction: %w", err)
	}
	t.Tx = next
	return nil
}

func beginKitATEThreadLifecycleTransaction(ctx context.Context, pool *pgxpool.Pool, assistantID, runtimeID, threadID uuid.UUID) (*kitATEThreadLifecycleTransaction, func(), error) {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("acquire ATE thread lifecycle connection: %w", err)
	}
	var (
		lifecycle       *kitATEThreadLifecycleTransaction
		heldSharedLock  bool
		heldThreadLock  bool
		assistantLockID = assistantID.String()
	)
	cleanup := func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		if lifecycle != nil {
			_ = lifecycle.Rollback(cleanupCtx)
		}
		q := assistantrepo.New(conn)
		unlockFailed := false
		if heldThreadLock {
			if _, err := q.ReleaseATEThreadSessionLock(cleanupCtx, assistantrepo.ReleaseATEThreadSessionLockParams{
				RuntimeID: runtimeID.String(),
				ThreadID:  threadID.String(),
			}); err != nil {
				unlockFailed = true
			}
		}
		if heldSharedLock {
			if _, err := q.ReleaseAssistantSharedSessionLock(cleanupCtx, assistantLockID); err != nil {
				unlockFailed = true
			}
		}
		if unlockFailed {
			raw := conn.Hijack()
			_ = raw.Close(cleanupCtx)
			return
		}
		conn.Release()
	}
	q := assistantrepo.New(conn)
	// Shared assistant lock allows sibling threads to proceed concurrently while
	// still excluding runtime-wide Stop/Reap/switch (which take the exclusive lock).
	if err = q.AcquireAssistantSharedSessionLock(ctx, assistantLockID); err != nil {
		heldSharedLock = true
		cleanup()
		return nil, nil, fmt.Errorf("acquire shared ATE runtime lock: %w", err)
	}
	heldSharedLock = true
	if err = q.AcquireATEThreadSessionLock(ctx, assistantrepo.AcquireATEThreadSessionLockParams{
		RuntimeID: runtimeID.String(),
		ThreadID:  threadID.String(),
	}); err != nil {
		heldThreadLock = true
		cleanup()
		return nil, nil, fmt.Errorf("lock ATE thread lifecycle: %w", err)
	}
	heldThreadLock = true
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:       pgx.ReadCommitted,
		AccessMode:     pgx.ReadWrite,
		DeferrableMode: pgx.NotDeferrable,
		BeginQuery:     "",
		CommitQuery:    "",
	})
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("begin ATE thread lifecycle transaction: %w", err)
	}
	lifecycle = &kitATEThreadLifecycleTransaction{Tx: tx, conn: conn}
	return lifecycle, cleanup, nil
}
