package k8s

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/fake"
	ktesting "k8s.io/client-go/testing"
)

func TestNetworkIngressOrphanScanReportsWithoutDeleting(t *testing.T) {
	t.Parallel()
	provisioner, _, _, desired := newTestTailscaleProvisioner(t)
	listKinds := map[schema.GroupVersionResource]string{
		tailnetGVR: "TailnetList", proxyGroupGVR: "ProxyGroupList", proxyGroupPolicyGVR: "ProxyGroupPolicyList",
		{Group: "", Version: "v1", Resource: "namespaces"}:                       "NamespaceList",
		{Group: "", Version: "v1", Resource: "services"}:                         "ServiceList",
		{Group: "", Version: "v1", Resource: "serviceaccounts"}:                  "ServiceAccountList",
		{Group: "", Version: "v1", Resource: "secrets"}:                          "SecretList",
		{Group: "apps", Version: "v1", Resource: "deployments"}:                  "DeploymentList",
		{Group: "networking.k8s.io", Version: "v1", Resource: "ingresses"}:       "IngressList",
		{Group: "networking.k8s.io", Version: "v1", Resource: "networkpolicies"}: "NetworkPolicyList",
	}
	client := fake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), listKinds)
	known := tailnetObject(desired)
	_, err := client.Resource(tailnetGVR).Create(t.Context(), known, metav1.CreateOptions{})
	require.NoError(t, err)
	orphanID := uuid.New()
	orphan := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "tailscale.com/v1alpha1", "kind": "Tailnet", "metadata": map[string]any{"name": "unmatched", "labels": map[string]any{managedByLabelKey: networkIngressManagedBy, networkIngressIDLabel: orphanID.String()}}}}
	_, err = client.Resource(tailnetGVR).Create(t.Context(), orphan, metav1.CreateOptions{})
	require.NoError(t, err)
	client.ClearActions()
	client.PrependReactor("delete", "*", func(ktesting.Action) (bool, runtime.Object, error) {
		t.Fatal("orphan scan must not delete")
		return true, nil, nil
	})
	provisioner.dynamic = client
	orphans, err := provisioner.FindOrphans(t.Context(), []NetworkIngressResourceNames{desired.Resources})
	require.NoError(t, err)
	require.Equal(t, []NetworkIngressOrphan{{OwnerID: orphanID, Kind: "tailnets"}}, orphans)
	for _, action := range client.Actions() {
		require.Equal(t, "list", action.GetVerb())
	}
	_, err = client.Resource(tailnetGVR).Get(context.WithoutCancel(t.Context()), "unmatched", metav1.GetOptions{})
	require.NoError(t, err)
}
