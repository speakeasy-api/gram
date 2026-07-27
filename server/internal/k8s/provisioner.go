package k8s

import "context"

var _ CustomDomainProvisioner = (*StubProvisioner)(nil)
var _ CustomDomainProvisioner = (*IngressProvisioner)(nil)

type ProvisionerKind string

const ProvisionerKindIngress ProvisionerKind = "ingress"

// Orphan reconciliation relies on every provisioner applying these labels.
const (
	managedByLabelKey    = "app.kubernetes.io/managed-by"
	managedByLabelValue  = "custom-domain-chart"
	customDomainLabelKey = "custom-domain"
)

// SetupResult carries the provisioned resource identifiers.
// SecretName is empty when the provisioner does not own a TLS Secret.
type SetupResult struct {
	ResourceName string
	SecretName   string
}

// CustomDomainProvisioner abstracts Kubernetes custom-domain provisioning.
// Get probes resource existence only; readiness polling is a follow-up.
// Delete is idempotent.
//
// Setup accepts an ipAllowlist of IPv4 addresses and/or CIDR ranges. A non-empty
// list restricts inbound traffic to those sources; an empty/nil list removes any
// previously applied restriction (open to all). Setup is idempotent and is also
// the re-apply path when only the allowlist changes.
type CustomDomainProvisioner interface {
	Kind() ProvisionerKind
	Setup(ctx context.Context, domain string, ipAllowlist []string) (SetupResult, error)
	Get(ctx context.Context, resourceName string) error
	Delete(ctx context.Context, resourceName, secretName string) error
}

// ProvisionerFactory creates a CustomDomainProvisioner for the given kind.
// *KubernetesClients implements this interface; tests can inject a stub.
type ProvisionerFactory interface {
	Provisioner(kind ProvisionerKind) CustomDomainProvisioner
}
