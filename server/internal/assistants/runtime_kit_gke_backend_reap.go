package assistants

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

// Reap is permanent deletion, not a substitute for checkpoint Stop. No cold
// recreation is permitted for this row after deletion. The caller retires it.
func (k *KitGKERuntimeBackend) Reap(ctx context.Context, r assistantRuntimeRecord) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.reap(ctx, r, false)
}
func (k *KitGKERuntimeBackend) ReapStoppedMachine(ctx context.Context, r assistantRuntimeRecord) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.reap(ctx, r, true)
}
func (k *KitGKERuntimeBackend) reap(ctx context.Context, r assistantRuntimeRecord, stoppedOnly bool) error {
	state, authority, err := k.loadAuthority(ctx, r)
	if err != nil {
		return err
	}
	m := state.Metadata
	r.BackendMetadataJSON, _ = json.Marshal(m)
	// Inspect all objects before taking any destructive action. A label selector
	// alone is never sufficient deletion authority.
	steps := []struct {
		gvr  schema.GroupVersionResource
		name string
		uid  types.UID
	}{
		{gkeSandboxClaimGVR, kitGKEName(r), m.ClaimUID},
		{gkePodGVR, m.PodName, m.PodUID},
		{kitTemplateGVR, kitGKEName(r), m.TemplateUID},
		{kitSecretGVR, m.SecretName, m.SecretUID},
	}
	exists := make([]bool, len(steps))
	for i, s := range steps {
		_, err = k.get(ctx, r, s.gvr, s.name, s.uid)
		if apierrors.IsNotFound(err) {
			continue
		}
		if err != nil {
			return err
		}
		exists[i] = true
	}
	if stoppedOnly && exists[1] {
		return fmt.Errorf("refusing to reap a live Kit GKE pod as a stopped machine")
	}
	if exists[1] && state.Phase == "active" {
		if stoppedOnly {
			return fmt.Errorf("refusing to reap a live Kit GKE pod as a stopped machine")
		}
		p, err := k.protocol(ctx, r)
		if err != nil {
			return err
		}
		coordinator, ok := p.(runtimeSwitchCoordinator)
		if !ok {
			return errRuntimeSwitchUnavailable
		}
		var assistant assistantRecord
		assistant.ID = r.AssistantID
		assistant.ProjectID = r.ProjectID
		fence, err := coordinator.PrepareRuntimeSwitch(ctx, assistant, []assistantRuntimeRecord{r}, 3)
		if fence.Release != nil {
			defer fence.Release()
		}
		if err != nil {
			return fmt.Errorf("kit GKE reap: %w", err)
		}
		if fence.Release == nil {
			return errRuntimeSwitchUnavailable
		}
		if !fence.Settled {
			return errRuntimeSwitchBusy
		}
	}
	if err = k.config.Fence(ctx, r); err != nil {
		return fmt.Errorf("fence Kit GKE authority before reap: %w", err)
	}
	for i, s := range steps {
		if !exists[i] {
			continue
		}
		if err = k.deleteOwned(ctx, s.gvr, s.name, s.uid); err != nil {
			return err
		}
	}
	for _, s := range []struct {
		gvr  schema.GroupVersionResource
		name string
		uid  types.UID
	}{{kitGKETriggerGVR, state.TriggerName, state.TriggerUID}, {kitGKESnapshotGVR, state.SnapshotName, state.SnapshotUID}} {
		if s.name != "" && s.uid != "" {
			if err = k.deleteOwned(ctx, s.gvr, s.name, s.uid); err != nil {
				return err
			}
		}
	}
	for _, checkpoint := range state.PriorCheckpoints {
		if err = k.deleteOwned(ctx, kitGKETriggerGVR, checkpoint.TriggerName, checkpoint.TriggerUID); err != nil {
			return err
		}
		if err = k.deleteOwned(ctx, kitGKESnapshotGVR, checkpoint.SnapshotName, checkpoint.SnapshotUID); err != nil {
			return err
		}
	}
	state.Phase = "retired"
	_, err = k.saveAuthority(ctx, authority, state)
	return err
}
func (k *KitGKERuntimeBackend) deleteOwned(ctx context.Context, gvr schema.GroupVersionResource, name string, uid types.UID) error {
	ctx, cancel := context.WithTimeout(ctx, gkeRuntimeReapCallTimeout)
	defer cancel()
	resource := k.config.GKE.Dynamic.Resource(gvr).Namespace(k.config.GKE.Namespace)
	propagation := metav1.DeletePropagationForeground
	err := resource.Delete(ctx, name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}, PropagationPolicy: &propagation})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("delete owned Kit GKE %s: %w", gvr.Resource, err)
	}
	for {
		obj, err := resource.Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("kit GKE deleteOwned: %w", err)
		}
		if obj.GetUID() != uid {
			return fmt.Errorf("kit GKE resource replaced while waiting for deletion")
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait Kit GKE deletion: %w", ctx.Err())
		case <-time.After(gkeRuntimePollInterval):
		}
	}
}
func (k *KitGKERuntimeBackend) RuntimeExists(ctx context.Context, r assistantRuntimeRecord) (bool, error) {
	m, err := k.metadata(r)
	if err != nil {
		return false, err
	}
	_, err = k.get(ctx, r, gkeSandboxClaimGVR, kitGKEName(r), m.ClaimUID)
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	return err == nil, err
}
