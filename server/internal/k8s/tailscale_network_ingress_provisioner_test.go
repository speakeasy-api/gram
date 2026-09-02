package k8s

import (
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func TestTailscaleNetworkIngressProvisionerApplyObserveAndDelete(t *testing.T) {
	t.Parallel()

	provisioner, typed, dynamicClient, desired := newTestTailscaleProvisioner(t)
	first, err := provisioner.Apply(t.Context(), desired)
	require.NoError(t, err)
	require.Equal(t, NetworkIngressStatusPending, first.Status)
	second, err := provisioner.Apply(t.Context(), desired)
	require.NoError(t, err)
	require.Equal(t, NetworkIngressStatusPending, second.Status)

	secret, err := typed.CoreV1().Secrets("tailscale").Get(t.Context(), desired.Resources.CredentialsSecret, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, []byte("test-client"), secret.Data["client_id"])
	require.Equal(t, []byte("test-secret"), secret.Data["client_secret"])
	tailnet, err := dynamicClient.Resource(tailnetGVR).Get(t.Context(), desired.Resources.Tailnet, metav1.GetOptions{})
	require.NoError(t, err)
	secretName, found, err := unstructured.NestedString(tailnet.Object, "spec", "credentials", "secretName")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, desired.Resources.CredentialsSecret, secretName)
	proxyGroup, err := dynamicClient.Resource(proxyGroupGVR).Get(t.Context(), desired.Resources.ProxyGroup, metav1.GetOptions{})
	require.NoError(t, err)
	tailnetName, found, err := unstructured.NestedString(proxyGroup.Object, "spec", "tailnet")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, desired.Resources.Tailnet, tailnetName)
	policy, err := dynamicClient.Resource(proxyGroupPolicyGVR).Namespace(desired.Resources.Namespace).Get(t.Context(), desired.Resources.ProxyGroupPolicy, metav1.GetOptions{})
	require.NoError(t, err)
	allowedGroups, found, err := unstructured.NestedSlice(policy.Object, "spec", "ingress")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, []any{desired.Resources.ProxyGroup}, allowedGroups)

	deployment, err := typed.AppsV1().Deployments(desired.Resources.Namespace).Get(t.Context(), desired.Resources.AttestorDeployment, metav1.GetOptions{})
	require.NoError(t, err)
	require.False(t, *deployment.Spec.Template.Spec.AutomountServiceAccountToken)
	require.Equal(t, networkIngressTokenAudience, deployment.Spec.Template.Spec.Volumes[0].Projected.Sources[0].ServiceAccountToken.Audience)
	require.Equal(t, int64(600), *deployment.Spec.Template.Spec.Volumes[0].Projected.Sources[0].ServiceAccountToken.ExpirationSeconds)
	caSecret, err := typed.CoreV1().Secrets(desired.Resources.Namespace).Get(t.Context(), desired.Resources.AttestorCASecret, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, []byte("test-ca"), caSecret.Data["ca.crt"])

	attestorPolicy, err := typed.NetworkingV1().NetworkPolicies(desired.Resources.Namespace).Get(t.Context(), desired.Resources.AttestorNetworkPolicy, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, desired.Resources.ProxyGroup, attestorPolicy.Spec.Ingress[0].From[0].PodSelector.MatchLabels[tailscaleParentResource])
	proxyPolicy, err := typed.NetworkingV1().NetworkPolicies("tailscale").Get(t.Context(), desired.Resources.ProxyNetworkPolicy, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, desired.Resources.ProxyGroup, proxyPolicy.Spec.PodSelector.MatchLabels[tailscaleParentResource])

	setUnstructuredReady(t, dynamicClient, tailnetGVR, "", desired.Resources.Tailnet, "TailnetReady")
	setUnstructuredReady(t, dynamicClient, proxyGroupGVR, "", desired.Resources.ProxyGroup, "ProxyGroupReady")
	deployment.Generation = 2
	deployment.Status.ObservedGeneration = 2
	deployment.Status.UpdatedReplicas = 1
	deployment.Status.ReadyReplicas = 1
	deployment.Status.AvailableReplicas = 1
	_, err = typed.AppsV1().Deployments(desired.Resources.Namespace).UpdateStatus(t.Context(), deployment, metav1.UpdateOptions{})
	require.NoError(t, err)
	ingress, err := typed.NetworkingV1().Ingresses(desired.Resources.Namespace).Get(t.Context(), desired.Resources.Ingress, metav1.GetOptions{})
	require.NoError(t, err)
	ingress.Status.LoadBalancer.Ingress = []networkingv1.IngressLoadBalancerIngress{{Hostname: "Private-Test.Example.TS.NET."}}
	_, err = typed.NetworkingV1().Ingresses(desired.Resources.Namespace).UpdateStatus(t.Context(), ingress, metav1.UpdateOptions{})
	require.NoError(t, err)

	_, err = provisioner.Apply(t.Context(), desired)
	require.NoError(t, err)
	deployment, err = typed.AppsV1().Deployments(desired.Resources.Namespace).Get(t.Context(), desired.Resources.AttestorDeployment, metav1.GetOptions{})
	require.NoError(t, err)
	deployment.Status.ObservedGeneration = deployment.Generation
	deployment.Status.UpdatedReplicas = 1
	deployment.Status.ReadyReplicas = 1
	deployment.Status.AvailableReplicas = 1
	_, err = typed.AppsV1().Deployments(desired.Resources.Namespace).UpdateStatus(t.Context(), deployment, metav1.UpdateOptions{})
	require.NoError(t, err)
	observation, err := provisioner.Observe(t.Context(), desired.Resources)
	require.NoError(t, err)
	require.Equal(t, NetworkIngressStatusOnline, observation.Status)
	require.Equal(t, "private-test.example.ts.net", observation.DNSName)

	deployment.Status.ObservedGeneration--
	_, err = typed.AppsV1().Deployments(desired.Resources.Namespace).UpdateStatus(t.Context(), deployment, metav1.UpdateOptions{})
	require.NoError(t, err)
	pending, err := provisioner.observe(t.Context(), desired.Resources, desired.Hostname)
	require.NoError(t, err)
	require.Equal(t, NetworkIngressStatusPending, pending.Status)
	deployment.Status.ObservedGeneration = deployment.Generation
	_, err = typed.AppsV1().Deployments(desired.Resources.Namespace).UpdateStatus(t.Context(), deployment, metav1.UpdateOptions{})
	require.NoError(t, err)
	pending, err = provisioner.observe(t.Context(), desired.Resources, "renamed-private-test")
	require.NoError(t, err)
	require.Equal(t, NetworkIngressStatusPending, pending.Status)

	var mu sync.Mutex
	var deletes []string
	var missingPrecondition bool
	recordDelete := func(action ktesting.Action) (bool, runtime.Object, error) {
		if action.GetVerb() == "delete" {
			deleteAction, ok := action.(ktesting.DeleteAction)
			mu.Lock()
			if !ok || deleteAction.GetDeleteOptions().Preconditions == nil || deleteAction.GetDeleteOptions().Preconditions.UID == nil {
				missingPrecondition = true
			}
			deletes = append(deletes, action.GetResource().Resource)
			mu.Unlock()
		}
		return false, nil, nil
	}
	typed.PrependReactor("delete", "*", recordDelete)
	dynamicClient.PrependReactor("delete", "*", recordDelete)
	require.NoError(t, provisioner.Delete(t.Context(), desired.Resources))
	require.NoError(t, provisioner.Delete(t.Context(), desired.Resources))
	require.False(t, missingPrecondition)
	require.GreaterOrEqual(t, len(deletes), 12)
	require.Equal(t, []string{"ingresses", "services", "deployments", "proxygrouppolicies", "proxygroups", "tailnets", "secrets", "secrets", "serviceaccounts", "networkpolicies", "networkpolicies", "namespaces"}, deletes[:12])
}

