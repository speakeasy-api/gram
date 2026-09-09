package k8s

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	rbacv1 "k8s.io/api/rbac/v1"
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
	client := newOrphanInventoryClient()
	createInventoryObject(t, client, namespaceObject(desired.Resources))
	createInventoryObject(t, client, tailnetObject(desired))
	createInventoryObject(t, client, ownedInventoryObject(
		schema.GroupVersionResource{Group: "", Version: "v1", Resource: "secrets"},
		"Secret", desired.Resources.Namespace, desired.Resources.AttestorCASecret, desired.ID,
	))

	orphanID := uuid.New()
	orphanNames, err := NewNetworkIngressResourceNames(orphanID)
	require.NoError(t, err)
	createInventoryObject(t, client, ownedInventoryObject(tailnetGVR, "Tailnet", "", orphanNames.Tailnet, orphanID))

	client.ClearActions()
	client.PrependReactor("delete", "*", func(ktesting.Action) (bool, runtime.Object, error) {
		t.Fatal("orphan scan must not delete")
		return true, nil, nil
	})
	provisioner.dynamic = client
	orphans, err := provisioner.FindOrphans(t.Context(), []NetworkIngressResourceNames{desired.Resources})
	require.NoError(t, err)
	require.Equal(t, []NetworkIngressOrphan{{OwnerID: orphanID, Kind: "tailnets"}}, orphans)
	assertNamespacedInventoryIsScoped(t, client.Actions(), desired.Resources.Namespace, provisioner.config.OperatorNamespace)
	for _, action := range client.Actions() {
		require.Equal(t, "list", action.GetVerb())
	}
	_, err = client.Resource(tailnetGVR).Get(context.WithoutCancel(t.Context()), orphanNames.Tailnet, metav1.GetOptions{})
	require.NoError(t, err)
}

func TestNetworkIngressOrphanScanInventoriesOwnedNamespaceWithoutDBRow(t *testing.T) {
	t.Parallel()
	provisioner, typed, _, _ := newTestTailscaleProvisioner(t)
	client := newOrphanInventoryClient()

	orphanID := uuid.New()
	orphanNames, err := NewNetworkIngressResourceNames(orphanID)
	require.NoError(t, err)
	createInventoryObject(t, client, ownedInventoryObject(
		schema.GroupVersionResource{Group: "", Version: "v1", Resource: "namespaces"},
		"Namespace", "", orphanNames.Namespace, orphanID,
	))
	createInventoryObject(t, client, ownedInventoryObject(
		schema.GroupVersionResource{Group: "", Version: "v1", Resource: "secrets"},
		"Secret", orphanNames.Namespace, orphanNames.AttestorCASecret, orphanID,
	))
	_, err = typed.RbacV1().RoleBindings(orphanNames.Namespace).Create(t.Context(), &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: networkIngressAttestorManagerBinding, Namespace: orphanNames.Namespace, Labels: resourceOwnerLabels(orphanID.String())},
		RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: networkIngressAttestorManagerRole},
		Subjects:   []rbacv1.Subject{{Kind: rbacv1.ServiceAccountKind, Name: networkIngressWorkerServiceAccount, Namespace: provisioner.config.WorkerNamespace}},
	}, metav1.CreateOptions{})
	require.NoError(t, err)

	client.ClearActions()
	provisioner.dynamic = client
	orphans, err := provisioner.FindOrphans(t.Context(), nil)
	require.NoError(t, err)
	require.ElementsMatch(t, []NetworkIngressOrphan{
		{OwnerID: orphanID, Kind: "namespaces"},
		{OwnerID: orphanID, Kind: "secrets"},
		{OwnerID: orphanID, Kind: "rolebindings"},
	}, orphans)
	assertNamespacedInventoryIsScoped(t, client.Actions(), orphanNames.Namespace, provisioner.config.OperatorNamespace)
}

func newOrphanInventoryClient() *fake.FakeDynamicClient {
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
	return fake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), listKinds)
}

func namespaceObject(names NetworkIngressResourceNames) *unstructured.Unstructured {
	return ownedInventoryObject(
		schema.GroupVersionResource{Group: "", Version: "v1", Resource: "namespaces"},
		"Namespace", "", names.Namespace, names.OwnerID,
	)
}

func ownedInventoryObject(gvr schema.GroupVersionResource, kind, namespace, name string, ownerID uuid.UUID) *unstructured.Unstructured {
	apiVersion := gvr.Version
	if gvr.Group != "" {
		apiVersion = gvr.Group + "/" + gvr.Version
	}
	metadata := map[string]any{
		"name": name,
		"labels": map[string]any{
			managedByLabelKey:     networkIngressManagedBy,
			networkIngressIDLabel: ownerID.String(),
		},
	}
	if namespace != "" {
		metadata["namespace"] = namespace
	}
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": apiVersion,
		"kind":       kind,
		"metadata":   metadata,
	}}
}

func createInventoryObject(t *testing.T, client *fake.FakeDynamicClient, object *unstructured.Unstructured) {
	t.Helper()
	gvr, _ := metaResourceForObject(object)
	resource := client.Resource(gvr)
	if object.GetNamespace() != "" {
		_, err := resource.Namespace(object.GetNamespace()).Create(t.Context(), object, metav1.CreateOptions{})
		require.NoError(t, err)
		return
	}
	_, err := resource.Create(t.Context(), object, metav1.CreateOptions{})
	require.NoError(t, err)
}

func metaResourceForObject(object *unstructured.Unstructured) (schema.GroupVersionResource, schema.GroupVersionKind) {
	gv, err := schema.ParseGroupVersion(object.GetAPIVersion())
	if err != nil {
		panic(err)
	}
	resource := map[string]string{
		"Namespace": "namespaces",
		"Tailnet":   "tailnets",
		"Secret":    "secrets",
	}[object.GetKind()]
	return gv.WithResource(resource), gv.WithKind(object.GetKind())
}

func assertNamespacedInventoryIsScoped(t *testing.T, actions []ktesting.Action, wantNamespace, operatorNamespace string) {
	t.Helper()
	namespacedResources := map[string]bool{
		"proxygrouppolicies": true,
		"deployments":        true,
		"services":           true,
		"serviceaccounts":    true,
		"secrets":            true,
		"ingresses":          true,
		"networkpolicies":    true,
	}
	var secretLists int
	for _, action := range actions {
		if action.GetVerb() != "list" || !namespacedResources[action.GetResource().Resource] {
			continue
		}
		require.NotEmpty(t, action.GetNamespace(), "namespaced inventory must never list across all namespaces")
		if action.GetResource().Resource != "networkpolicies" || action.GetNamespace() != operatorNamespace {
			require.Equal(t, wantNamespace, action.GetNamespace())
		}
		if action.GetResource().Resource != "secrets" {
			continue
		}
		secretLists++
		require.NotEqual(t, operatorNamespace, action.GetNamespace(), "operator Secrets must not be inventoried")
		listAction, ok := action.(ktesting.ListAction)
		require.True(t, ok)
		require.NotEmpty(t, listAction.GetListRestrictions().Fields.String(), "Secret inventory must select the deterministic CA Secret name")
	}
	require.Equal(t, 1, secretLists)
}
