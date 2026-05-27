package k8s

import "context"

var _ CustomDomainProvisioner = (*StubProvisioner)(nil)
var _ CustomDomainProvisioner = (*IngressProvisioner)(nil)
var _ CustomDomainProvisioner = (*GatewayProvisioner)(nil)

type ProvisionerKind string

const (
	ProvisionerKindIngress ProvisionerKind = "ingress"
	ProvisionerKindGateway ProvisionerKind = "gateway"
)

// SetupResult carries the provisioned resource identifiers.
// SecretName is empty when the provisioner does not own a TLS Secret
// (Gateway rows: parent Gateway owns TLS, HTTPRoute does not reference a Secret).
type SetupResult struct {
	ResourceName string
	SecretName   string
}

// CustomDomainProvisioner abstracts Kubernetes Ingress and Gateway API provisioning.
// Get probes resource existence only; readiness polling is a follow-up.
// Delete is idempotent; Gateway impl must not touch any Secret it did not create.
type CustomDomainProvisioner interface {
	Kind() ProvisionerKind
	Setup(ctx context.Context, domain string) (SetupResult, error)
	Get(ctx context.Context, resourceName string) error
	Delete(ctx context.Context, resourceName, secretName string) error
}

// ProvisionerFactory creates a CustomDomainProvisioner for the given kind.
// *KubernetesClients implements this interface; tests can inject a stub.
type ProvisionerFactory interface {
	Provisioner(kind ProvisionerKind) CustomDomainProvisioner
}
