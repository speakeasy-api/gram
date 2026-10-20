package assistants

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"sync"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/google/uuid"
)

// versionedRuntimeBackend keeps infrastructure selection separate from guest
// protocol evolution. Runtime rows, never UI flags, select the implementation.
type versionedRuntimeBackend struct {
	legacy RuntimeBackend
	kit    RuntimeBackend
}

func (v *versionedRuntimeBackend) forVersion(version int16) (RuntimeBackend, error) {
	var backend RuntimeBackend
	switch version {
	case 1, 2:
		backend = v.legacy
	case 3:
		backend = v.kit
	default:
		return nil, fmt.Errorf("unsupported assistant runtime version %d", version)
	}
	if backend == nil {
		return nil, fmt.Errorf("assistant runtime version %d is not configured", version)
	}
	return backend, nil
}

func (v *versionedRuntimeBackend) route(r assistantRuntimeRecord) (RuntimeBackend, error) {
	b, err := v.forVersion(r.RuntimeVersion)
	if err != nil {
		return nil, err
	}
	if !b.SupportsBackend(r.Backend) {
		return nil, fmt.Errorf("assistant runtime version %d does not support backend %q", r.RuntimeVersion, r.Backend)
	}
	return b, nil
}

func (v *versionedRuntimeBackend) Backend() string { return v.legacy.Backend() }
func (v *versionedRuntimeBackend) SupportsBackend(b string) bool {
	return v.legacy.SupportsBackend(b) || (v.kit != nil && v.kit.SupportsBackend(b))
}
func (v *versionedRuntimeBackend) ServerURL() *url.URL { return v.legacy.ServerURL() }
func (v *versionedRuntimeBackend) ImageRef() string    { return v.legacy.ImageRef() }

func (v *versionedRuntimeBackend) BackendForVersion(version int16) (string, error) {
	b, err := v.forVersion(version)
	if err != nil {
		return "", err
	}
	return b.Backend(), nil
}
func (v *versionedRuntimeBackend) ProtocolForRuntime(r assistantRuntimeRecord) (runtimeProtocol, error) {
	return v.route(r)
}
func (v *versionedRuntimeBackend) Ensure(ctx context.Context, r assistantRuntimeRecord) (RuntimeBackendEnsureResult, error) {
	b, err := v.route(r)
	if err != nil {
		return RuntimeBackendEnsureResult{}, err
	}
	result, err := b.Ensure(ctx, r)
	if err != nil {
		return result, fmt.Errorf("ensure selected runtime: %w", err)
	}
	return result, nil
}
func (v *versionedRuntimeBackend) RecycleImage(ctx context.Context, r assistantRuntimeRecord) (RuntimeBackendRecycleResult, error) {
	b, err := v.route(r)
	if err != nil {
		return RuntimeBackendRecycleResult{}, err
	}
	result, err := b.RecycleImage(ctx, r)
	if err != nil {
		return result, fmt.Errorf("recycle selected runtime image: %w", err)
	}
	return result, nil
}
func (v *versionedRuntimeBackend) RunTurn(ctx context.Context, r assistantRuntimeRecord, turn runTurnRequest) error {
	b, err := v.route(r)
	if err != nil {
		return err
	}
	if err := b.RunTurn(ctx, r, turn); err != nil {
		return fmt.Errorf("run selected runtime turn: %w", err)
	}
	return nil
}
func (v *versionedRuntimeBackend) InterruptTurn(ctx context.Context, r assistantRuntimeRecord, thread uuid.UUID) (bool, error) {
	b, err := v.route(r)
	if err != nil {
		return false, err
	}
	result, err := b.InterruptTurn(ctx, r, thread)
	if err != nil {
		return result, fmt.Errorf("interrupt selected runtime turn: %w", err)
	}
	return result, nil
}
func (v *versionedRuntimeBackend) Status(ctx context.Context, r assistantRuntimeRecord) (RuntimeBackendStatus, error) {
	b, err := v.route(r)
	if err != nil {
		return RuntimeBackendStatus{}, err
	}
	result, err := b.Status(ctx, r)
	if err != nil {
		return result, fmt.Errorf("read selected runtime status: %w", err)
	}
	return result, nil
}
func (v *versionedRuntimeBackend) Stop(ctx context.Context, r assistantRuntimeRecord) error {
	b, err := v.route(r)
	if err != nil {
		return err
	}
	if err := b.Stop(ctx, r); err != nil {
		return fmt.Errorf("stop selected runtime: %w", err)
	}
	return nil
}
func (v *versionedRuntimeBackend) Reap(ctx context.Context, r assistantRuntimeRecord) error {
	b, err := v.route(r)
	if err != nil {
		return err
	}
	if err := b.Reap(ctx, r); err != nil {
		return fmt.Errorf("reap selected runtime: %w", err)
	}
	return nil
}
func (v *versionedRuntimeBackend) ReapStoppedMachine(ctx context.Context, r assistantRuntimeRecord) error {
	b, err := v.route(r)
	if err != nil {
		return err
	}
	if err := b.ReapStoppedMachine(ctx, r); err != nil {
		return fmt.Errorf("reap stopped selected runtime: %w", err)
	}
	return nil
}
func (v *versionedRuntimeBackend) RuntimeExists(ctx context.Context, r assistantRuntimeRecord) (bool, error) {
	b, err := v.route(r)
	if err != nil {
		return false, err
	}
	probe, ok := b.(interface {
		RuntimeExists(context.Context, assistantRuntimeRecord) (bool, error)
	})
	if !ok {
		return true, nil
	}
	result, err := probe.RuntimeExists(ctx, r)
	if err != nil {
		return result, fmt.Errorf("probe selected runtime: %w", err)
	}
	return result, nil
}

