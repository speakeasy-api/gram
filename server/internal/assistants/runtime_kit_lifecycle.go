package assistants

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
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
	return version == 3 && backend == runtimeBackendGKE
}

// sweepIdleKitRuntimes uses the existing durable reaper cadence, not legacy
// expiry's terminal row transition. Platform MCP parity: this is internal
// process management, not a new member operation or conversation migration.
func (s *ServiceCore) sweepIdleKitRuntimes(ctx context.Context, now time.Time) int64 {
	rows, err := assistantrepo.New(s.db).ListActiveAssistantRuntimes(ctx, runtimeStateActive)
	if err != nil {
		s.logger.WarnContext(ctx, "list idle Kit runtimes failed", attr.SlogError(err))
		return 0
	}
	var stopped int64
	attempted := 0
	for _, row := range rows {
		if !kitProductionRuntime(row.RuntimeVersion, row.Backend) || !row.WarmUntil.Valid || row.WarmUntil.Time.After(now) {
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
	err = s.kitGKELifecycle(ctx, assistantRuntimeRecord{ID: runtimeID, ProjectID: projectID, AssistantID: assistantID}, func(ctx context.Context, record assistantRuntimeRecord) error {
		q := assistantrepo.New(record.LifecycleTx)
		row, err := q.GetAssistantRuntime(ctx, assistantrepo.GetAssistantRuntimeParams{ID: runtimeID, ProjectID: projectID})
		if err != nil {
			return fmt.Errorf("reload Kit checkpoint authority: %w", err)
		}
		if row.AssistantID != assistantID || !kitProductionRuntime(row.RuntimeVersion, row.Backend) || row.State != runtimeStateActive || row.Deleted || row.Ended || !row.WarmUntil.Valid || row.WarmUntil.Time.After(now) {
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