func TestTailscaleNetworkIngressProvisionerPreservesDynamicMetadata(t *testing.T) {
	t.Parallel()

	provisioner, _, dynamicClient, desired := newTestTailscaleProvisioner(t)
	tailnet := tailnetObject(desired)
	tailnet.SetUID(types.UID("tailnet-uid"))
	tailnet.SetFinalizers([]string{"tailscale.com/finalizer"})
	tailnet.SetAnnotations(map[string]string{"tailscale.com/controller-state": "preserve"})
	_, err := dynamicClient.Resource(tailnetGVR).Create(t.Context(), tailnet, metav1.CreateOptions{})
	require.NoError(t, err)

	require.NoError(t, provisioner.applyDynamic(t.Context(), tailnetGVR, "", tailnetObject(desired)))
	got, err := dynamicClient.Resource(tailnetGVR).Get(t.Context(), desired.Resources.Tailnet, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, types.UID("tailnet-uid"), got.GetUID())
	require.Equal(t, []string{"tailscale.com/finalizer"}, got.GetFinalizers())
	require.Equal(t, "preserve", got.GetAnnotations()["tailscale.com/controller-state"])
}

func TestTailscaleNetworkIngressProvisionerRefusesUnownedResources(t *testing.T) {
	t.Parallel()

	provisioner, typed, _, desired := newTestTailscaleProvisioner(t)
	_, err := typed.CoreV1().Namespaces().Create(t.Context(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: desired.Resources.Namespace}}, metav1.CreateOptions{})
	require.NoError(t, err)

	observation, err := provisioner.Apply(t.Context(), desired)
	require.Error(t, err)
	require.Equal(t, NetworkIngressStatusError, observation.Status)
	require.Contains(t, err.Error(), "refuse to adopt namespace")

	err = provisioner.Delete(t.Context(), desired.Resources)
	require.Error(t, err)
	require.Contains(t, err.Error(), "refuse to delete unowned namespace")
	_, err = typed.CoreV1().Namespaces().Get(t.Context(), desired.Resources.Namespace, metav1.GetOptions{})
	require.NoError(t, err)
}

