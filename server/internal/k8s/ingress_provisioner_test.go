package k8s

import (
	"strings"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

const ingressTestNamespace = "gram-test"

func newIngressProvisioner(t *testing.T) (*IngressProvisioner, *fake.Clientset) {
	t.Helper()
	cs := fake.NewSimpleClientset()
	return &IngressProvisioner{
		clientset: cs,
		namespace: ingressTestNamespace,
		logger:    testenv.NewLogger(t),
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

	result, err := p.Setup(ctx, domain)
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
}

func TestIngressProvisioner_Setup_UpdateExisting(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	p, cs := newIngressProvisioner(t)
	domain := "update.example.com"

	_, err := p.Setup(ctx, domain)
	require.NoError(t, err)

	_, err = p.Setup(ctx, domain)
	require.NoError(t, err)

	expectedName, err := SanitizeDomainForK8sName(domain)
	require.NoError(t, err)

	ingresses, err := cs.NetworkingV1().Ingresses(ingressTestNamespace).List(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, ingresses.Items, 1)
	require.Equal(t, expectedName, ingresses.Items[0].Name)
}

func TestIngressProvisioner_Get_Found(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	p, _ := newIngressProvisioner(t)
	domain := "get.example.com"

	result, err := p.Setup(ctx, domain)
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

	result, err := p.Setup(ctx, domain)
	require.NoError(t, err)

	_, err = cs.CoreV1().Secrets(ingressTestNamespace).Create(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      result.SecretName,
			Namespace: ingressTestNamespace,
		},
	}, metav1.CreateOptions{})
	require.NoError(t, err)

	require.NoError(t, p.Delete(ctx, result.ResourceName, result.SecretName))

	_, err = cs.NetworkingV1().Ingresses(ingressTestNamespace).Get(ctx, result.ResourceName, metav1.GetOptions{})
	require.True(t, k8serrors.IsNotFound(err))

	_, err = cs.CoreV1().Secrets(ingressTestNamespace).Get(ctx, result.SecretName, metav1.GetOptions{})
	require.True(t, k8serrors.IsNotFound(err))
}
