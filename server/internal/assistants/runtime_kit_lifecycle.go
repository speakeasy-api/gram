package assistants

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
)

// kitIdleCheckpointBackend is an explicit opt-in to nonterminal idle stops.
// Implementations must fail closed unless all-session settlement, fencing and
// durable checkpoint publication are verified. Ordinary Stop is not sufficient.
type kitIdleCheckpointBackend interface {
	CheckpointIdleRuntime(context.Context, assistantRuntimeRecord) error
}

func (k *KitGKERuntimeBackend) CheckpointIdleRuntime(ctx context.Context, r assistantRuntimeRecord) error {
	return k.Stop(ctx, r)
}

func kitIdleCheckpointCapability(backend RuntimeBackend, r assistantRuntimeRecord) (kitIdleCheckpointBackend, error) {
	if !kitProductionRuntime(r.RuntimeVersion, r.Backend) {
		return nil, fmt.Errorf("kit idle checkpoint requires production v3 authority")
	}
	switch b := backend.(type) {
	case *telemetryRuntimeBackend:
		return kitIdleCheckpointCapability(b.inner, r)
	case *versionedRuntimeBackend:
		selected, err := b.route(r)
		if err != nil {
			return nil, err
		}
		return kitIdleCheckpointCapability(selected, r)
	default:
		capability, ok := backend.(kitIdleCheckpointBackend)
		if !ok {
			return nil, fmt.Errorf("kit idle checkpoint capability unavailable")
		}
		return capability, nil
	}
}

func kitProductionRuntime(version int16, backend string) bool {
	return version == 3 && (backend == runtimeBackendGKE || backend == runtimeBackendATE)
}

func kitATEBackend(backend RuntimeBackend) *KitATERuntimeBackend {
	switch b := backend.(type) {
	case *telemetryRuntimeBackend:
		return kitATEBackend(b.inner)
	case *versionedRuntimeBackend:
		return kitATEBackend(b.kit)
	case *KitATERuntimeBackend:
		return b
	default:
		return nil
	}
}

// sweepIdleKitRuntimes uses the existing durable reaper cadence, not legacy
// expiry's terminal row transition. Platform MCP parity: this is internal
// process management, not a new member operation or conversation migration.
func (s *ServiceCore) sweepIdleKitRuntimes(ctx context.Context, now time.Time) int64 {
	var stopped int64
	rows, err := assistantrepo.New(s.db).ListActiveAssistantRuntimes(ctx, runtimeStateActive)
	if err != nil {
		s.logger.WarnContext(ctx, "list idle Kit runtimes failed", attr.SlogError(err))
	} else {
		attempted := 0
		for _, row := range rows {
			// ATE actors are swept per thread below via assistant_ate_actors so
			// one active thread does not block suspending idle sibling threads.
			if row.Backend == runtimeBackendATE || !kitProductionRuntime(row.RuntimeVersion, row.Backend) || !row.WarmUntil.Valid || row.WarmUntil.Time.After(now.Add(-runtimeIdleSweepGrace)) {
				continue
			}
			if attempted >= runtimeIdleSweepBatch {
				break
			}
			attempted++
			rowCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
			didStop, err := s.checkpointIdleKitRuntime(rowCtx, row.ProjectID, row.AssistantID, row.ID, now)
			cancel()
			if err != nil {
				s.logger.WarnContext(ctx, "checkpoint idle Kit runtime failed", attr.SlogAssistantRuntimeID(row.ID.String()), attr.SlogError(err))
			} else if didStop {
				stopped++
			}
		}
	}
	stopped += s.sweepATEThreadActors(ctx, now)
	stopped += s.retireAndFinishSupersededKitRuntimes(ctx, runtimeIdleSweepBatch)
	return stopped
}