// installKitRuntime is called during service composition before serving work.
// Keeping installation local to the service avoids changing the legacy default.
func (s *ServiceCore) installKitRuntime(kit RuntimeBackend) error {
	if kit == nil {
		return fmt.Errorf("kit runtime backend is required")
	}
	if t, ok := s.runtime.(*telemetryRuntimeBackend); ok {
		t.inner = &versionedRuntimeBackend{legacy: t.inner, kit: kit}
	} else {
		s.runtime = &versionedRuntimeBackend{legacy: s.runtime, kit: kit}
	}
	return nil
}

func runtimeBackendForVersion(backend RuntimeBackend, version int16) (string, error) {
	if selector, ok := backend.(interface{ BackendForVersion(int16) (string, error) }); ok {
		result, err := selector.BackendForVersion(version)
		if err != nil {
			return result, fmt.Errorf("select runtime backend: %w", err)
		}
		return result, nil
	}
	if version == 1 || version == 2 {
		return backend.Backend(), nil
	}
	return "", fmt.Errorf("assistant runtime version %d admission is not configured", version)
}
func boundRuntimeProtocol(backend RuntimeBackend, r assistantRuntimeRecord) (runtimeProtocol, error) {
	if selector, ok := backend.(interface {
		ProtocolForRuntime(assistantRuntimeRecord) (runtimeProtocol, error)
	}); ok {
		result, err := selector.ProtocolForRuntime(r)
		if err != nil {
			return result, fmt.Errorf("select runtime protocol: %w", err)
		}
		return result, nil
	}
	return resolveRuntimeProtocol(r.RuntimeVersion, backend, nil)
}

func (t *telemetryRuntimeBackend) BackendForVersion(version int16) (string, error) {
	return runtimeBackendForVersion(t.inner, version)
}
func (t *telemetryRuntimeBackend) ProtocolForRuntime(r assistantRuntimeRecord) (runtimeProtocol, error) {
	if _, err := boundRuntimeProtocol(t.inner, r); err != nil {
		return nil, err
	}
	return t, nil
}
func (t *telemetryRuntimeBackend) PrepareRuntimeSwitch(ctx context.Context, assistant assistantRecord, rows []assistantRuntimeRecord, destination int16) (runtimeSwitchReadiness, error) {
	coordinator, ok := t.inner.(runtimeSwitchCoordinator)
	if !ok {
		return runtimeSwitchReadiness{}, errRuntimeSwitchUnavailable
	}
	result, err := coordinator.PrepareRuntimeSwitch(ctx, assistant, rows, destination)
	if err != nil {
		return result, fmt.Errorf("prepare runtime switch: %w", err)
	}
	return result, nil
}

