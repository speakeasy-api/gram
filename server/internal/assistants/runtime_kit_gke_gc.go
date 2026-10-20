package assistants

import (
	"context"
	"fmt"
)

// gcSupersededCheckpoints runs only after the replacement authority is durable.
// Keep references until both exact-UID deletions succeed, so retries can finish
// partial cleanup without enumerating or trusting labels on cluster objects.
func (k *KitGKERuntimeBackend) gcSupersededCheckpoints(ctx context.Context, state kitGKEAuthority, handle *kitGKEAuthorityHandle) (kitGKEAuthority, *kitGKEAuthorityHandle, error) {
	if len(state.PriorCheckpoints) == 0 {
		return state, handle, nil
	}
	switch state.Phase {
	case "checkpointed", "stopped", "restoring", "active":
	default:
		return state, handle, fmt.Errorf("Kit GKE superseded checkpoint cleanup requires durable replacement")
	}
	if state.SnapshotName == "" || state.SnapshotUID == "" {
		return state, handle, fmt.Errorf("Kit GKE superseded checkpoint cleanup requires replacement snapshot")
	}
	for _, ref := range state.PriorCheckpoints {
		if (ref.SnapshotName != "" && ref.SnapshotName == state.SnapshotName) || (ref.TriggerName != "" && ref.TriggerName == state.TriggerName) {
			return state, handle, fmt.Errorf("Kit GKE prior checkpoint overlaps current authority")
		}
	}
	return k.gcCheckpointRefs(ctx, state, handle)
}

func (k *KitGKERuntimeBackend) gcCheckpointRefs(ctx context.Context, state kitGKEAuthority, handle *kitGKEAuthorityHandle) (kitGKEAuthority, *kitGKEAuthorityHandle, error) {
	for len(state.PriorCheckpoints) > 0 {
		if err := k.deleteCheckpointRef(ctx, state.PriorCheckpoints[0]); err != nil {
			return state, handle, err
		}
		next := state
		next.PriorCheckpoints = state.PriorCheckpoints[1:]
		saved, err := k.saveAuthority(ctx, handle, next)
		if err != nil {
			return state, handle, err
		}
		state, handle = next, saved
	}
	return state, handle, nil
}

// gcRetiredCheckpoints is never used by idle Stop: its latest snapshot is the
// durable restore point and must remain indefinitely until replaced or retired.
func (k *KitGKERuntimeBackend) gcRetiredCheckpoints(ctx context.Context, state kitGKEAuthority, handle *kitGKEAuthorityHandle) (kitGKEAuthority, *kitGKEAuthorityHandle, error) {
	if state.Phase != "retired" {
		return state, handle, fmt.Errorf("Kit GKE checkpoint cleanup requires permanent retirement")
	}
	var err error
	state, handle, err = k.gcCheckpointRefs(ctx, state, handle)
	if err != nil {
		return state, handle, err
	}
	ref := kitGKECheckpointRef{TriggerName: state.TriggerName, TriggerUID: state.TriggerUID, SnapshotName: state.SnapshotName, SnapshotUID: state.SnapshotUID}
	if err = k.deleteCheckpointRef(ctx, ref); err != nil {
		return state, handle, err
	}
	next := state
	next.TriggerName, next.TriggerUID, next.SnapshotName, next.SnapshotUID, next.Source = "", "", "", "", nil
	saved, err := k.saveAuthority(ctx, handle, next)
	if err != nil {
		return state, handle, err
	}
	return next, saved, nil
}

func (k *KitGKERuntimeBackend) deleteCheckpointRef(ctx context.Context, ref kitGKECheckpointRef) error {
	// An entirely absent object is valid; a partially recorded identity is not.
	if (ref.TriggerName == "") != (ref.TriggerUID == "") || (ref.SnapshotName == "") != (ref.SnapshotUID == "") {
		return fmt.Errorf("Kit GKE checkpoint cleanup requires exact object identities")
	}
	if ref.TriggerName != "" {
		if err := k.deleteOwned(ctx, kitGKETriggerGVR, ref.TriggerName, ref.TriggerUID); err != nil {
			return err
		}
	}
	if ref.SnapshotName != "" {
		return k.deleteOwned(ctx, kitGKESnapshotGVR, ref.SnapshotName, ref.SnapshotUID)
	}
	return nil
}