// retireSupersededKitBackendRuntimes transitions idle Kit v3 runtimes parked on
// a backend different from the deployment's active Kit v3 target backend into
// switch_retiring (via RetireAssistantRuntimesForSwitch) so the existing switch
// retirement path tears them down without waiting for another turn. Busy
// assistants with unsettled events, wakes, or guest activity are skipped.
func (s *ServiceCore) retireSupersededKitBackendRuntimes(ctx context.Context, limit int32) ([]assistantRuntimeRecord, int) {
	if limit <= 0 {
		return nil, 0
	}
	targetBackend, err := runtimeBackendForVersion(s.runtime, 3)
	if err != nil {
		if errors.Is(err, ErrRuntimeDisabled) || strings.Contains(err.Error(), "is not configured") {
			return nil, 0
		}
		if s.logger != nil {
			s.logger.WarnContext(ctx, "select target Kit backend for superseded runtime sweep failed", attr.SlogError(err))
		}
		return nil, 1
	}
	if targetBackend == "" {
		if s.logger != nil {
			s.logger.WarnContext(ctx, "target Kit backend for superseded runtime sweep is empty")
		}
		return nil, 1
	}
	q := assistantrepo.New(s.db)
	maxRetired := int(limit)
	var (
		retired        []assistantRuntimeRecord
		errCnt         int
		afterProjectID = uuid.Nil
	)
	for len(retired) < maxRetired {
		projectIDs, err := q.ListProjectsWithSupersededKitRuntimes(ctx, assistantrepo.ListProjectsWithSupersededKitRuntimesParams{
			TargetBackend:  targetBackend,
			AfterProjectID: afterProjectID,
			LimitCount:     limit,
		})
		if err != nil {
			if s.logger != nil {
				s.logger.WarnContext(ctx, "list projects with superseded Kit runtimes failed", attr.SlogError(err))
			}
			return retired, errCnt + 1
		}
		if len(projectIDs) == 0 {
			break
		}
		for _, projectID := range projectIDs {
			afterProjectID = projectID
			afterRuntimeID := uuid.Nil
			for len(retired) < maxRetired {
				rows, err := q.ListSupersededKitRuntimesByProject(ctx, assistantrepo.ListSupersededKitRuntimesByProjectParams{
					ProjectID:     projectID,
					TargetBackend: targetBackend,
					AfterID:       afterRuntimeID,
					LimitCount:    limit,
				})
				if err != nil {
					errCnt++
					if s.logger != nil {
						s.logger.WarnContext(ctx, "list superseded Kit runtimes for project failed",
							attr.SlogProjectID(projectID.String()),
							attr.SlogError(err),
						)
					}
					break
				}
				if len(rows) == 0 {
					break
				}
				for _, row := range rows {
					afterRuntimeID = row.ID
					if len(retired) >= maxRetired {
						break
					}
					rec, didRetire, retireErr := s.markSupersededKitRuntimeForRetirement(ctx, row.ProjectID, row.AssistantID, row.ID, targetBackend)
					if retireErr != nil {
						errCnt++
						if s.logger != nil {
							s.logger.WarnContext(ctx, "mark superseded Kit runtime for retirement failed",
								attr.SlogAssistantID(row.AssistantID.String()),
								attr.SlogAssistantRuntimeID(row.ID.String()),
								attr.SlogAssistantRuntimeBackend(row.Backend),
								attr.SlogError(retireErr),
							)
						}
						continue
					}
					if didRetire {
						retired = append(retired, rec)
					}
				}
				if len(rows) < maxRetired {
					break
				}
			}
			if len(retired) >= maxRetired {
				break
			}
		}
		if len(projectIDs) < maxRetired {
			break
		}
	}
	return retired, errCnt
}

