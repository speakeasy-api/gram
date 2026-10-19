package assistants

import (
	"context"
	"fmt"
	"slices"
	"sync"
)

var _ runtimeSwitchCoordinator = (*KitLocalRuntimeBackend)(nil)

// RuntimeReadyForAdmission checks the configured local image without creating
// volumes/containers or starting a guest. Constructor validation owns the image
// command, HOME ownership, bootstrap and authenticated ACP callback contracts.
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

// PrepareRuntimeSwitch is a source-side fence. The versioned wrapper checks the
// destination separately; this backend can certify destination readiness only
// for version 3. The caller must hold shared assistant admission until Release.
// A running guest must supply the complete authoritative coordinator capability:
// foreground idle status never proves Kit background work has settled.
func (k *KitLocalRuntimeBackend) PrepareRuntimeSwitch(ctx context.Context, assistant assistantRecord, rows []assistantRuntimeRecord, destination int16) (runtimeSwitchReadiness, error) {
	releases := make([]func(), 0, len(rows))
	result := runtimeSwitchReadiness{DestinationReady: false, Settled: false, Release: sync.OnceFunc(func() {
		for _, release := range slices.Backward(releases) {
			release()
		}
	})}
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
		if row.State != runtimeStateActive && row.State != runtimeStateStopped && row.State != runtimeStateFailed {
			return result, errRuntimeSwitchBusy
		}
		info, exists, err := k.ownedContainer(ctx, row)
		if err != nil {
			return result, fmt.Errorf("%w: inspect kit switch source: %w", errRuntimeSwitchUnavailable, err)
		}
		// DB stopped/failed is not evidence on its own: inspect the bound engine
		// resource. Files in an actually stopped/absent local guest cannot execute;
		// no host-side wake worker is owned by this backend. DB pending work is
		// independently checked by the caller under its shared admission lock.
		if row.State != runtimeStateActive && (!exists || !info.Running) {
			continue
		}
		if !exists || !info.Running {
			return result, errRuntimeSwitchUnavailable
		}
		protocol, err := k.protocol(ctx, row)
		if err != nil {
			return result, fmt.Errorf("%w: resolve kit switch protocol: %w", errRuntimeSwitchUnavailable, err)
		}
		coordinator, ok := protocol.(runtimeSwitchCoordinator)
		if !ok {
			return result, errRuntimeSwitchUnavailable
		}
		fence, err := coordinator.PrepareRuntimeSwitch(ctx, assistant, []assistantRuntimeRecord{row}, destination)
		if fence.Release != nil {
			releases = append(releases, fence.Release)
		}
		if err != nil {
			return result, fmt.Errorf("fence kit runtime authority: %w", err)
		}
		if fence.Release == nil {
			return result, errRuntimeSwitchUnavailable
		}
		if !fence.Settled {
			return result, errRuntimeSwitchBusy
		}
	}
	result.Settled = true
	return result, nil
}