func (v *versionedRuntimeBackend) PrepareRuntimeSwitch(ctx context.Context, assistant assistantRecord, rows []assistantRuntimeRecord, destination int16) (runtimeSwitchReadiness, error) {
	target, err := v.forVersion(destination)
	if err != nil {
		return runtimeSwitchReadiness{}, err
	}
	if ready, ok := target.(interface{ RuntimeReadyForAdmission(context.Context) error }); ok {
		if err := ready.RuntimeReadyForAdmission(ctx); err != nil {
			return runtimeSwitchReadiness{}, fmt.Errorf("check destination runtime readiness: %w", err)
		}
	} else if destination == 3 {
		return runtimeSwitchReadiness{}, errRuntimeSwitchUnavailable
	}
	releases := make([]func(), 0, len(rows))
	release := func() {
		for _, release := range slices.Backward(releases) {
			release()
		}
	}
	for _, row := range rows {
		source, err := v.route(row)
		if err != nil {
			release()
			return runtimeSwitchReadiness{}, err
		}
		if coordinator, ok := source.(runtimeSwitchCoordinator); ok {
			result, err := coordinator.PrepareRuntimeSwitch(ctx, assistant, []assistantRuntimeRecord{row}, destination)
			if result.Release != nil {
				releases = append(releases, result.Release)
			}
			if err != nil {
				release()
				return runtimeSwitchReadiness{}, fmt.Errorf("prepare source runtime switch: %w", err)
			}
			if !result.Settled || result.Release == nil {
				release()
				return runtimeSwitchReadiness{}, errRuntimeSwitchBusy
			}
			continue
		}
		if row.RuntimeVersion == 3 {
			release()
			return runtimeSwitchReadiness{}, errRuntimeSwitchUnavailable
		}
		// Legacy guests admit work only through the event pipeline; its DB lock is
		// held by the caller. Their idle clock includes turn-end persistence and
		// compaction. This reasoning does NOT apply to Kit background work.
		status, err := source.Status(ctx, row)
		if err != nil {
			release()
			return runtimeSwitchReadiness{}, fmt.Errorf("verify legacy runtime settlement: %w", err)
		}
		if !status.Configured || (status.IdleSeconds != nil && *status.IdleSeconds == 0) {
			release()
			return runtimeSwitchReadiness{}, errRuntimeSwitchBusy
		}
	}
	return runtimeSwitchReadiness{DestinationReady: true, Settled: true, Release: release}, nil
}

// RuntimeReadyForAdmission checks the production control plane without creating
// a guest or minting credentials. Constructor validation owns the image contract;
// authenticated ACP readiness is checked after provisioning by Ensure.
func (k *KitGKERuntimeBackend) RuntimeReadyForAdmission(ctx context.Context) error {
	if _, err := k.config.GKE.Dynamic.Resource(gkeSandboxClaimGVR).Namespace(k.config.GKE.Namespace).List(ctx, metav1.ListOptions{Limit: 1}); err != nil {
		return fmt.Errorf("%w: inspect Kit GKE admission API: %w", errRuntimeSwitchUnavailable, err)
	}
	return nil
}

var _ runtimeSwitchCoordinator = (*KitGKERuntimeBackend)(nil)

// PrepareRuntimeSwitch retains every guest fence until the caller commits the
// selection. A missing or unverified production process is never proof of idle.
func (k *KitGKERuntimeBackend) PrepareRuntimeSwitch(ctx context.Context, assistant assistantRecord, rows []assistantRuntimeRecord, destination int16) (runtimeSwitchReadiness, error) {
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
	for _, row := range rows {
		if row.AssistantID != assistant.ID || row.ProjectID != assistant.ProjectID {
			return result, ErrKitRuntimeBinding
		}
		if err := k.validate(row); err != nil {
			return result, err
		}
		if row.State != runtimeStateActive {
			if err := k.verifyStoppedSwitchSource(ctx, row); err != nil {
				return result, err
			}
			continue
		}
		protocol, err := k.protocol(ctx, row)
		if err != nil {
			return result, fmt.Errorf("%w: inspect Kit GKE switch source: %w", errRuntimeSwitchUnavailable, err)
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
			return result, fmt.Errorf("prepare runtime switch: %w", err)
		}
		if !fence.Settled || fence.Release == nil {
			return result, errRuntimeSwitchBusy
		}
	}
	result.Settled = true
	return result, nil
}
