package assistants

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	ktesting "k8s.io/client-go/testing"
)

type kitGKEBackendProtocol struct{ settled bool }

func (p *kitGKEBackendProtocol) RunTurn(context.Context, assistantRuntimeRecord, runTurnRequest) error {
	return nil
}
func (p *kitGKEBackendProtocol) InterruptTurn(context.Context, assistantRuntimeRecord, uuid.UUID) (bool, error) {
	return true, nil
}
func (p *kitGKEBackendProtocol) Status(context.Context, assistantRuntimeRecord) (RuntimeBackendStatus, error) {
	return RuntimeBackendStatus{Configured: true}, nil
}
func (p *kitGKEBackendProtocol) PrepareRuntimeSwitch(context.Context, assistantRecord, []assistantRuntimeRecord, int16) (runtimeSwitchReadiness, error) {
	return runtimeSwitchReadiness{Settled: p.settled, Release: func() {}}, nil
}

func kitGKEBackendFixture(t *testing.T) (*KitGKERuntimeBackend, *dynamicfake.FakeDynamicClient, assistantRuntimeRecord, *int) {
	t.Helper()
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{gkePodGVR: "PodList"})
	input := kitGKEInput()
	input.Command, input.Args = KitGatewayCommand(int(input.GuestPort))
	fences := new(int)
	k, err := NewKitGKERuntimeBackend(KitGKERuntimeConfig{GKE: GKERuntimeConfig{Dynamic: client, Namespace: input.Identity.Namespace, ServerURL: &url.URL{Scheme: "https", Host: "example.invalid"}}, Provision: input,
		Bootstrap: func(context.Context, assistantRuntimeRecord) (KitLocalBootstrap, error) {
			return KitLocalBootstrap{AgentsMD: input.AgentsMD, InferenceJSON: input.InferenceJSON, MCPJSON: input.MCPJSON, GatewayToken: input.GatewayToken}, nil
		},
		Fence: func(context.Context, assistantRuntimeRecord) error { *fences++; return nil },
		Protocol: func(context.Context, assistantRuntimeRecord, *url.URL) (runtimeProtocol, error) {
			return &kitGKEBackendProtocol{settled: true}, nil
		},
	})
	require.NoError(t, err)
	r := assistantRuntimeRecord{ID: uuid.New(), AssistantID: uuid.New(), ProjectID: uuid.New(), RuntimeVersion: 3, Backend: runtimeBackendGKE, BackendMetadataJSON: []byte("{}")}
	client.PrependReactor("create", "*", func(action ktesting.Action) (bool, runtime.Object, error) {
		a, ok := action.(ktesting.CreateAction)
		require.True(t, ok)
		obj, ok := a.GetObject().(*unstructured.Unstructured)
		require.True(t, ok)
		obj.SetUID(types.UID("uid-" + obj.GetName()))
		obj.SetResourceVersion("1")
		if action.GetResource() == gkeSandboxClaimGVR {
			pod := kitObject("Pod", "v1", "pod-"+r.ID.String(), k.identity(r))
			pod.SetUID("pod-uid")
			labels := obj.GetLabels()
			labels[gkeClaimUIDLabel] = string(obj.GetUID())
			pod.SetLabels(labels)
			pod.SetAnnotations(obj.GetAnnotations())
			pod.Object["spec"] = map[string]any{"containers": []any{map[string]any{"name": "kit", "image": k.ImageRef()}}}
			pod.Object["status"] = map[string]any{"phase": "Running", "podIP": "10.1.2.3", "containerStatuses": []any{map[string]any{"name": "kit", "ready": true, "restartCount": int64(0), "imageID": "containerd://sha256:" + strings.Repeat("a", 64), "containerID": "containerd://instance"}}}
			require.NoError(t, client.Tracker().Create(gkePodGVR, pod, k.config.GKE.Namespace))
		}
		return false, nil, nil
	})
	return k, client, r, fences
}
func TestKitGKEBackendColdEnsureAndExactReuse(t *testing.T) {
	t.Parallel()
	k, client, r, _ := kitGKEBackendFixture(t)
	result, err := k.Ensure(t.Context(), r)
	require.NoError(t, err)
	require.True(t, result.ColdStart)
	r.BackendMetadataJSON = result.BackendMetadataJSON
	result, err = k.Ensure(t.Context(), r)
	require.NoError(t, err)
	require.False(t, result.ColdStart)
	m, err := k.metadata(r)
	require.NoError(t, err)
	pod, err := client.Resource(gkePodGVR).Namespace(m.Namespace).Get(t.Context(), m.PodName, metav1.GetOptions{})
	require.NoError(t, err)
	pod.SetUID("replacement")
	_, err = client.Resource(gkePodGVR).Namespace(m.Namespace).Update(t.Context(), pod, metav1.UpdateOptions{})
	require.NoError(t, err)
	_, err = k.Ensure(t.Context(), r)
	require.ErrorContains(t, err, "authority mismatch")
}
func TestKitGKEBackendStopCannotClaimCheckpointSuccess(t *testing.T) {
	t.Parallel()
	k, _, r, fences := kitGKEBackendFixture(t)
	result, err := k.Ensure(t.Context(), r)
	require.NoError(t, err)
	r.BackendMetadataJSON = result.BackendMetadataJSON
	require.ErrorContains(t, k.Stop(t.Context(), r), "checkpoint/restore unavailable")
	require.Zero(t, *fences)
	config := k.config
	config.Provision.SnapshotsEnabled = true
	managed, err := NewKitGKERuntimeBackend(config)
	require.NoError(t, err)
	require.IsType(t, &KitGKEManagedCheckpointDriver{}, managed.config.Snapshots)
}
func TestKitGKEBackendReapRequiresSettlementAndFences(t *testing.T) {
	t.Parallel()
	k, client, r, fences := kitGKEBackendFixture(t)
	result, err := k.Ensure(t.Context(), r)
	require.NoError(t, err)
	r.BackendMetadataJSON = result.BackendMetadataJSON
	require.ErrorContains(t, k.ReapStoppedMachine(t.Context(), r), "live Kit GKE pod")
	k.config.Protocol = func(context.Context, assistantRuntimeRecord, *url.URL) (runtimeProtocol, error) {
		return &kitGKEBackendProtocol{settled: false}, nil
	}
	require.ErrorIs(t, k.Reap(t.Context(), r), errRuntimeSwitchBusy)
	require.Zero(t, *fences)
	k.config.Protocol = func(context.Context, assistantRuntimeRecord, *url.URL) (runtimeProtocol, error) {
		return &kitGKEBackendProtocol{settled: true}, nil
	}
	require.NoError(t, k.Reap(t.Context(), r))
	require.Equal(t, 1, *fences)
	require.NoError(t, k.Reap(t.Context(), r))
	for _, a := range client.Actions() {
		if a.GetVerb() == "delete" {
			deletion, ok := a.(ktesting.DeleteAction)
			require.True(t, ok)
			require.NotNil(t, deletion.GetDeleteOptions().Preconditions.UID)
		}
	}
	_, err = k.Ensure(t.Context(), r)
	require.Error(t, err)
}
func TestKitGKEBackendRejectsForeignAndUnrecordedAuthority(t *testing.T) {
	t.Parallel()
	k, _, r, _ := kitGKEBackendFixture(t)
	result, err := k.Ensure(t.Context(), r)
	require.NoError(t, err)
	_, err = k.Ensure(t.Context(), r)
	require.ErrorContains(t, err, "unrecorded")
	r.BackendMetadataJSON = result.BackendMetadataJSON
	r.ProjectID = uuid.New()
	_, err = k.Ensure(t.Context(), r)
	require.ErrorContains(t, err, "authority")
	var m kitGKEBackendMetadata
	require.NoError(t, json.Unmarshal(result.BackendMetadataJSON, &m))
	m.Fingerprint = "old"
	r.ProjectID = m.ProjectID
	r.BackendMetadataJSON, _ = json.Marshal(m)
	_, err = k.RecycleImage(t.Context(), r)
	require.ErrorContains(t, err, "compatibility")
}

