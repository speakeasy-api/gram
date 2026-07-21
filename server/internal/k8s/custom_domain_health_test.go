package k8s

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/stretchr/testify/require"
)

func TestCheckCustomDomainInfrastructure(t *testing.T) {
	t.Parallel()

	const (
		namespace    = "gram-test"
		domain       = "mcp.example.com"
		resourceName = "mcp-example-com"
		secretName   = "mcp-example-com-tls"
	)

	t.Run("missing ingress is unhealthy", func(t *testing.T) {
		t.Parallel()

		clients := &KubernetesClients{
			Clientset:     fake.NewSimpleClientset(),
			DynamicClient: dynamicfake.NewSimpleDynamicClient(runtime.NewScheme()),
			namespace:     namespace,
			enabled:       true,
		}

		health, err := clients.CheckCustomDomainInfrastructure(t.Context(), CustomDomainInfrastructureCheck{
			Domain:          domain,
			ResourceName:    resourceName,
			CertSecretName:  secretName,
			ProvisionerKind: ProvisionerKindIngress,
		})

		require.NoError(t, err)
		require.Equal(t, CustomDomainInfrastructureIssueResourceMissing, health.Issue)
	})

	t.Run("ready certificate is healthy", func(t *testing.T) {
		t.Parallel()

		expiresAt := time.Now().UTC().Add(30 * 24 * time.Hour)
		clients := newCustomDomainHealthTestClients(t, namespace, domain, resourceName, secretName, expiresAt, true)

		health, err := clients.CheckCustomDomainInfrastructure(t.Context(), CustomDomainInfrastructureCheck{
			Domain:          domain,
			ResourceName:    resourceName,
			CertSecretName:  secretName,
			ProvisionerKind: ProvisionerKindIngress,
		})

		require.NoError(t, err)
		require.Empty(t, health.Issue)
		require.WithinDuration(t, expiresAt, *health.CertificateExpiresAt, time.Second)
	})

	t.Run("expired certificate is unhealthy", func(t *testing.T) {
		t.Parallel()

		expiresAt := time.Now().UTC().Add(-time.Hour)
		clients := newCustomDomainHealthTestClients(t, namespace, domain, resourceName, secretName, expiresAt, true)

		health, err := clients.CheckCustomDomainInfrastructure(t.Context(), CustomDomainInfrastructureCheck{
			Domain:          domain,
			ResourceName:    resourceName,
			CertSecretName:  secretName,
			ProvisionerKind: ProvisionerKindIngress,
		})

		require.NoError(t, err)
		require.Equal(t, CustomDomainInfrastructureIssueCertificateExpired, health.Issue)
		require.WithinDuration(t, expiresAt, *health.CertificateExpiresAt, time.Second)
	})

	t.Run("non-ready cert-manager resource is unhealthy", func(t *testing.T) {
		t.Parallel()

		expiresAt := time.Now().UTC().Add(30 * 24 * time.Hour)
		clients := newCustomDomainHealthTestClients(t, namespace, domain, resourceName, secretName, expiresAt, false)

		health, err := clients.CheckCustomDomainInfrastructure(t.Context(), CustomDomainInfrastructureCheck{
			Domain:          domain,
			ResourceName:    resourceName,
			CertSecretName:  secretName,
			ProvisionerKind: ProvisionerKindIngress,
		})

		require.NoError(t, err)
		require.Equal(t, CustomDomainInfrastructureIssueCertificateNotReady, health.Issue)
	})
}

func newCustomDomainHealthTestClients(t *testing.T, namespace, domain, resourceName, secretName string, expiresAt time.Time, ready bool) *KubernetesClients {
	t.Helper()

	certificatePEM := newCertificatePEM(t, domain, expiresAt)
	ingress := &networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{Name: resourceName, Namespace: namespace}}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: namespace},
		Data:       map[string][]byte{corev1.TLSCertKey: certificatePEM},
	}
	certificate := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "cert-manager.io/v1",
		"kind":       "Certificate",
		"metadata": map[string]any{
			"name":      secretName,
			"namespace": namespace,
		},
		"status": map[string]any{
			"conditions": []any{map[string]any{
				"type":   "Ready",
				"status": map[bool]string{true: "True", false: "False"}[ready],
			}},
		},
	}}

	return &KubernetesClients{
		Clientset:     fake.NewSimpleClientset(ingress, secret),
		DynamicClient: dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), certificate),
		namespace:     namespace,
		enabled:       true,
	}
}

func newCertificatePEM(t *testing.T, domain string, expiresAt time.Time) []byte {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: domain},
		DNSNames:     []string{domain},
		NotBefore:    time.Now().Add(-24 * time.Hour),
		NotAfter:     expiresAt,
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}
