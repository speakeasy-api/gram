package k8s

import (
	"strings"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

const ingressTestNamespace = "gram-test"

func newIngressProvisioner(t *testing.T) (*IngressProvisioner, *fake.Clientset) {
	t.Helper()
	cs := fake.NewSimpleClientset()
	return &IngressProvisioner{
		clientset:      cs,
		namespace:      ingressTestNamespace,
		backendService: "gram-server",
		logger:         testenv.NewLogger(t),
	}, cs
}

func TestIngressProvisioner_Kind(t *testing.T) {
	t.Parallel()
	p, _ := newIngressProvisioner(t)
	require.Equal(t, ProvisionerKindIngress, p.Kind())
}

func TestIngressProvisioner_Setup_CreateNew(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	p, cs := newIngressProvisioner(t)
	domain := "test.example.com"

	result, err := p.Apply(ctx, RouteConfig{Domain: domain, IPAllowlist: nil, RootTarget: nil})
	require.NoError(t, err)

	expectedName, err := SanitizeDomainForK8sName(domain)
	require.NoError(t, err)
	expectedSecret := strings.ReplaceAll(domain, ".", "-") + "-tls"

	require.Equal(t, expectedName, result.ResourceName)
	require.Equal(t, expectedSecret, result.SecretName)

	ingress, err := cs.NetworkingV1().Ingresses(ingressTestNamespace).Get(ctx, expectedName, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, ingressTestNamespace, ingress.Namespace)
	require.Equal(t, expectedName, ingress.Name)
	require.Equal(t, domain, ingress.Labels["custom-domain"])
	require.Len(t, ingress.Spec.TLS, 1)
	require.Equal(t, expectedSecret, ingress.Spec.TLS[0].SecretName)
	require.Contains(t, ingress.Spec.Rules[0].HTTP.Paths, networkingv1.HTTPIngressPath{
		Path:     "/.well-known/openai-apps-challenge",
		PathType: new(networkingv1.PathTypeExact),
		Backend: networkingv1.IngressBackend{
			Service: &networkingv1.IngressServiceBackend{
				Name: "gram-server",
				Port: networkingv1.ServiceBackendPort{Number: 80},
			},
			Resource: nil,
		},
	})
	pathTypePrefix := networkingv1.PathTypePrefix
	require.Contains(t, ingress.Spec.Rules[0].HTTP.Paths, networkingv1.HTTPIngressPath{
		Path:     "/shared/skills",
		PathType: &pathTypePrefix,
		Backend: networkingv1.IngressBackend{
			Service: &networkingv1.IngressServiceBackend{
				Name: "gram-server",
				Port: networkingv1.ServiceBackendPort{Number: 80},
			},
			Resource: nil,
		},
	})
}

func TestIngressProvisioner_Setup_WithAllowlist_SetsAnnotation(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	p, cs := newIngressProvisioner(t)
	domain := "allow.example.com"

	result, err := p.Apply(ctx, RouteConfig{Domain: domain, IPAllowlist: []string{"1.2.3.4", "10.0.0.0/8"}, RootTarget: nil})
	require.NoError(t, err)

	ingress, err := cs.NetworkingV1().Ingresses(ingressTestNamespace).Get(ctx, result.ResourceName, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, "1.2.3.4,10.0.0.0/8", ingress.Annotations["nginx.ingress.kubernetes.io/whitelist-source-range"])
}

func TestIngressProvisioner_Setup_EmptyAllowlist_RemovesAnnotation(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	p, cs := newIngressProvisioner(t)
	domain := "clear.example.com"

	// Provision with a restriction, then re-apply with an empty allowlist.
	_, err := p.Apply(ctx, RouteConfig{Domain: domain, IPAllowlist: []string{"1.2.3.4"}, RootTarget: nil})
	require.NoError(t, err)

	result, err := p.Apply(ctx, RouteConfig{Domain: domain, IPAllowlist: nil, RootTarget: nil})
	require.NoError(t, err)

	ingress, err := cs.NetworkingV1().Ingresses(ingressTestNamespace).Get(ctx, result.ResourceName, metav1.GetOptions{})
	require.NoError(t, err)
	_, ok := ingress.Annotations["nginx.ingress.kubernetes.io/whitelist-source-range"]
	require.False(t, ok, "whitelist annotation must be removed when allowlist is empty")
}

func TestIngressProvisioner_Setup_UpdateExisting(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	p, cs := newIngressProvisioner(t)
	domain := "update.example.com"

	_, err := p.Apply(ctx, RouteConfig{Domain: domain, IPAllowlist: nil, RootTarget: nil})
	require.NoError(t, err)

	_, err = p.Apply(ctx, RouteConfig{Domain: domain, IPAllowlist: nil, RootTarget: nil})
	require.NoError(t, err)

	expectedName, err := SanitizeDomainForK8sName(domain)
	require.NoError(t, err)

	ingresses, err := cs.NetworkingV1().Ingresses(ingressTestNamespace).List(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, ingresses.Items, 1)
	require.Equal(t, expectedName, ingresses.Items[0].Name)
}

func TestIngressProvisioner_Apply_RootIngressLifecycle(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	p, cs := newIngressProvisioner(t)
	domain := "root.example.com"
	target := "/mcp/default"

	result, err := p.Apply(ctx, RouteConfig{
		Domain:      domain,
		IPAllowlist: []string{"1.2.3.4"},
		RootTarget:  &target,
	})
	require.NoError(t, err)

	rootName, err := RootIngressName(result.ResourceName)
	require.NoError(t, err)
	rootIngress, err := cs.NetworkingV1().Ingresses(ingressTestNamespace).Get(ctx, rootName, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, customDomainRoleRoot, rootIngress.Labels[customDomainRoleKey])
	require.Equal(t, target, rootIngress.Annotations["nginx.ingress.kubernetes.io/rewrite-target"])
	require.Equal(t, "true", rootIngress.Annotations["nginx.ingress.kubernetes.io/use-regex"])
	require.Equal(t, "15m", rootIngress.Annotations["nginx.ingress.kubernetes.io/proxy-body-size"])
	require.Equal(t, "1.2.3.4", rootIngress.Annotations["nginx.ingress.kubernetes.io/whitelist-source-range"])
	require.NotContains(t, rootIngress.Annotations, "cert-manager.io/cluster-issuer")
	require.Len(t, rootIngress.Spec.Rules, 1)
	require.Len(t, rootIngress.Spec.Rules[0].HTTP.Paths, 1)
	require.Equal(t, "/$", rootIngress.Spec.Rules[0].HTTP.Paths[0].Path)
	require.Equal(t, networkingv1.PathTypeImplementationSpecific, *rootIngress.Spec.Rules[0].HTTP.Paths[0].PathType)
	require.Equal(t, result.SecretName, rootIngress.Spec.TLS[0].SecretName)

	wellKnownRootName, err := WellKnownRootIngressName(result.ResourceName)
	require.NoError(t, err)
	wellKnownRootIngress, err := cs.NetworkingV1().Ingresses(ingressTestNamespace).Get(ctx, wellKnownRootName, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, customDomainRoleWellKnownRoot, wellKnownRootIngress.Labels[customDomainRoleKey])
	require.Equal(t, "/.well-known/$1"+target, wellKnownRootIngress.Annotations["nginx.ingress.kubernetes.io/rewrite-target"])
	require.Equal(t, "true", wellKnownRootIngress.Annotations["nginx.ingress.kubernetes.io/use-regex"])
	require.Equal(t, "15m", wellKnownRootIngress.Annotations["nginx.ingress.kubernetes.io/proxy-body-size"])
	require.Equal(t, "1.2.3.4", wellKnownRootIngress.Annotations["nginx.ingress.kubernetes.io/whitelist-source-range"])
	require.NotContains(t, wellKnownRootIngress.Annotations, "cert-manager.io/cluster-issuer")
	require.Len(t, wellKnownRootIngress.Spec.Rules, 1)
	require.Len(t, wellKnownRootIngress.Spec.Rules[0].HTTP.Paths, 1)
	require.Equal(t, `/\.well-known/(oauth-protected-resource|oauth-authorization-server)$`, wellKnownRootIngress.Spec.Rules[0].HTTP.Paths[0].Path)
	require.Equal(t, networkingv1.PathTypeImplementationSpecific, *wellKnownRootIngress.Spec.Rules[0].HTTP.Paths[0].PathType)
	require.Equal(t, result.SecretName, wellKnownRootIngress.Spec.TLS[0].SecretName)

	renamedTarget := "/mcp/renamed"
	_, err = p.Apply(ctx, RouteConfig{Domain: domain, IPAllowlist: nil, RootTarget: &renamedTarget})
	require.NoError(t, err)
	rootIngress, err = cs.NetworkingV1().Ingresses(ingressTestNamespace).Get(ctx, rootName, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, renamedTarget, rootIngress.Annotations["nginx.ingress.kubernetes.io/rewrite-target"])
	require.NotContains(t, rootIngress.Annotations, "nginx.ingress.kubernetes.io/whitelist-source-range")
	wellKnownRootIngress, err = cs.NetworkingV1().Ingresses(ingressTestNamespace).Get(ctx, wellKnownRootName, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, "/.well-known/$1"+renamedTarget, wellKnownRootIngress.Annotations["nginx.ingress.kubernetes.io/rewrite-target"])
	require.NotContains(t, wellKnownRootIngress.Annotations, "nginx.ingress.kubernetes.io/whitelist-source-range")

	_, err = p.Apply(ctx, RouteConfig{Domain: domain, IPAllowlist: nil, RootTarget: nil})
	require.NoError(t, err)
	_, err = cs.NetworkingV1().Ingresses(ingressTestNamespace).Get(ctx, rootName, metav1.GetOptions{})
	require.True(t, k8serrors.IsNotFound(err))
	_, err = cs.NetworkingV1().Ingresses(ingressTestNamespace).Get(ctx, wellKnownRootName, metav1.GetOptions{})
	require.True(t, k8serrors.IsNotFound(err))
	_, err = cs.NetworkingV1().Ingresses(ingressTestNamespace).Get(ctx, result.ResourceName, metav1.GetOptions{})
	require.NoError(t, err)
}

func TestIngressProvisioner_Get_Found(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	p, _ := newIngressProvisioner(t)
	domain := "get.example.com"

	result, err := p.Apply(ctx, RouteConfig{Domain: domain, IPAllowlist: nil, RootTarget: nil})
	require.NoError(t, err)

	require.NoError(t, p.Get(ctx, result.ResourceName))
}

func TestIngressProvisioner_Get_NotFound(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	p, _ := newIngressProvisioner(t)

	err := p.Get(ctx, "nonexistent-ingress")
	require.Error(t, err)
}

func TestIngressProvisioner_Delete(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	p, cs := newIngressProvisioner(t)
	domain := "delete.example.com"
	target := "/mcp/default"

	result, err := p.Apply(ctx, RouteConfig{Domain: domain, IPAllowlist: nil, RootTarget: &target})
	require.NoError(t, err)

	_, err = cs.CoreV1().Secrets(ingressTestNamespace).Create(ctx, &corev1.Secret{
		Name:      result.SecretName,
		Namespace: ingressTestNamespace,
	}, metav1.CreateOptions{})
	require.NoError(t, err)

	require.NoError(t, p.Delete(ctx, result.ResourceName, result.SecretName))
	require.NoError(t, p.Delete(ctx, result.ResourceName, result.SecretName))

	_, err = cs.NetworkingV1().Ingresses(ingressTestNamespace).Get(ctx, result.ResourceName, metav1.GetOptions{})
	require.True(t, k8serrors.IsNotFound(err))
	rootName, err := RootIngressName(result.ResourceName)
	require.NoError(t, err)
	_, err = cs.NetworkingV1().Ingresses(ingressTestNamespace).Get(ctx, rootName, metav1.GetOptions{})
	require.True(t, k8serrors.IsNotFound(err))
	wellKnownRootName, err := WellKnownRootIngressName(result.ResourceName)
	require.NoError(t, err)
	_, err = cs.NetworkingV1().Ingresses(ingressTestNamespace).Get(ctx, wellKnownRootName, metav1.GetOptions{})
	require.True(t, k8serrors.IsNotFound(err))

	_, err = cs.CoreV1().Secrets(ingressTestNamespace).Get(ctx, result.SecretName, metav1.GetOptions{})
	require.True(t, k8serrors.IsNotFound(err))
}
func TestIngressProvisioner_Delete_MissingResourcesIsNoop(t *testing.T) {
	t.Parallel()
	p, _ := newIngressProvisioner(t)

	// Models a retry after the previous attempt already deleted both resources.
	require.NoError(t, p.Delete(t.Context(), "never-created", "never-created-tls"))
}

func TestIngressProvisioner_Delete_EmptySecretNameSkipsSecret(t *testing.T) {
	t.Parallel()
	p, cs := newIngressProvisioner(t)

	result, err := p.Apply(t.Context(), RouteConfig{Domain: "nosecret.example.com", IPAllowlist: nil, RootTarget: nil})
	require.NoError(t, err)

	require.NoError(t, p.Delete(t.Context(), result.ResourceName, ""))

	_, err = cs.NetworkingV1().Ingresses(ingressTestNamespace).Get(t.Context(), result.ResourceName, metav1.GetOptions{})
	require.True(t, k8serrors.IsNotFound(err))
}