type kitGKEFakeCheckpointDriver struct {
	client      *dynamicfake.FakeDynamicClient
	selected    *unstructured.Unstructured
	unavailable bool
	restored    bool
	refreshed   bool
}

func (d *kitGKEFakeCheckpointDriver) Select(ctx context.Context, trigger *unstructured.Unstructured, binding KitGKESnapshotBinding) (*unstructured.Unstructured, error) {
	if trigger.GetUID() == "" || binding.SourcePodUID == "" {
		return nil, fmt.Errorf("missing trigger/source authority")
	}
	d.selected = kitObject("PodSnapshot", "podsnapshot.gke.io/v1", "verified-snapshot", binding.Identity)
	d.selected.SetUID("snapshot-uid")
	if err := d.client.Tracker().Create(kitGKESnapshotGVR, d.selected, binding.Identity.Namespace); err != nil {
		return nil, fmt.Errorf("kit GKE Select: %w", err)
	}
	return d.selected, nil
}
func (d *kitGKEFakeCheckpointDriver) VerifyAvailable(context.Context, *unstructured.Unstructured, KitGKESnapshotBinding) error {
	if d.unavailable {
		return fmt.Errorf("backing snapshot expired")
	}
	return nil
}
func (d *kitGKEFakeCheckpointDriver) VerifyRestored(context.Context, *unstructured.Unstructured, *unstructured.Unstructured, KitGKESnapshotBinding) error {
	if !d.restored {
		return fmt.Errorf("GKE silently cold started")
	}
	return nil
}
func (d *kitGKEFakeCheckpointDriver) RefreshRestored(context.Context, assistantRuntimeRecord, *unstructured.Unstructured, KitLocalBootstrap) error {
	d.refreshed = true
	return nil
}