func TestTailscaleNetworkIngressProvisionerPartialFailureAndRedaction(t *testing.T) {
	t.Parallel()

	provisioner, typed, dynamicClient, desired := newTestTailscaleProvisioner(t)
	typed.PrependReactor("create", "deployments", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("injected deployment failure")
	})
	observation, err := provisioner.Apply(t.Context(), desired)
	require.Error(t, err)
	require.Equal(t, NetworkIngressStatusError, observation.Status)
	require.Equal(t, NetworkIngressErrorKubernetes, observation.ErrorCode)
	require.NotContains(t, err.Error(), "test-secret")
	_, err = dynamicClient.Resource(proxyGroupGVR).Get(t.Context(), desired.Resources.ProxyGroup, metav1.GetOptions{})
	require.NoError(t, err)
	_, err = typed.CoreV1().Services(desired.Resources.Namespace).Get(t.Context(), desired.Resources.AttestorService, metav1.GetOptions{})
	require.Error(t, err)

	desired.Credentials = []byte(`{"client_id":"sensitive-client","unexpected":"sensitive-secret"}`)
	observation, err = provisioner.Apply(t.Context(), desired)
	require.Error(t, err)
	require.Equal(t, NetworkIngressErrorInvalidCredentials, observation.ErrorCode)
	require.NotContains(t, err.Error(), "sensitive-client")
	require.NotContains(t, err.Error(), "sensitive-secret")

	desired.Credentials = []byte(`{"client_id":"sensitive-client","client_secret":"sensitive-secret"} garbage`)
	observation, err = provisioner.Apply(t.Context(), desired)
	require.Error(t, err)
	require.Equal(t, NetworkIngressErrorInvalidCredentials, observation.ErrorCode)
	require.NotContains(t, err.Error(), "sensitive-client")
	require.NotContains(t, err.Error(), "sensitive-secret")
}

