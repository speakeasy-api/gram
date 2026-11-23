package assistants

import (
	"context"
	"fmt"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ktesting "k8s.io/client-go/testing"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestKitGKEManagedPolicyRequiresObservedGeneration(t *testing.T) {
	t.Parallel()
	for _, observed := range []int64{-1, 0, 1, 2} {
		policy := kitManagedPolicy(kitGKEInput().Identity, "rootfs-only")
		if observed == -1 {
			delete(policy.Object, "status")
		} else {
			require.NoError(t, unstructured.SetNestedField(policy.Object, observed, "status", "observedGeneration"))
		}
		err := kitGKEManagedScope(policy)
		if observed == 1 {
			require.NoError(t, err)
		} else {
			require.ErrorIs(t, err, ErrKitGKEManagedSnapshotSchema)
		}
	}
}

func TestKitGKEBackendRequiresHTTPSManagement(t *testing.T) {
	t.Parallel()
	k, _, _, _ := kitGKEBackendFixture(t)
	k.config.GKE.ServerURL.Scheme = "http"
	_, err := NewKitGKERuntimeBackend(k.config)
	require.ErrorContains(t, err, "HTTPS management URL")
}

func TestKitGKEStoppedReapPreservesReusableLineage(t *testing.T) {
	t.Parallel()
	k, client, r, _ := kitGKEBackendFixture(t)
	k.config.Provision.SnapshotsEnabled = true
	k.config.Snapshots = &kitGKEFakeCheckpointDriver{client: client, restored: true}
	result, err := k.Ensure(t.Context(), r)
	require.NoError(t, err)
	r.BackendMetadataJSON = result.BackendMetadataJSON
	claim, err := client.Resource(kitClaimGVR).Namespace(k.config.GKE.Namespace).Get(t.Context(), kitGKEName(r), metav1.GetOptions{})
	require.NoError(t, err)
	require.NoError(t, k.Stop(t.Context(), r))
	// A leftover claim must be removed without retiring reusable authority.
	err = client.Tracker().Create(kitClaimGVR, claim, k.config.GKE.Namespace)
	require.NoError(t, err)
	require.NoError(t, k.ReapStoppedMachine(t.Context(), r))
	_, err = client.Resource(kitClaimGVR).Namespace(k.config.GKE.Namespace).Get(t.Context(), kitGKEName(r), metav1.GetOptions{})
	require.True(t, apierrors.IsNotFound(err))
	state, _, err := k.loadAuthority(t.Context(), r)
	require.NoError(t, err)
	require.Equal(t, "stopped", state.Phase)
	_, err = k.get(t.Context(), r, kitTemplateGVR, kitGKETemplateName(kitGKEName(r)), state.Metadata.TemplateUID)
	require.NoError(t, err)
	_, err = k.get(t.Context(), r, kitSecretGVR, state.Metadata.SecretName, state.Metadata.SecretUID)
	require.NoError(t, err)
	snapshot, err := client.Resource(kitGKESnapshotGVR).Namespace(k.config.GKE.Namespace).Get(t.Context(), state.SnapshotName, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, state.SnapshotUID, snapshot.GetUID())
}

func TestKitGKESwitchSettledStoppedSource(t *testing.T) {
	t.Parallel()
	for _, rowState := range []string{runtimeStateStopped, runtimeStateFailed} {
		t.Run(rowState, func(t *testing.T) {
			t.Parallel()
			k, client, r, _ := kitGKEBackendFixture(t)
			k.config.Provision.SnapshotsEnabled = true
			k.config.Snapshots = &kitGKEFakeCheckpointDriver{client: client, restored: true}
			result, err := k.Ensure(t.Context(), r)
			require.NoError(t, err)
			r.BackendMetadataJSON = result.BackendMetadataJSON
			r.State = rowState
			var assistant assistantRecord
			assistant.ID, assistant.ProjectID = r.AssistantID, r.ProjectID
			fence, err := k.PrepareRuntimeSwitch(t.Context(), assistant, []assistantRuntimeRecord{r}, 2)
			require.ErrorIs(t, err, errRuntimeSwitchUnavailable, "row state alone is not settlement")
			fence.Release()
			pod, err := client.Resource(gkePodGVR).Namespace(k.config.GKE.Namespace).Get(t.Context(), "pod-"+r.ID.String(), metav1.GetOptions{})
			require.NoError(t, err)
			require.NoError(t, k.Stop(t.Context(), r))
			fence, err = k.PrepareRuntimeSwitch(t.Context(), assistant, []assistantRuntimeRecord{r}, 2)
			require.NoError(t, err)
			require.True(t, fence.Settled)
			require.NotNil(t, fence.Release)
			fence.Release()
			_, err = client.Resource(gkePodGVR).Namespace(k.config.GKE.Namespace).Create(t.Context(), pod, metav1.CreateOptions{})
			require.NoError(t, err)
			fence, err = k.PrepareRuntimeSwitch(t.Context(), assistant, []assistantRuntimeRecord{r}, 2)
			require.ErrorIs(t, err, errRuntimeSwitchBusy, "stopped authority cannot override a live process")
			fence.Release()
		})
	}
}

func TestKitGKEBackendRejectsUnsafeTransportOptions(t *testing.T) {
	t.Parallel()
	for _, option := range []string{"userinfo", "insecure-inference"} {
		t.Run(option, func(t *testing.T) {
			t.Parallel()
			k, _, _, _ := kitGKEBackendFixture(t)
			if option == "userinfo" {
				k.config.GKE.ServerURL.User = url.User("embedded")
			} else {
				k.config.Provision.AllowInsecureHTTP = true
			}
			_, err := NewKitGKERuntimeBackend(k.config)
			require.Error(t, err)
		})
	}
}

func TestKitGKEColdCreationFailureCleansOwnedResources(t *testing.T) {
	t.Parallel()
	for _, failed := range []string{kitTemplateGVR.Resource, kitWarmPoolGVR.Resource, kitClaimGVR.Resource} {
		t.Run(failed, func(t *testing.T) {
			t.Parallel()
			k, client, r, fences := kitGKEBackendFixture(t)
			var secretName, serviceAccountName string
			client.PrependReactor("create", "serviceaccounts", func(action ktesting.Action) (bool, runtime.Object, error) {
				creation, ok := action.(ktesting.CreateAction)
				require.True(t, ok)
				serviceAccount, ok := creation.GetObject().(*unstructured.Unstructured)
				require.True(t, ok)
				serviceAccountName = serviceAccount.GetName()
				return false, nil, nil
			})
			client.PrependReactor("create", "secrets", func(action ktesting.Action) (bool, runtime.Object, error) {
				create, ok := action.(ktesting.CreateAction)
				require.True(t, ok)
				secret, ok := create.GetObject().(*unstructured.Unstructured)
				require.True(t, ok)
				secretName = secret.GetName()
				return false, nil, nil
			})
			client.PrependReactor("create", failed, func(ktesting.Action) (bool, runtime.Object, error) {
				return true, nil, fmt.Errorf("injected create failure")
			})
			_, err := k.Ensure(t.Context(), r)
			require.ErrorContains(t, err, "injected create failure")
			require.Equal(t, 1, *fences)
			require.NotEmpty(t, secretName)
			type objectIdentity struct {
				gvr  schema.GroupVersionResource
				name string
			}
			expected := []objectIdentity{{gvr: kitServiceAccountGVR, name: serviceAccountName}, {gvr: kitSecretGVR, name: secretName}}
			if failed == kitClaimGVR.Resource || failed == kitWarmPoolGVR.Resource {
				expected = append(expected, objectIdentity{gvr: kitTemplateGVR, name: kitGKETemplateName(kitGKEName(r))})
			}
			if failed == kitClaimGVR.Resource {
				expected = append(expected, objectIdentity{gvr: kitWarmPoolGVR, name: kitGKEWarmPoolName(kitGKEName(r))})
			}
			deleted := []objectIdentity{}
			for _, action := range client.Actions() {
				if action.GetVerb() == "delete" {
					deletion, ok := action.(ktesting.DeleteAction)
					require.True(t, ok)
					require.NotEmpty(t, *deletion.GetDeleteOptions().Preconditions.UID)
					deleted = append(deleted, objectIdentity{gvr: action.GetResource(), name: deletion.GetName()})
				}
			}
			require.ElementsMatch(t, expected, deleted)
			for _, object := range expected {
				_, err := client.Resource(object.gvr).Namespace(k.config.GKE.Namespace).Get(t.Context(), object.name, metav1.GetOptions{})
				require.True(t, apierrors.IsNotFound(err), "created object must be absent: %s/%s", object.gvr, object.name)
			}
		})
	}
}

func TestKitGKEColdAdmissionRestartIsTerminal(t *testing.T) {
	t.Parallel()
	for _, container := range []string{"kit", "acp-tls"} {
		t.Run(container, func(t *testing.T) {
			t.Parallel()
			k, client, r, fences := kitGKEBackendFixture(t)
			client.PrependReactor("list", "pods", func(action ktesting.Action) (bool, runtime.Object, error) {
				pods, err := client.Tracker().List(gkePodGVR, gkePodGVR.GroupVersion().WithKind("Pod"), action.GetNamespace())
				require.NoError(t, err)
				list, ok := pods.(*unstructured.UnstructuredList)
				require.True(t, ok)
				statuses, _, err := unstructured.NestedSlice(list.Items[0].Object, "status", "containerStatuses")
				require.NoError(t, err)
				for _, raw := range statuses {
					status, ok := raw.(map[string]any)
					require.True(t, ok)
					if status["name"] == container {
						status["restartCount"] = int64(1)
						status["ready"] = false
					}
				}
				require.NoError(t, unstructured.SetNestedSlice(list.Items[0].Object, statuses, "status", "containerStatuses"))
				return true, list, nil
			})
			ctx := context.WithValue(t.Context(), kitAdmissionProgressKey{}, kitAdmissionProgressWriter(func(context.Context, []byte) error { return nil }))
			ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			_, err := k.Ensure(ctx, r)
			require.ErrorContains(t, err, "restarted outside admission")
			require.Equal(t, 1, *fences)
			require.NoError(t, ctx.Err(), "restart must fail immediately, not as a readiness timeout")
		})
	}
}

func TestKitGKEReapPublishesRetirementBeforeTeardown(t *testing.T) {
	t.Parallel()
	k, client, r, fences := kitGKEBackendFixture(t)
	result, err := k.Ensure(t.Context(), r)
	require.NoError(t, err)
	r.BackendMetadataJSON = result.BackendMetadataJSON
	client.PrependReactor("delete", "*", func(ktesting.Action) (bool, runtime.Object, error) {
		state, _, err := k.loadAuthority(t.Context(), r)
		require.NoError(t, err)
		require.Equal(t, "retired", state.Phase)
		require.Positive(t, *fences)
		return true, nil, fmt.Errorf("teardown interrupted")
	})
	require.ErrorContains(t, k.Reap(t.Context(), r), "teardown interrupted")
	state, _, err := k.loadAuthority(t.Context(), r)
	require.NoError(t, err)
	require.Equal(t, "retired", state.Phase)
	client.ReactionChain = client.ReactionChain[1:]
	require.NoError(t, k.Reap(t.Context(), r))
}
