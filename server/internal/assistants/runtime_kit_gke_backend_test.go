package assistants

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

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
func (p *kitGKEBackendProtocol) CloseCheckpointSessions(context.Context, assistantRuntimeRecord) error {
	return nil
}
func (p *kitGKEBackendProtocol) VerifyCheckpointSettled(context.Context, assistantRuntimeRecord) error {
	if !p.settled {
		return errRuntimeSwitchBusy
	}
	return nil
}
func (p *kitGKEBackendProtocol) PrepareRuntimeSwitch(context.Context, assistantRecord, []assistantRuntimeRecord, int16) (runtimeSwitchReadiness, error) {
	return runtimeSwitchReadiness{Settled: p.settled, Release: func() {}}, nil
}

func kitGKEBackendFixture(t *testing.T) (*KitGKERuntimeBackend, *dynamicfake.FakeDynamicClient, assistantRuntimeRecord, *int) {
	t.Helper()
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{gkePodGVR: "PodList", gkeSandboxClaimGVR: "SandboxClaimList"})
	input := kitGKEInput()
	input.Command, input.Args = KitGatewayCommand(int(input.GuestPort))
	fences := new(int)
	k, err := NewKitGKERuntimeBackend(KitGKERuntimeConfig{GKE: GKERuntimeConfig{Dynamic: client, Namespace: input.Identity.Namespace, ServerURL: &url.URL{Scheme: "https", Host: "example.invalid"}}, Provision: input, AuthorityStore: newKitGKEAuthorityTestStore(),
		Bootstrap: func(context.Context, assistantRuntimeRecord) (KitLocalBootstrap, error) {
			return KitLocalBootstrap{AgentsMD: input.AgentsMD, InferenceJSON: input.InferenceJSON, MCPJSON: input.MCPJSON, GatewayToken: []byte(uuid.NewString())}, nil
		},
		Fence: func(context.Context, assistantRuntimeRecord) error { *fences++; return nil },
		Protocol: func(_ context.Context, _ assistantRuntimeRecord, endpoint *url.URL) (runtimeProtocol, error) {
			require.Equal(t, "https", endpoint.Scheme)
			return &kitGKEBackendProtocol{settled: true}, nil
		},
	})
	require.NoError(t, err)
	r := assistantRuntimeRecord{LifecycleTx: &kitGKEFixtureTransaction{Tx: nil}, ID: uuid.New(), AssistantID: uuid.New(), ProjectID: uuid.New(), RuntimeVersion: 3, Backend: runtimeBackendGKE, BackendMetadataJSON: []byte("{}")}
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
			pod.Object["spec"] = map[string]any{"containers": []any{map[string]any{"name": "kit", "image": k.ImageRef()}, map[string]any{"name": "acp-tls", "image": kitGKETLSProxyImage}}}
			pod.Object["status"] = map[string]any{"phase": "Running", "podIP": "10.1.2.3", "containerStatuses": []any{map[string]any{"name": "kit", "ready": true, "restartCount": int64(0), "imageID": "containerd://sha256:" + strings.Repeat("a", 64), "containerID": "containerd://instance"}, map[string]any{"name": "acp-tls", "ready": true, "restartCount": int64(0), "imageID": "containerd://sha256:" + strings.Repeat("c", 64)}}}
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
	require.Equal(t, "sha256:"+strings.Repeat("c", 64), state.Metadata.ProxyImageDigest)
	previousCertificate := append([]byte(nil), state.Metadata.TLSCertificate...)
	require.NotEmpty(t, previousCertificate)
	require.Equal(t, types.UID("snapshot-uid"), state.SnapshotUID)
	require.NoError(t, k.Stop(t.Context(), r))
	require.Equal(t, 1, *fences)
	_, err = k.Status(t.Context(), r)
	require.ErrorContains(t, err, "fenced")
	result, err = k.Ensure(t.Context(), r)
	require.NoError(t, err)
	require.False(t, result.ColdStart)
	r.BackendMetadataJSON = result.BackendMetadataJSON
	state, _, err = k.loadAuthority(t.Context(), r)
	require.NoError(t, err)
	require.Equal(t, "active", state.Phase)
	require.Equal(t, "sha256:"+strings.Repeat("c", 64), state.Metadata.ProxyImageDigest)
	require.NotEqual(t, previousCertificate, state.Metadata.TLSCertificate)
	_, err = kitGKETLSConfig(state.Metadata.TLSCertificate, kitGKEName(r)+".kit.internal")
	require.NoError(t, err)
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

