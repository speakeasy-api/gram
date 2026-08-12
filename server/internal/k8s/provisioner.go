package k8s

import "context"

var _ CustomDomainProvisioner = (*StubProvisioner)(nil)
var _ CustomDomainProvisioner = (*IngressProvisioner)(nil)

type ProvisionerKind string

const ProvisionerKindIngress ProvisionerKind = "ingress"

// Orphan reconciliation relies on every provisioner applying these labels.
const (
	managedByLabelKey             = "app.kubernetes.io/managed-by"
	managedByLabelValue           = "custom-domain-chart"
	customDomainLabelKey          = "custom-domain"
	customDomainRoleKey           = "custom-domain-role"
	customDomainRoleMain          = "primary"
	customDomainRoleRoot          = "root"
	customDomainRoleWellKnownRoot = "wellknown-root"
)

// RouteConfig is the desired routing state for one custom domain.
type RouteConfig struct {
	Domain      string
	IPAllowlist []string
	RootTarget  *string
}

// SetupResult carries the provisioned primary resource identifiers.
// SecretName is empty when the provisioner does not own a TLS Secret.
type SetupResult struct {
	ResourceName string
	SecretName   string
}

// CustomDomainProvisioner abstracts Kubernetes custom-domain provisioning.
// Get probes resource existence only; readiness polling is a follow-up.
// Delete is idempotent.
//
// Apply accepts the complete desired route state. It is idempotent: a non-empty
// allowlist restricts inbound traffic, an empty list removes the restriction,
// and RootTarget controls whether the custom-domain root route exists.
type CustomDomainProvisioner interface {
	Kind() ProvisionerKind
	Apply(ctx context.Context, config RouteConfig) (SetupResult, error)
	Get(ctx context.Context, resourceName string) error
	Delete(ctx context.Context, resourceName, secretName string) error
}

// ProvisionerFactory creates a CustomDomainProvisioner for the given kind.
// *KubernetesClients implements this interface; tests can inject a stub.
type ProvisionerFactory interface {
	Provisioner(kind ProvisionerKind) CustomDomainProvisioner
}
