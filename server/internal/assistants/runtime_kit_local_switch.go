package assistants

import (
	"context"
	"fmt"
)

var _ runtimeSwitchCoordinator = (*KitLocalRuntimeBackend)(nil)

// RuntimeReadyForAdmission checks the configured local image without creating
// volumes/containers or starting a guest. Constructor validation owns the image
// contract, HOME ownership, bootstrap, and native ACP callback contracts.
func (k *KitLocalRuntimeBackend) RuntimeReadyForAdmission(ctx context.Context) error {
	imageID, err := k.engine.ImageID(ctx, k.config.Image)
	if err != nil {
		return fmt.Errorf("%w: resolve local kit image: %w", errRuntimeSwitchUnavailable, err)
	}
	if imageID == "" {
		return fmt.Errorf("%w: local kit image has no immutable image ID", errRuntimeSwitchUnavailable)
	}
	return nil
}

// PrepareRuntimeSwitch verifies destination readiness and settles every running
// local thread container via native ACP CloseNativeThreadSession while the
// caller holds the shared assistant admission lock.
func (k *KitLocalRuntimeBackend) PrepareRuntimeSwitch(ctx context.Context, assistant assistantRecord, rows []assistantRuntimeRecord, destination int16) (runtimeSwitchReadiness, error) {
	result := runtimeSwitchReadiness{DestinationReady: false, Settled: false, Release: func() {}}
	if destination != 2 && destination != 3 {
		return result, errRuntimeSwitchUnavailable
	}
	if destination == 3 {
		if err := k.RuntimeReadyForAdmission(ctx); err != nil {
			return result, err
		}
		result.DestinationReady = true
	}
	if len(rows) == 0 {
		if !result.DestinationReady {
			return result, errRuntimeSwitchUnavailable
		}
		result.Settled = true
		return result, nil
	}
	for _, row := range rows {
		if row.AssistantID != assistant.ID || row.ProjectID != assistant.ProjectID {
			return result, fmt.Errorf("%w: kit switch row belongs to another assistant", errRuntimeSwitchUnavailable)
		}
		if row.State == runtimeStateFailed {
			return result, fmt.Errorf("%w: cannot certify settlement of failed kit local runtime", errRuntimeSwitchUnavailable)
		}
		if row.State != runtimeStateActive && row.State != runtimeStateStopped {
			return result, errRuntimeSwitchBusy
		}
		containers, err := k.ownedThreadContainers(ctx, row)
		if err != nil {
			return result, fmt.Errorf("%w: inspect kit switch source: %w", errRuntimeSwitchUnavailable, err)
		}
		for _, entry := range containers {
			if !entry.info.Running {
				continue
			}
			if err := k.closeRunningThreadSession(ctx, row, entry); err != nil {
				return result, fmt.Errorf("%w: settle kit switch thread: %w", errRuntimeSwitchBusy, err)
			}
		}
	}
	result.Settled = true
	return result, nil
}