func TestKitGKEBackendCheckpointRestorePersistsAuthority(t *testing.T) {
	t.Parallel()
	k, client, r, fences := kitGKEBackendFixture(t)
	k.config.Provision.SnapshotsEnabled = true
	driver := &kitGKEFakeCheckpointDriver{client: client, restored: true}
	k.config.Snapshots = driver
	result, err := k.Ensure(t.Context(), r)
	require.NoError(t, err)
	r.BackendMetadataJSON = result.BackendMetadataJSON
	require.NoError(t, k.Stop(t.Context(), r))
	require.Equal(t, 1, *fences)
	state, _, err := k.loadAuthority(t.Context(), r)
	require.NoError(t, err)
	require.Equal(t, "stopped", state.Phase)
	require.Equal(t, types.UID("snapshot-uid"), state.SnapshotUID)
	require.NoError(t, k.Stop(t.Context(), r))
	require.Equal(t, 1, *fences)
	_, err = k.Status(t.Context(), r)
	require.ErrorContains(t, err, "fenced")
	result, err = k.Ensure(t.Context(), r)
	require.NoError(t, err)
	require.False(t, result.ColdStart)
	require.True(t, driver.refreshed)
	r.BackendMetadataJSON = result.BackendMetadataJSON
	state, _, err = k.loadAuthority(t.Context(), r)
	require.NoError(t, err)
	require.Equal(t, "active", state.Phase)
	template, err := client.Resource(kitTemplateGVR).Namespace(k.config.GKE.Namespace).Get(t.Context(), kitGKEName(r), metav1.GetOptions{})
	require.NoError(t, err)
	selected, _, err := unstructured.NestedString(template.Object, "spec", "podTemplate", "metadata", "annotations", "podsnapshot.gke.io/ps-name")
	require.NoError(t, err)
	require.Equal(t, "verified-snapshot", selected)
}
func TestKitGKEBackendCheckpointRejectsColdFallback(t *testing.T) {
	t.Parallel()
	k, client, r, _ := kitGKEBackendFixture(t)
	k.config.Provision.SnapshotsEnabled = true
	driver := &kitGKEFakeCheckpointDriver{client: client}
	k.config.Snapshots = driver
	result, err := k.Ensure(t.Context(), r)
	require.NoError(t, err)
	r.BackendMetadataJSON = result.BackendMetadataJSON
	require.NoError(t, k.Stop(t.Context(), r))
	_, err = k.Ensure(t.Context(), r)
	require.ErrorContains(t, err, "cold started")
	require.False(t, driver.refreshed)
	state, _, err := k.loadAuthority(t.Context(), r)
	require.NoError(t, err)
	require.Equal(t, "restoring", state.Phase)
	_, err = k.Status(t.Context(), r)
	require.ErrorContains(t, err, "fenced")
}
func TestKitGKEBackendCheckpointUnavailableNeverDeletes(t *testing.T) {
	t.Parallel()
	k, client, r, _ := kitGKEBackendFixture(t)
	k.config.Provision.SnapshotsEnabled = true
	driver := &kitGKEFakeCheckpointDriver{client: client, unavailable: true}
	k.config.Snapshots = driver
	result, err := k.Ensure(t.Context(), r)
	require.NoError(t, err)
	r.BackendMetadataJSON = result.BackendMetadataJSON
	require.ErrorContains(t, k.Stop(t.Context(), r), "expired")
	for _, a := range client.Actions() {
		require.NotEqual(t, "delete", a.GetVerb())
	}
	state, _, err := k.loadAuthority(t.Context(), r)
	require.NoError(t, err)
	require.Equal(t, "checkpointing", state.Phase)
}

