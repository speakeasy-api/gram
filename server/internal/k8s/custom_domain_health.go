package k8s

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type CustomDomainInfrastructureIssue string

const (
	CustomDomainInfrastructureIssueResourceMissing     CustomDomainInfrastructureIssue = "resource_missing"
	CustomDomainInfrastructureIssueCertificateMissing  CustomDomainInfrastructureIssue = "certificate_missing"
	CustomDomainInfrastructureIssueCertificateNotReady CustomDomainInfrastructureIssue = "certificate_not_ready"
	CustomDomainInfrastructureIssueCertificateExpired  CustomDomainInfrastructureIssue = "certificate_expired"
	CustomDomainInfrastructureIssueCertificateInvalid  CustomDomainInfrastructureIssue = "certificate_invalid"
)

type CustomDomainInfrastructureHealth struct {
	Issue                CustomDomainInfrastructureIssue
	CertificateExpiresAt *time.Time
}

type CustomDomainInfrastructureCheck struct {
	Domain          string
	ResourceName    string
	CertSecretName  string
	ProvisionerKind ProvisionerKind
}

var certificateGVR = schema.GroupVersionResource{
	Group:    "cert-manager.io",
	Version:  "v1",
	Resource: "certificates",
}

func (k *KubernetesClients) CheckCustomDomainInfrastructure(ctx context.Context, check CustomDomainInfrastructureCheck) (CustomDomainInfrastructureHealth, error) {
	health := CustomDomainInfrastructureHealth{Issue: "", CertificateExpiresAt: nil}

	if !k.enabled {
		return health, nil
	}
	if check.ResourceName == "" {
		health.Issue = CustomDomainInfrastructureIssueResourceMissing
		return health, nil
	}

	if err := k.Provisioner(check.ProvisionerKind).Get(ctx, check.ResourceName); err != nil {
		if k8serrors.IsNotFound(err) {
			health.Issue = CustomDomainInfrastructureIssueResourceMissing
			return health, nil
		}
		return health, fmt.Errorf("get custom domain resource: %w", err)
	}
	if check.ProvisionerKind == ProvisionerKindGateway {
		return health, nil
	}
	if check.CertSecretName == "" {
		health.Issue = CustomDomainInfrastructureIssueCertificateMissing
		return health, nil
	}

	secret, err := k.Clientset.CoreV1().Secrets(k.namespace).Get(ctx, check.CertSecretName, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			health.Issue = CustomDomainInfrastructureIssueCertificateMissing
			return health, nil
		}
		return health, fmt.Errorf("get custom domain certificate secret: %w", err)
	}

	certificate, err := parseTLSCertificate(secret)
	if err != nil {
		health.Issue = CustomDomainInfrastructureIssueCertificateInvalid
		return health, nil
	}
	expiresAt := certificate.NotAfter.UTC()
	health.CertificateExpiresAt = &expiresAt
	now := time.Now()
	if now.Before(certificate.NotBefore) {
		health.Issue = CustomDomainInfrastructureIssueCertificateInvalid
		return health, nil
	}
	if now.After(expiresAt) {
		health.Issue = CustomDomainInfrastructureIssueCertificateExpired
		return health, nil
	}
	if err := certificate.VerifyHostname(check.Domain); err != nil {
		health.Issue = CustomDomainInfrastructureIssueCertificateInvalid
		return health, nil
	}

	resource, err := k.DynamicClient.Resource(certificateGVR).Namespace(k.namespace).Get(ctx, check.CertSecretName, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			health.Issue = CustomDomainInfrastructureIssueCertificateMissing
			return health, nil
		}
		return health, fmt.Errorf("get cert-manager certificate: %w", err)
	}

	conditions, found, err := unstructured.NestedSlice(resource.Object, "status", "conditions")
	if err != nil {
		return health, fmt.Errorf("read cert-manager certificate conditions: %w", err)
	}
	if !found {
		health.Issue = CustomDomainInfrastructureIssueCertificateNotReady
		return health, nil
	}
	for _, condition := range conditions {
		conditionMap, ok := condition.(map[string]any)
		if !ok {
			continue
		}
		if conditionMap["type"] == "Ready" && conditionMap["status"] == "True" {
			return health, nil
		}
	}

	health.Issue = CustomDomainInfrastructureIssueCertificateNotReady
	return health, nil
}

func parseTLSCertificate(secret *corev1.Secret) (*x509.Certificate, error) {
	data := secret.Data[corev1.TLSCertKey]
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("decode TLS certificate PEM")
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse TLS certificate: %w", err)
	}
	return certificate, nil
}
