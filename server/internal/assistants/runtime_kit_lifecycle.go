package assistants

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
)

// kitIdleCheckpointBackend is an explicit opt-in to nonterminal idle stops.
// Implementations must fail closed unless settlement, fencing, and durable
// state preservation are verified. Ordinary Stop is not sufficient.
type kitIdleCheckpointBackend interface {
	CheckpointIdleRuntime(context.Context, assistantRuntimeRecord) error
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

func kitLocalIdleCapability(backend RuntimeBackend, r assistantRuntimeRecord) (kitIdleCheckpointBackend, bool) {
	if r.RuntimeVersion != 3 || r.Backend != runtimeBackendLocal {
		return nil, false
	}
	switch b := backend.(type) {
	case *telemetryRuntimeBackend:
		return kitLocalIdleCapability(b.inner, r)
	case *versionedRuntimeBackend:
		selected, err := b.route(r)
		if err != nil {
			return nil, false
		}
		return kitLocalIdleCapability(selected, r)
	default:
		capability, ok := backend.(kitIdleCheckpointBackend)
		return capability, ok
	}
}

func kitProductionRuntime(version int16, backend string) bool {
	return version == 3 && backend == runtimeBackendATE
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

func kitLocalBackend(backend RuntimeBackend) *KitLocalRuntimeBackend {
	switch b := backend.(type) {
	case *telemetryRuntimeBackend:
		return kitLocalBackend(b.inner)
	case *versionedRuntimeBackend:
		return kitLocalBackend(b.kit)
	case *KitLocalRuntimeBackend:
		return b
	default:
		return nil
	}
}

// sweepIdleKitRuntimes drives nonterminal idle stops for Kit runtimes:
//   - retires any leftover Kit GKE rows cleanly so they never error-loop,
//   - stops idle per-thread local Docker Kit containers (and full local runtimes past warm_until),
//   - suspends idle or draining ATE thread actors via assistant_ate_actors.
func (s *ServiceCore) sweepIdleKitRuntimes(ctx context.Context, now time.Time) int64 {
	var stopped int64
	rows, err := assistantrepo.New(s.db).ListActiveAssistantRuntimes(ctx, runtimeStateActive)
	if err != nil {
		s.logger.WarnContext(ctx, "list idle Kit runtimes failed", attr.SlogError(err))
	} else {
		local := kitLocalBackend(s.runtime)
		attempted := 0
		for _, row := range rows {
			if row.RuntimeVersion != 3 {
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
			if isRetiredKitBackend(rec) {
				if attempted >= runtimeIdleSweepBatch {
					break
				}
				attempted++
				rowCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
				if s.retireLeftoverKitRuntime(rowCtx, rec) {
					stopped++
				}
				cancel()
				continue
			}
			if row.Backend == runtimeBackendLocal {
				capability, ok := kitLocalIdleCapability(s.runtime, rec)
				if !ok {
					continue
				}
				warmExpired := row.WarmUntil.Valid && !row.WarmUntil.Time.After(now.Add(-runtimeIdleSweepGrace))
				if !warmExpired && (local == nil || !local.hasTrackedThreads(row.ID)) {
					continue
				}
				if attempted >= runtimeIdleSweepBatch {
					break
				}
				attempted++
				rowCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
				n, stopErr := s.sweepIdleLocalKitRuntime(rowCtx, capability, local, rec, now)
				cancel()
				if stopErr != nil {
					s.logger.WarnContext(ctx, "stop idle local Kit containers failed", attr.SlogAssistantRuntimeID(row.ID.String()), attr.SlogError(stopErr))
				} else {
					stopped += n
				}
			}
		}
	}
	stopped += s.sweepATEThreadActors(ctx, now)
	return stopped
}

func (s *ServiceCore) sweepIdleLocalKitRuntime(ctx context.Context, capability kitIdleCheckpointBackend, local *KitLocalRuntimeBackend, rec assistantRuntimeRecord, now time.Time) (int64, error) {
	tx, release, err := beginKitLifecycleTransaction(ctx, s.db, rec.AssistantID.String())
	if err != nil {
		return 0, fmt.Errorf("begin local Kit idle sweep: %w", err)
	}
	defer release()

	q := assistantrepo.New(tx)
	row, err := q.GetAssistantRuntime(ctx, assistantrepo.GetAssistantRuntimeParams{ID: rec.ID, ProjectID: rec.ProjectID})
	if err != nil {
		return 0, fmt.Errorf("reload local Kit runtime for idle sweep: %w", err)
	}
	if row.AssistantID != rec.AssistantID || row.RuntimeVersion != 3 || row.Backend != runtimeBackendLocal || row.State != runtimeStateActive || row.Deleted || row.Ended {
		return 0, nil
	}
	rec.BackendMetadataJSON = row.BackendMetadataJson
	rec.WarmUntil = row.WarmUntil
	rec.LifecycleTx = tx
	ctx = context.WithValue(ctx, runtimeSwitchQueriesKey{}, q)

	// If the runtime's overall warm window elapsed with no in-flight work,
	// stop all running thread containers and clear warm_until.
	if row.WarmUntil.Valid && !row.WarmUntil.Time.After(now.Add(-runtimeIdleSweepGrace)) {
		inFlight, err := q.CountInFlightAssistantThreadEvents(ctx, assistantrepo.CountInFlightAssistantThreadEventsParams{
			ProjectID:        rec.ProjectID,
			AssistantID:      rec.AssistantID,
			PendingStatus:    eventStatusPending,
			ProcessingStatus: eventStatusProcessing,
		})
		if err != nil {
			return 0, fmt.Errorf("check local Kit in-flight events: %w", err)
		}
		if inFlight == 0 {
			if err := capability.CheckpointIdleRuntime(ctx, rec); err != nil {
				return 0, fmt.Errorf("stop idle local Kit runtime: %w", err)
			}
			row.WarmUntil.Valid = false
			if err := q.SetAssistantRuntimeActive(ctx, assistantrepo.SetAssistantRuntimeActiveParams{
				ActiveState: runtimeStateActive,
				RuntimeID:   rec.ID,
				ProjectID:   rec.ProjectID,
				WarmUntil:   row.WarmUntil,
			}); err != nil {
				return 0, fmt.Errorf("clear local Kit warm window: %w", err)
			}
			if err := tx.Commit(ctx); err != nil {
				return 0, fmt.Errorf("commit local Kit idle stop: %w", err)
			}
			return 1, nil
		}
	}

	if local == nil {
		return 0, nil
	}

	// Otherwise, stop any individual thread containers that have gone idle
	// past kitLocalThreadIdleWindow while sibling threads remain active.
	stopped, err := local.StopIdleThreadContainers(ctx, rec, now, kitLocalThreadIdleWindow, func(ctx context.Context, locked assistantRuntimeRecord, threadID uuid.UUID) error {
		busy, err := assistantrepo.New(locked.LifecycleTx).ThreadHasUnsettledEvents(ctx, assistantrepo.ThreadHasUnsettledEventsParams{
			ProjectID:      locked.ProjectID,
			AssistantID:    locked.AssistantID,
			ThreadID:       threadID,
			ExcludeEventID: uuid.Nil,
		})
		if err != nil {
			return fmt.Errorf("check local Kit thread unsettled events: %w", err)
		}
		if busy {
			return fmt.Errorf("local Kit thread has unsettled events")
		}
		return nil
	})
	if err != nil {
		return stopped, err
	}
	if err := tx.Commit(ctx); err != nil {
		return stopped, fmt.Errorf("commit local Kit per-thread idle stop: %w", err)
	}
	return stopped, nil
}

// retireLeftoverKitRuntime cleanly logs, revokes workload admission for, and
// terminally retires a Kit runtime row referencing the removed GKE backend.
func (s *ServiceCore) retireLeftoverKitRuntime(ctx context.Context, record assistantRuntimeRecord) bool {
	s.logger.WarnContext(ctx, "retiring leftover Kit runtime referencing removed backend",
		attr.SlogAssistantID(record.AssistantID.String()),
		attr.SlogProjectID(record.ProjectID.String()),
		attr.SlogAssistantRuntimeID(record.ID.String()),
		attr.SlogAssistantRuntimeBackend(record.Backend),
	)
	if err := s.revokeKitWorkload(ctx, s.db, record); err != nil {
		s.logger.WarnContext(ctx, "revoke leftover Kit runtime workload failed",
			attr.SlogAssistantRuntimeID(record.ID.String()),
			attr.SlogError(err),
		)
	}
	if err := s.stopRuntimeRecord(ctx, record.ProjectID, record.ID, runtimeStateFailed); err != nil {
		s.logger.WarnContext(ctx, "mark leftover Kit runtime retired failed",
			attr.SlogAssistantRuntimeID(record.ID.String()),
			attr.SlogError(err),
		)
		return false
	}
	return true
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
// admission and selection throughout suspension/stop.
func (s *ServiceCore) checkpointIdleKitRuntime(ctx context.Context, projectID, assistantID, runtimeID uuid.UUID, now time.Time) (bool, error) {
	row, err := assistantrepo.New(s.db).GetAssistantRuntime(ctx, assistantrepo.GetAssistantRuntimeParams{ID: runtimeID, ProjectID: projectID})
	if err != nil {
		return false, fmt.Errorf("read Kit checkpoint eligibility: %w", err)
	}
	if row.AssistantID != assistantID || !kitProductionRuntime(row.RuntimeVersion, row.Backend) {
		return false, nil
	}
	stopped := false
	err = s.kitATELifecycle(ctx, assistantRuntimeRecord{LifecycleTx: nil, Retired: false, AssistantThreadID: uuid.Nil, RuntimeVersion: 0, Backend: "", BackendMetadataJSON: nil, State: "", WarmUntil: pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false}, ID: runtimeID, ProjectID: projectID, AssistantID: assistantID}, func(ctx context.Context, record assistantRuntimeRecord) error {
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