func (d *kitGKEFakeCheckpointDriver) PrepareColdRestore(_ context.Context, _ assistantRuntimeRecord, template *unstructured.Unstructured, _ *unstructured.Unstructured, _ KitGKESnapshotBinding) (*unstructured.Unstructured, error) {
	return template, nil
}

func TestKitGKEBackendObservedProcessIdentity(t *testing.T) {
	t.Parallel()
	for _, change := range []string{"image", "restart", "not-ready", "container"} {
		t.Run(change, func(t *testing.T) {
			t.Parallel()
			k, client, r, _ := kitGKEBackendFixture(t)
			result, err := k.Ensure(t.Context(), r)
			require.NoError(t, err)
			r.BackendMetadataJSON = result.BackendMetadataJSON
			m, err := k.metadata(r)
			require.NoError(t, err)
			pod, err := client.Resource(gkePodGVR).Namespace(m.Namespace).Get(t.Context(), m.PodName, metav1.GetOptions{})
			require.NoError(t, err)
			statuses, _, _ := unstructured.NestedSlice(pod.Object, "status", "containerStatuses")
			status, ok := statuses[0].(map[string]any)
			require.True(t, ok)
			switch change {
			case "image":
				status["imageID"] = "containerd://sha256:" + strings.Repeat("b", 64)
			case "restart":
				status["restartCount"] = int64(1)
			case "not-ready":
				status["ready"] = false
			case "container":
				status["containerID"] = "containerd://replacement"
			}
			require.NoError(t, unstructured.SetNestedSlice(pod.Object, statuses, "status", "containerStatuses"))
			_, err = client.Resource(gkePodGVR).Namespace(m.Namespace).Update(t.Context(), pod, metav1.UpdateOptions{})
			require.NoError(t, err)
			_, err = k.Status(t.Context(), r)
			require.Error(t, err)
		})
	}
}
func TestKitGKEBackendDerivesProductionDefaultsAndAcceptsTags(t *testing.T) {
	t.Parallel()
	k, _, _, _ := kitGKEBackendFixture(t)
	config := k.config
	config.Provision.Command = nil
	config.Provision.Args = nil
	config.Provision.Identity.CompatibilityGeneration = ""
	config.Provision.GuestPort = 0
	config.Provision.Image = "example.invalid/kit:2c4a35e"
	backend, err := NewKitGKERuntimeBackend(config)
	require.NoError(t, err)
	require.Equal(t, int32(8081), backend.config.Provision.GuestPort)
	require.NotEmpty(t, backend.config.Provision.Identity.CompatibilityGeneration)
}

func (d *kitGKEFakeCheckpointDriver) VerifySettled(context.Context, assistantRuntimeRecord, *unstructured.Unstructured) error {
	return nil
}