func (s *ServiceCore) markSupersededKitRuntimeForRetirement(ctx context.Context, projectID, assistantID, runtimeID uuid.UUID, targetBackend string) (assistantRuntimeRecord, bool, error) {
	var zero assistantRuntimeRecord
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return zero, false, fmt.Errorf("begin superseded Kit retirement tx: %w", err)
	}
	var releaseFence func()
	defer func() {
		_ = tx.Rollback(ctx)
		if releaseFence != nil {
			releaseFence()
		}
	}()

	q := assistantrepo.New(tx)
	if err := q.AcquireAssistantAdvisoryLock(ctx, assistantID.String()); err != nil {
		return zero, false, fmt.Errorf("lock superseded Kit retirement: %w", err)
	}
	row, err := q.GetAssistantRuntimeV2(ctx, assistantrepo.GetAssistantRuntimeV2Params{
		ProjectID:   projectID,
		AssistantID: assistantID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, false, nil
	}
	if err != nil {
		return zero, false, fmt.Errorf("reload superseded Kit runtime: %w", err)
	}
	if row.ID != runtimeID || row.RuntimeVersion != 3 || row.Backend == targetBackend {
		return zero, false, nil
	}
	if row.State != runtimeStateActive && row.State != runtimeStateStopped && row.State != runtimeStateFailed {
		return zero, false, nil
	}
	busy, err := q.AssistantHasUnsettledEvents(ctx, assistantrepo.AssistantHasUnsettledEventsParams{
		ProjectID:   projectID,
		AssistantID: assistantID,
	})
	if err != nil {
		return zero, false, fmt.Errorf("check unsettled events before superseded Kit retirement: %w", err)
	}
	if !busy.Valid || busy.Bool {
		return zero, false, nil
	}
	asstRow, err := q.GetAssistant(ctx, assistantrepo.GetAssistantParams{
		ProjectID:   projectID,
		AssistantID: assistantID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, false, nil
	}
	if err != nil {
		return zero, false, fmt.Errorf("load assistant before superseded Kit retirement: %w", err)
	}
	candidate := assistantRuntimeRecord{
		LifecycleTx:         tx,
		Retired:             false,
		ID:                  row.ID,
		AssistantThreadID:   row.AssistantThreadID,
		AssistantID:         row.AssistantID,
		ProjectID:           row.ProjectID,
		RuntimeVersion:      row.RuntimeVersion,
		Backend:             row.Backend,
		BackendMetadataJSON: row.BackendMetadataJson,
		State:               row.State,
		WarmUntil:           row.WarmUntil,
	}
	if coordinator, ok := s.runtime.(kitBackendSwitchCoordinator); ok {
		readiness, err := coordinator.PrepareKitBackendSwitch(
			context.WithValue(ctx, runtimeSwitchQueriesKey{}, q),
			assistantRecordFromGetRow(asstRow),
			candidate,
		)
		releaseFence = readiness.Release
		if err != nil {
			if errors.Is(err, errRuntimeSwitchBusy) {
				return zero, false, nil
			}
			return zero, false, fmt.Errorf("prepare superseded Kit backend switch: %w", err)
		}
		if !readiness.DestinationReady || !readiness.Settled || readiness.Release == nil {
			return zero, false, nil
		}
	}
	if err := q.RetireAssistantRuntimesForSwitch(ctx, assistantrepo.RetireAssistantRuntimesForSwitchParams{
		ProjectID:   projectID,
		AssistantID: assistantID,
	}); err != nil {
		return zero, false, fmt.Errorf("retire superseded Kit runtime: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return zero, false, fmt.Errorf("commit superseded Kit retirement: %w", err)
	}
	return assistantRuntimeRecord{
		LifecycleTx:         nil,
		Retired:             true,
		ID:                  row.ID,
		AssistantThreadID:   row.AssistantThreadID,
		AssistantID:         row.AssistantID,
		ProjectID:           row.ProjectID,
		RuntimeVersion:      row.RuntimeVersion,
		Backend:             row.Backend,
		BackendMetadataJSON: row.BackendMetadataJson,
		State:               "switch_retiring",
		WarmUntil:           row.WarmUntil,
	}, true, nil
}

func (s *ServiceCore) retireAndFinishSupersededKitRuntimes(ctx context.Context, limit int32) int64 {
	retired, _ := s.retireSupersededKitBackendRuntimes(ctx, limit)
	var completed int64
	for _, rec := range retired {
		rowCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		err := s.finishRuntimeSwitchRetirement(rowCtx, rec)
		cancel()
		if err != nil {
			if s.logger != nil {
				s.logger.WarnContext(ctx, "finish superseded Kit runtime retirement failed",
					attr.SlogAssistantID(rec.AssistantID.String()),
					attr.SlogAssistantRuntimeID(rec.ID.String()),
					attr.SlogAssistantRuntimeBackend(rec.Backend),
					attr.SlogError(err),
				)
			}
			continue
		}
		completed++
	}
	return completed
}

// sweepATEThreadActors paginates live running rows in assistant_ate_actors
// (joined to active ATE runtimes so retired rows cannot starve the batch) and
// synchronously suspends up to runtimeIdleSweepBatch actors per tick that are
// idle past their warm window or advertising a worker eviction drain (SIGTERM).
func (s *ServiceCore) sweepATEThreadActors(ctx context.Context, now time.Time) int64 {
	ate := kitATEBackend(s.runtime)
	if ate == nil {
		return 0
	}
	q := assistantrepo.New(s.db)
	var (
		stopped int64
		afterID = uuid.Nil
	)
	for stopped < runtimeIdleSweepBatch {
		actors, err := q.ListRunningAssistantATEActorsPage(ctx, assistantrepo.ListRunningAssistantATEActorsPageParams{
			RunningState: ateActorStateRunning,
			AfterID:      afterID,
			LimitCount:   runtimeIdleSweepBatch,
		})
		if err != nil {
			s.logger.WarnContext(ctx, "list running ATE thread actors failed", attr.SlogError(err))
			return stopped
		}
		if len(actors) == 0 {
			break
		}
		for _, actor := range actors {
			if stopped >= runtimeIdleSweepBatch {
				return stopped
			}
			row, err := q.GetAssistantRuntime(ctx, assistantrepo.GetAssistantRuntimeParams{
				ID:        actor.RuntimeID,
				ProjectID: actor.ProjectID,
			})
			if err != nil {
				s.logger.WarnContext(ctx, "read ATE actor runtime failed", attr.SlogAssistantRuntimeID(actor.RuntimeID.String()), attr.SlogError(err))
				continue
			}
			if row.AssistantID != actor.AssistantID || row.RuntimeVersion != 3 || row.Backend != runtimeBackendATE || row.Deleted || row.Ended {
				continue
			}
			rec := assistantRuntimeRecord{
				ID:                  row.ID,
				AssistantThreadID:   row.AssistantThreadID,
				AssistantID:         row.AssistantID,
				ProjectID:           row.ProjectID,
				RuntimeVersion:      row.RuntimeVersion,
				Backend:             row.Backend,
				BackendMetadataJSON: row.BackendMetadataJson,
				State:               row.State,
				WarmUntil:           row.WarmUntil,
				LifecycleTx:         nil,
				Retired:             false,
			}
			actorCtx, cancel := context.WithTimeout(ctx, ateDrainSuspendTimeout)
			didStop, err := ate.SuspendThreadActor(actorCtx, rec, actor.AssistantThreadID, now)
			cancel()
			if err != nil {
				s.logger.WarnContext(ctx, "suspend ATE thread actor failed", attr.SlogAssistantRuntimeID(actor.RuntimeID.String()), attr.SlogError(err))
			} else if didStop {
				stopped++
			}
		}
		if len(actors) < runtimeIdleSweepBatch {
			break
		}
		afterID = actors[len(actors)-1].ID
	}
	return stopped
}

// checkpointIdleKitRuntime holds the same assistant-wide transaction lock as
// admission and selection throughout checkpoint/stop. Stop's production Kit
// contract must fence the guest, positively verify ALL sessions settled, publish
// a durable checkpoint, then stop. Status.Configured or nil IdleSeconds alone
// never authorize destruction. Any unknown settlement/checkpoint failure leaves
// the canonical runtime row intact so retries cannot create a new session owner.
func (s *ServiceCore) checkpointIdleKitRuntime(ctx context.Context, projectID, assistantID, runtimeID uuid.UUID, now time.Time) (bool, error) {
	// Backend and runtime version are immutable. Skip non-production rows before
	// entering the strict GKE authority helper; eligible rows are reloaded under
	// its advisory fence before any checkpoint operation.
	row, err := assistantrepo.New(s.db).GetAssistantRuntime(ctx, assistantrepo.GetAssistantRuntimeParams{ID: runtimeID, ProjectID: projectID})
	if err != nil {
		return false, fmt.Errorf("read Kit checkpoint eligibility: %w", err)
	}
	if row.AssistantID != assistantID || !kitProductionRuntime(row.RuntimeVersion, row.Backend) {
		return false, nil
	}
	stopped := false
	lifecycle := s.kitGKELifecycle
	if row.Backend == runtimeBackendATE {
		lifecycle = s.kitATELifecycle
	}
	err = lifecycle(ctx, assistantRuntimeRecord{LifecycleTx: nil, Retired: false, AssistantThreadID: uuid.Nil, RuntimeVersion: 0, Backend: "", BackendMetadataJSON: nil, State: "", WarmUntil: pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false}, ID: runtimeID, ProjectID: projectID, AssistantID: assistantID}, func(ctx context.Context, record assistantRuntimeRecord) error {
		q := assistantrepo.New(record.LifecycleTx)
		row, err := q.GetAssistantRuntime(ctx, assistantrepo.GetAssistantRuntimeParams{ID: runtimeID, ProjectID: projectID})
		if err != nil {
			return fmt.Errorf("reload Kit checkpoint authority: %w", err)
		}
		if row.AssistantID != assistantID || !kitProductionRuntime(row.RuntimeVersion, row.Backend) || row.State != runtimeStateActive || row.Deleted || row.Ended || !row.WarmUntil.Valid || row.WarmUntil.Time.After(now.Add(-runtimeIdleSweepGrace)) {
			return nil
		}
		inFlight, err := q.CountInFlightAssistantThreadEvents(ctx, assistantrepo.CountInFlightAssistantThreadEventsParams{ProjectID: projectID, AssistantID: assistantID, PendingStatus: eventStatusPending, ProcessingStatus: eventStatusProcessing})
		if err != nil {
			return fmt.Errorf("check Kit in-flight events: %w", err)
		}
		if inFlight != 0 {
			return nil
		}

		capability, err := kitIdleCheckpointCapability(s.runtime, record)
		if err != nil {
			return err
		}
		if err := capability.CheckpointIdleRuntime(ctx, record); err != nil {
			return fmt.Errorf("checkpoint and stop Kit runtime: %w", err)
		}
		// Deliberately no StopAssistantRuntime: ended/deleted rows cannot be restored
		// as the same authority. Ensure restores this exact lineage on next admission.
		// A NULL warm window records a nonterminal checkpoint. Admission assigns a
		// fresh window after restoring this same runtime. Do not spend future sweep
		// batches repeatedly stopping an already-checkpointed process.
		row.WarmUntil.Valid = false
		if err := q.SetAssistantRuntimeActive(ctx, assistantrepo.SetAssistantRuntimeActiveParams{
			ActiveState: runtimeStateActive, RuntimeID: runtimeID, ProjectID: projectID, WarmUntil: row.WarmUntil,
		}); err != nil {
			return fmt.Errorf("record Kit idle checkpoint: %w", err)
		}
		stopped = true
		return nil
	})
	return stopped, err
}