func TestKitGKEBackendObservedProcessIdentity(t *testing.T) {
	t.Parallel()
	for _, change := range []string{"image", "restart", "not-ready", "container", "tls-not-ready", "tls-restart", "tls-missing", "tls-image", "tls-image-missing", "tls-image-invalid"} {
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
			case "tls-not-ready", "tls-restart":
				proxy, ok := statuses[1].(map[string]any)
				require.True(t, ok)
				if change == "tls-not-ready" {
					proxy["ready"] = false
				} else {
					proxy["restartCount"] = int64(1)
				}
			case "tls-image", "tls-image-missing", "tls-image-invalid":
				proxy, ok := statuses[1].(map[string]any)
				require.True(t, ok)
				switch change {
				case "tls-image":
					proxy["imageID"] = "containerd://sha256:" + strings.Repeat("d", 64)
				case "tls-image-missing":
					delete(proxy, "imageID")
				case "tls-image-invalid":
					proxy["imageID"] = "nginx:1.28-alpine"
				}
			case "tls-missing":
				statuses = statuses[:1]
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

// A protocol factory is not network evidence; failed initialization keeps the
// authoritative phase restoring even when Kubernetes reports a Running Pod.
type kitGKEFailedStatusProtocol struct{ kitGKEBackendProtocol }

func (*kitGKEFailedStatusProtocol) Status(context.Context, assistantRuntimeRecord) (RuntimeBackendStatus, error) {
	return RuntimeBackendStatus{}, fmt.Errorf("fresh gateway authentication failed")
}
func TestKitGKEBackendRestoreAuthenticationBeforeActive(t *testing.T) {
	t.Parallel()
	k, client, r, _ := kitGKEBackendFixture(t)
	k.config.Provision.SnapshotsEnabled = true
	k.config.Snapshots = &kitGKEFakeCheckpointDriver{client: client, restored: true}
	result, err := k.Ensure(t.Context(), r)
	require.NoError(t, err)
	r.BackendMetadataJSON = result.BackendMetadataJSON
	require.NoError(t, k.Stop(t.Context(), r))
	before, _, err := k.loadAuthority(t.Context(), r)
	require.NoError(t, err)
	k.config.Protocol = func(context.Context, assistantRuntimeRecord, *url.URL) (runtimeProtocol, error) {
		return &kitGKEFailedStatusProtocol{}, nil
	}
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	_, err = k.Ensure(ctx, r)
	require.ErrorContains(t, err, "fresh gateway authentication failed")
	state, _, err := k.loadAuthority(t.Context(), r)
	require.NoError(t, err)
	require.Equal(t, "restoring", state.Phase)
	require.NotEqual(t, before.Metadata.SecretName, state.Metadata.SecretName)
	require.NotEqual(t, before.Metadata.CheckpointMarker, state.Metadata.CheckpointMarker)
	require.Equal(t, before.Metadata.CheckpointMarker, state.Source.CheckpointMarker)
}

// Embed only the declared interfaces, not the underlying concrete settlement API.
type kitGKEMissingSettlementProtocol struct {
	runtimeProtocol
	runtimeSwitchCoordinator
}
type kitGKEBusySettlementProtocol struct{ kitGKEBackendProtocol }

func (*kitGKEBusySettlementProtocol) VerifyCheckpointSettled(context.Context, assistantRuntimeRecord) error {
	return errRuntimeSwitchBusy
}
func TestKitGKEBackendCheckpointRequiresGatewaySettlement(t *testing.T) {
	t.Parallel()
	for _, missing := range []bool{true, false} {
		name := "busy"
		if missing {
			name = "missing"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			k, client, r, fences := kitGKEBackendFixture(t)
			k.config.Provision.SnapshotsEnabled = true
			k.config.Snapshots = &kitGKEFakeCheckpointDriver{client: client, restored: true}
			result, err := k.Ensure(t.Context(), r)
			require.NoError(t, err)
			r.BackendMetadataJSON = result.BackendMetadataJSON
			k.config.Protocol = func(context.Context, assistantRuntimeRecord, *url.URL) (runtimeProtocol, error) {
				base := &kitGKEBackendProtocol{settled: true}
				if missing {
					return &kitGKEMissingSettlementProtocol{runtimeProtocol: base, runtimeSwitchCoordinator: base}, nil
				}
				return &kitGKEBusySettlementProtocol{kitGKEBackendProtocol: *base}, nil
			}
			client.ClearActions()
			err = k.Stop(t.Context(), r)
			if missing {
				require.ErrorIs(t, err, errRuntimeSwitchUnavailable)
			} else {
				require.ErrorIs(t, err, errRuntimeSwitchBusy)
			}
			require.Zero(t, *fences)
			for _, action := range client.Actions() {
				require.Equal(t, "get", action.GetVerb())
			}
			state, _, err := k.loadAuthority(t.Context(), r)
			require.NoError(t, err)
			require.Equal(t, "active", state.Phase)
		})
	}
}

func TestKitGKEBackendRestoreRetriesBootstrapFailure(t *testing.T) {
	t.Parallel()
	k, client, r, fences := kitGKEBackendFixture(t)
	k.config.Provision.SnapshotsEnabled = true
	k.config.Snapshots = &kitGKEFakeCheckpointDriver{client: client, restored: true}
	result, err := k.Ensure(t.Context(), r)
	require.NoError(t, err)
	r.BackendMetadataJSON = result.BackendMetadataJSON
	require.NoError(t, k.Stop(t.Context(), r))
	before, _, err := k.loadAuthority(t.Context(), r)
	require.NoError(t, err)
	bootstrap := k.config.Bootstrap
	attempts := 0
	k.config.Bootstrap = func(ctx context.Context, r assistantRuntimeRecord) (KitLocalBootstrap, error) {
		attempts++
		if attempts == 1 {
			return KitLocalBootstrap{}, fmt.Errorf("temporary bootstrap outage")
		}
		return bootstrap(ctx, r)
	}
	client.ClearActions()
	_, err = k.Ensure(t.Context(), r)
	require.ErrorContains(t, err, "temporary bootstrap outage")
	require.Equal(t, 2, *fences, "failed bootstrap authority must be revoked")
	for _, action := range client.Actions() {
		require.Equal(t, "get", action.GetVerb(), "preparation must not mutate Kubernetes resources")
	}
	state, _, err := k.loadAuthority(t.Context(), r)
	require.NoError(t, err)
	require.Equal(t, "stopped", state.Phase)
	require.Equal(t, before.Metadata, state.Metadata)
	require.Equal(t, before.Source, state.Source)
	require.Equal(t, before.SnapshotUID, state.SnapshotUID)
	result, err = k.Ensure(t.Context(), r)
	require.NoError(t, err)
	require.False(t, result.ColdStart)
	require.Equal(t, 2, attempts)
	r.BackendMetadataJSON = result.BackendMetadataJSON
	state, _, err = k.loadAuthority(t.Context(), r)
	require.NoError(t, err)
	require.Equal(t, "active", state.Phase)
	require.NotEqual(t, before.Metadata.CheckpointMarker, state.Metadata.CheckpointMarker)
}

func TestKitGKEBackendRestoreRejectsProxyImageDrift(t *testing.T) {
	t.Parallel()
	k, client, r, _ := kitGKEBackendFixture(t)
	k.config.Provision.SnapshotsEnabled = true
	driver := &kitGKEFakeCheckpointDriver{client: client, restored: true}
	k.config.Snapshots = driver
	result, err := k.Ensure(t.Context(), r)
	require.NoError(t, err)
	r.BackendMetadataJSON = result.BackendMetadataJSON
	require.NoError(t, k.Stop(t.Context(), r))
	client.PrependReactor("list", "pods", func(action ktesting.Action) (bool, runtime.Object, error) {
		object, err := client.Tracker().List(gkePodGVR, gkePodGVR.GroupVersion().WithKind("Pod"), action.GetNamespace())
		require.NoError(t, err)
		pods, ok := object.(*unstructured.UnstructuredList)
		require.True(t, ok)
		for i := range pods.Items {
			statuses, _, err := unstructured.NestedSlice(pods.Items[i].Object, "status", "containerStatuses")
			require.NoError(t, err)
			proxy, ok := statuses[1].(map[string]any)
			require.True(t, ok)
			proxy["imageID"] = "containerd://sha256:" + strings.Repeat("d", 64)
			require.NoError(t, unstructured.SetNestedSlice(pods.Items[i].Object, statuses, "status", "containerStatuses"))
		}
		return true, pods, nil
	})
	_, err = k.Ensure(t.Context(), r)
	require.ErrorContains(t, err, "TLS proxy image digest compatibility mismatch")
}
