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
	if k.config.Lifecycle != nil && r.LifecycleTx == nil {
		return k.config.Lifecycle(ctx, r, k.Reap)
	}
	return k.reap(ctx, r, false)
}
func (k *KitGKERuntimeBackend) ReapStoppedMachine(ctx context.Context, r assistantRuntimeRecord) error {
	if k.config.Lifecycle != nil && r.LifecycleTx == nil {
		return k.config.Lifecycle(ctx, r, k.ReapStoppedMachine)
	}
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
		{kitServiceAccountGVR, m.Workload.ServiceAccount, m.ServiceAccountUID},
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
	if stoppedOnly && !r.Retired {
		// A stopped reusable lineage still needs its template/bootstrap to restore.
		// Only permanent retirement may destroy those resources or its latest snapshot.
		if state.Phase != "stopped" {
			return fmt.Errorf("kit idle cleanup requires stopped authority")
		}
		if exists[0] {
			if err = k.deleteOwned(ctx, steps[0].gvr, steps[0].name, steps[0].uid); err != nil {
				return err
			}
		}
		_, _, err = k.gcSupersededCheckpoints(ctx, state, authority)
		return err
	}

	if exists[1] && state.Phase == "active" && !r.Retired {
		if stoppedOnly {
			return fmt.Errorf("refusing to reap a live Kit GKE pod as a stopped machine")
		}
		p, err := k.protocolForLifecycle(ctx, r, true)
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
	// Publish revocation and retirement before external teardown. Retrying a
	// failed delete must not restore the credentials of a surviving process.
	state.Phase = "retired"
	authority, err = k.saveAuthority(ctx, authority, state)
	if err != nil {
		return err
	}
	for i, s := range steps {
		if !exists[i] {
			continue
		}
		if err = k.deleteOwned(ctx, s.gvr, s.name, s.uid); err != nil {
			return err
		}
	}
	if !stoppedOnly || r.Retired {
		_, _, err = k.gcRetiredCheckpoints(ctx, state, authority)
		return err
	}
	// Reaping idle machine resources must retain the latest restore point.
	_, _, err = k.gcSupersededCheckpoints(ctx, state, authority)
	return err
}
func (k *KitGKERuntimeBackend) deleteOwned(ctx context.Context, gvr schema.GroupVersionResource, name string, uid types.UID) error {
	if name == "" || uid == "" {
		return fmt.Errorf("refusing to delete Kit GKE resource without exact name and UID")
	}
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

// verifyStoppedSwitchSource runs under the caller's assistant lifecycle fence.
// A failed row alone is not settlement evidence: require durable stopped
// authority and the absence of both the process and its recreating claim.
func (k *KitGKERuntimeBackend) verifyStoppedSwitchSource(ctx context.Context, r assistantRuntimeRecord) error {
	if r.State != runtimeStateStopped && r.State != runtimeStateFailed {
		return errRuntimeSwitchUnavailable
	}
	state, _, err := k.loadAuthority(ctx, r)
	if err != nil {
		return fmt.Errorf("%w: inspect stopped Kit authority: %w", errRuntimeSwitchUnavailable, err)
	}
	if state.Phase != "stopped" && state.Phase != "retired" {
		return errRuntimeSwitchUnavailable
	}
	for _, resource := range []struct {
		gvr  schema.GroupVersionResource
		name string
	}{
		{gkeSandboxClaimGVR, kitGKEName(r)},
		{gkePodGVR, state.Metadata.PodName},
	} {
		if resource.name == "" {
			return errRuntimeSwitchUnavailable
		}
		_, err := k.config.GKE.Dynamic.Resource(resource.gvr).Namespace(k.config.GKE.Namespace).Get(ctx, resource.name, metav1.GetOptions{})
		if !apierrors.IsNotFound(err) {
			if err != nil {
				return fmt.Errorf("%w: inspect stopped Kit resource: %w", errRuntimeSwitchUnavailable, err)
			}
			return errRuntimeSwitchBusy
		}
	}
	return nil
}
