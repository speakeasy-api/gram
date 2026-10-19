package assistants

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
)

var (
	errRuntimeSwitchBusy        = errors.New("assistant has pending or active work; retry after all work settles")
	errRuntimeSwitchConflict    = errors.New("assistant runtime selection changed; reload before switching")
	errRuntimeSwitchUnavailable = errors.New("runtime switching is unavailable until the destination and complete activity state can be verified")
)

// runtimeSelectionChange is independent of frontend rollout flags. Supplying
// either field requires both, including on a retry of a previously applied write.
type runtimeSelectionChange struct {
	Version         *int
	ExpectedVersion *int
}

// runtimeSwitchCoordinator is an internal adapter contract, not a guest wire
// extension. PrepareRuntimeSwitch must fence new guest work and positively
// verify all foreground/background jobs, queued admissions, persisted sessions
// and pending wakeups are settled, and the destination is configured. A
// foreground idle timestamp alone is insufficient. Unknown state is not settled.
// The fence must remain effective until Release is called after the database
// transaction ends. No backend may report readiness merely because its process
// is reachable. The adapter owns discovery and interpretation of guest activity.
type runtimeSwitchCoordinator interface {
	PrepareRuntimeSwitch(context.Context, assistantRecord, []assistantRuntimeRecord, int16) (runtimeSwitchReadiness, error)
}

type runtimeSwitchReadiness struct {
	DestinationReady bool
	Settled          bool
	// Release must be non-nil, even for an authoritative no-resource result.
	// It releases the guest admission fence, not the database advisory lock.
	Release func()
}

func validateRuntimeSelection(change runtimeSelectionChange) error {
	if change.Version == nil && change.ExpectedVersion == nil {
		return nil
	}
	if change.Version == nil || change.ExpectedVersion == nil {
		return fmt.Errorf("%w: runtime_version and expected_runtime_version must be supplied together", errAssistantValidation)
	}
	if (*change.Version != 2 && *change.Version != 3) || (*change.ExpectedVersion != 2 && *change.ExpectedVersion != 3) {
		return fmt.Errorf("%w: runtime versions must be 2 or 3", errAssistantValidation)
	}
	return nil
}

// prepareRuntimeSelection runs only under AcquireAssistantAdvisoryLock. Its
// caller must end the transaction before invoking the returned release callback.
// Runtime rows retain their original version even after physical retirement.
func (s *ServiceCore) prepareRuntimeSelection(ctx context.Context, queries *assistantrepo.Queries, projectID, assistantID uuid.UUID, change runtimeSelectionChange) (func(), error) {
	if err := validateRuntimeSelection(change); err != nil {
		return nil, err
	}
	if change.Version == nil {
		return nil, nil
	}
	// Validation above restricts both inputs to these explicit wire values.
	desiredVersion := int16(2)
	if *change.Version == 3 {
		desiredVersion = 3
	}
	expectedVersion := int16(2)
	if *change.ExpectedVersion == 3 {
		expectedVersion = 3
	}
	current, err := queries.GetAssistant(ctx, assistantrepo.GetAssistantParams{ProjectID: projectID, AssistantID: assistantID})
	if err != nil {
		return nil, fmt.Errorf("read runtime selection: %w", err)
	}
	if int(current.RuntimeVersion) != *change.ExpectedVersion {
		return nil, errRuntimeSwitchConflict
	}
	if int(current.RuntimeVersion) == *change.Version {
		return nil, nil
	}
	busy, err := queries.AssistantHasUnsettledEvents(ctx, assistantrepo.AssistantHasUnsettledEventsParams{ProjectID: projectID, AssistantID: assistantID})
	if err != nil {
		return nil, fmt.Errorf("check pending assistant work: %w", err)
	}
	if !busy.Valid || busy.Bool {
		return nil, errRuntimeSwitchBusy
	}
	rows, err := queries.ListAssistantRuntimesForSwitch(ctx, assistantrepo.ListAssistantRuntimesForSwitchParams{ProjectID: projectID, AssistantID: assistantID})
	if err != nil {
		return nil, fmt.Errorf("load runtimes for switching: %w", err)
	}
	runtimes := make([]assistantRuntimeRecord, 0, len(rows))
	for _, row := range rows {
		// A starting runtime may already be admitted but not yet accepting work.
		// Expiring or unknown lifecycle states are not safe to switch either.
		if row.State != runtimeStateActive && row.State != runtimeStateStopped && row.State != runtimeStateFailed {
			return nil, errRuntimeSwitchBusy
		}
		runtimes = append(runtimes, assistantRuntimeRecord{
			ID: row.ID, AssistantThreadID: row.AssistantThreadID, AssistantID: row.AssistantID,
			ProjectID: row.ProjectID, RuntimeVersion: row.RuntimeVersion, Backend: row.Backend,
			BackendMetadataJSON: row.BackendMetadataJson, State: row.State, WarmUntil: row.WarmUntil,
		})
	}
	coordinator, ok := s.runtime.(runtimeSwitchCoordinator)
	if !ok {
		return nil, errRuntimeSwitchUnavailable
	}
	readiness, err := coordinator.PrepareRuntimeSwitch(ctx, assistantRecordFromGetRow(current), runtimes, desiredVersion)
	if err != nil {
		return readiness.Release, fmt.Errorf("prepare runtime switch: %w", err)
	}
	if readiness.Release == nil || !readiness.DestinationReady {
		return readiness.Release, errRuntimeSwitchUnavailable
	}
	if !readiness.Settled {
		return readiness.Release, errRuntimeSwitchBusy
	}
	for _, runtime := range runtimes {
		if err := s.runtime.Stop(ctx, runtime); err != nil {
			return readiness.Release, fmt.Errorf("retire previous runtime: %w", err)
		}
	}
	if err := queries.RetireAssistantRuntimesForSwitch(ctx, assistantrepo.RetireAssistantRuntimesForSwitchParams{ProjectID: projectID, AssistantID: assistantID}); err != nil {
		return readiness.Release, fmt.Errorf("retire runtime bindings: %w", err)
	}
	updated, err := queries.SetAssistantDesiredRuntimeVersion(ctx, assistantrepo.SetAssistantDesiredRuntimeVersionParams{
		ProjectID: projectID, AssistantID: assistantID, RuntimeVersion: desiredVersion, ExpectedRuntimeVersion: expectedVersion,
	})
	if err != nil {
		return readiness.Release, fmt.Errorf("persist runtime selection: %w", err)
	}
	if updated != 1 {
		return readiness.Release, errRuntimeSwitchConflict
	}
	return readiness.Release, nil
}
