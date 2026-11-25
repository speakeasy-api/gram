package assistants

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	ktesting "k8s.io/client-go/testing"
)

func TestKitGKEPoolAuthorityFencing(t *testing.T) {
	t.Parallel()
	for mutation, wantError := range map[string]string{
		"uid":      "authority mismatch",
		"replicas": "zero speculative replicas",
		"template": "template binding mismatch",
		"claim":    "claim warm pool binding mismatch",
	} {
		t.Run(mutation, func(t *testing.T) {
			t.Parallel()
			k, client, r, fences := kitGKEBackendFixture(t)
			result, err := k.Ensure(t.Context(), r)
			require.NoError(t, err)
			r.BackendMetadataJSON = result.BackendMetadataJSON
			m, err := k.metadata(r)
			require.NoError(t, err)
			resource, name := kitWarmPoolGVR, m.WarmPoolName
			if mutation == "claim" {
				resource, name = kitClaimGVR, kitGKEName(r)
			}
			object, err := client.Resource(resource).Namespace(m.Namespace).Get(t.Context(), name, metav1.GetOptions{})
			require.NoError(t, err)
			switch mutation {
			case "uid":
				object.SetUID("replacement-pool")
			case "replicas":
				require.NoError(t, unstructured.SetNestedField(object.Object, int64(1), "spec", "replicas"))
			case "template":
				require.NoError(t, unstructured.SetNestedField(object.Object, "another-template", "spec", "sandboxTemplateRef", "name"))
			case "claim":
				require.NoError(t, unstructured.SetNestedField(object.Object, "another-pool", "spec", "warmPoolRef", "name"))
			}
			_, err = client.Resource(resource).Namespace(m.Namespace).Update(t.Context(), object, metav1.UpdateOptions{})
			require.NoError(t, err)
			_, err = k.Ensure(t.Context(), r)
			require.ErrorContains(t, err, wantError)
			client.ClearActions()
			require.ErrorContains(t, k.Reap(t.Context(), r), wantError)
			require.Zero(t, *fences)
			for _, action := range client.Actions() {
				require.NotEqual(t, "delete", action.GetVerb())
			}
		})
	}
}

func TestKitGKEPoolCleanupUIDPreconditions(t *testing.T) {
	t.Parallel()
	k, client, r, _ := kitGKEBackendFixture(t)
	result, err := k.Ensure(t.Context(), r)
	require.NoError(t, err)
	r.BackendMetadataJSON = result.BackendMetadataJSON
	m, err := k.metadata(r)
	require.NoError(t, err)
	client.ClearActions()
	r.Retired = true
	require.NoError(t, k.Reap(t.Context(), r))
	deleted := []string{}
	for _, action := range client.Actions() {
		if action.GetVerb() != "delete" {
			continue
		}
		deleted = append(deleted, action.GetResource().Resource)
		if action.GetResource() == kitWarmPoolGVR {
			deletion, ok := action.(ktesting.DeleteAction)
			require.True(t, ok)
			require.Equal(t, m.WarmPoolName, deletion.GetName())
			require.Equal(t, m.WarmPoolUID, *deletion.GetDeleteOptions().Preconditions.UID)
		}
	}
	require.Contains(t, deleted, "sandboxwarmpools")
	poolIndex, templateIndex := -1, -1
	for i, resource := range deleted {
		if resource == "sandboxwarmpools" {
			poolIndex = i
		}
		if resource == "sandboxtemplates" {
			templateIndex = i
		}
	}
	require.Less(t, poolIndex, templateIndex)
}

func TestKitGKERestoreRejectsReplacedPool(t *testing.T) {
	t.Parallel()
	k, client, r, fences := kitGKEBackendFixture(t)
	k.config.Provision.SnapshotsEnabled = true
	k.config.Snapshots = &kitGKEFakeCheckpointDriver{client: client, restored: true}
	result, err := k.Ensure(t.Context(), r)
	require.NoError(t, err)
	r.BackendMetadataJSON = result.BackendMetadataJSON
	require.NoError(t, k.Stop(t.Context(), r))
	state, _, err := k.loadAuthority(t.Context(), r)
	require.NoError(t, err)
	pool, err := client.Resource(kitWarmPoolGVR).Namespace(k.config.GKE.Namespace).Get(t.Context(), state.Metadata.WarmPoolName, metav1.GetOptions{})
	require.NoError(t, err)
	pool.SetUID("replacement-pool")
	_, err = client.Resource(kitWarmPoolGVR).Namespace(k.config.GKE.Namespace).Update(t.Context(), pool, metav1.UpdateOptions{})
	require.NoError(t, err)
	client.ClearActions()
	_, err = k.Ensure(t.Context(), r)
	require.ErrorContains(t, err, "authority mismatch")
	require.Equal(t, 1, *fences)
	for _, action := range client.Actions() {
		require.Equal(t, "get", action.GetVerb())
	}
}