func TestTailscaleNetworkIngressProvisionerReplacesImmutableProxyGroup(t *testing.T) {
	t.Parallel()

	provisioner, typed, dynamicClient, desired := newTestTailscaleProvisioner(t)
	wrong := proxyGroupObject(desired, "tag:wrong-proxy")
	_, err := dynamicClient.Resource(proxyGroupGVR).Create(t.Context(), wrong, metav1.CreateOptions{})
	require.NoError(t, err)
	_, err = typed.NetworkingV1().Ingresses(desired.Resources.Namespace).Create(t.Context(), &networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{Name: desired.Resources.Ingress, Namespace: desired.Resources.Namespace, Labels: ingressLabels(desired)}}, metav1.CreateOptions{})
	require.NoError(t, err)

	var deleted []string
	typed.PrependReactor("delete", "ingresses", func(action ktesting.Action) (bool, runtime.Object, error) {
		deleted = append(deleted, "ingress")
		return false, nil, nil
	})
	dynamicClient.PrependReactor("delete", "proxygroups", func(action ktesting.Action) (bool, runtime.Object, error) {
		deleted = append(deleted, "proxygroup")
		return false, nil, nil
	})
	_, err = provisioner.Apply(t.Context(), desired)
	require.ErrorIs(t, err, ErrNetworkIngressReplacementPending)
	_, err = provisioner.Apply(t.Context(), desired)
	require.NoError(t, err)
	require.Equal(t, []string{"ingress", "proxygroup"}, deleted)
	got, err := dynamicClient.Resource(proxyGroupGVR).Get(t.Context(), desired.Resources.ProxyGroup, metav1.GetOptions{})
	require.NoError(t, err)
	tailnet, _, err := unstructured.NestedString(got.Object, "spec", "tailnet")
	require.NoError(t, err)
	require.Equal(t, desired.Resources.Tailnet, tailnet)
}

func newTestTailscaleProvisioner(t *testing.T) (*TailscaleNetworkIngressProvisioner, *fake.Clientset, *dynamicfake.FakeDynamicClient, NetworkIngressDesired) {
	t.Helper()
	id := uuid.MustParse("0199aabb-ccdd-7000-8000-001122334455")
	resources, err := NewNetworkIngressResourceNames(id)
	require.NoError(t, err)
	typed := fake.NewSimpleClientset(&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "private-listener-ca", Namespace: "gram-system"}, Data: map[string][]byte{"ca.crt": []byte("test-ca")}})
	scheme := runtime.NewScheme()
	dynamicClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, map[schema.GroupVersionResource]string{tailnetGVR: "TailnetList", proxyGroupGVR: "ProxyGroupList", proxyGroupPolicyGVR: "ProxyGroupPolicyList"})
	provisioner, err := NewTailscaleNetworkIngressProvisioner(typed, dynamicClient, TailscaleNetworkIngressConfig{
		OperatorNamespace: "tailscale",
		BackendNamespace:  "gram-system",
		BackendPodLabels:  map[string]string{"app": "gram-server"},
		ProxyTag:          "tag:test-proxy",
		ServiceTag:        "tag:test-service",
		AttestorCASecret:  "private-listener-ca",
		KubernetesAPICIDR: "10.96.0.1/32",
		KubernetesAPIPort: 443,
		ClusterCIDRs:      []string{"10.0.0.0/8", "192.168.0.0/16"},
	})
	require.NoError(t, err)
	return provisioner, typed, dynamicClient, NetworkIngressDesired{
		ID: id, Provider: NetworkIngressProviderTailscale, Hostname: "private-test", Credentials: []byte(`{"client_id":"test-client","client_secret":"test-secret"}`), Resources: resources,
		AttestorImage: "gram-attestor@example", BackendService: "gram-server-private", BackendPort: 8443,
	}
}

func setUnstructuredReady(t *testing.T, client *dynamicfake.FakeDynamicClient, gvr schema.GroupVersionResource, namespace, name, conditionType string) {
	t.Helper()
	var resource dynamic.ResourceInterface = client.Resource(gvr)
	if namespace != "" {
		resource = client.Resource(gvr).Namespace(namespace)
	}
	object, err := resource.Get(t.Context(), name, metav1.GetOptions{})
	require.NoError(t, err)
	require.NoError(t, unstructured.SetNestedSlice(object.Object, []any{map[string]any{"type": conditionType, "status": "True"}}, "status", "conditions"))
	_, err = resource.UpdateStatus(t.Context(), object, metav1.UpdateOptions{})
	require.NoError(t, err)
}
