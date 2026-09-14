package networkingress

import (
	_ "crypto/sha256"
	"fmt"
	"net/netip"
	"regexp"
	"strings"

	"github.com/distribution/reference"
	"k8s.io/apimachinery/pkg/util/validation"

	"github.com/speakeasy-api/gram/server/internal/k8s"
)

var runtimeQueuePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$`)

// RuntimeConfig separates optional lifecycle configuration from permission to
// apply provider changes. Its zero value disables lifecycle delivery and mutation.
type RuntimeConfig struct {
	// ReconcileTaskQueue is explicit and never falls back to a general worker queue.
	ReconcileTaskQueue string

	// ProviderMutationsEnabled permits apply and rotation, not admission or cleanup.
	ProviderMutationsEnabled bool

	// Tailscale configures operator resources and private backend network access.
	Tailscale k8s.TailscaleNetworkIngressConfig

	// AttestorImage is the digest-pinned image deployed for each ingress.
	AttestorImage string

	// BackendService names the private HTTPS Service in Tailscale.BackendNamespace.
	BackendService string

	// BackendPort is the private HTTPS Service port; zero means unconfigured.
	BackendPort int32

	// IngressClass must explicitly select the supported tailscale ingress class.
	IngressClass string

	// TokenAudience must match the private listener's projected token audience.
	TokenAudience string
}

// Validate rejects invalid supplied values without requiring apply-only settings.
// Missing configuration leaves mutation unavailable without preventing cleanup.
func (c RuntimeConfig) Validate() error {
	if c.ReconcileTaskQueue != "" && !runtimeQueuePattern.MatchString(c.ReconcileTaskQueue) {
		return fmt.Errorf("invalid private ingress reconciliation task queue")
	}
	for _, name := range []struct{ field, value string }{
		{"operator namespace", c.Tailscale.OperatorNamespace},
		{"backend namespace", c.Tailscale.BackendNamespace},
		{"backend service", c.BackendService},
		{"attestor CA secret", c.Tailscale.AttestorCASecret},
	} {
		if name.value != "" && len(validation.IsDNS1123Label(name.value)) > 0 {
			return fmt.Errorf("invalid private ingress %s", name.field)
		}
	}
	for _, port := range []struct {
		field string
		value int32
	}{
		{"backend port", c.BackendPort},
		{"Kubernetes API port", c.Tailscale.KubernetesAPIPort},
	} {
		if port.value < 0 || port.value > 65535 {
			return fmt.Errorf("private ingress %s must be between 1 and 65535, or zero when unconfigured", port.field)
		}
	}
	if c.AttestorImage != "" {
		if err := ValidateAttestorImageReference(c.AttestorImage); err != nil {
			return err
		}
	}
	for key, value := range c.Tailscale.BackendPodLabels {
		if len(validation.IsQualifiedName(key)) > 0 || len(validation.IsValidLabelValue(value)) > 0 {
			return fmt.Errorf("invalid private ingress backend pod labels")
		}
	}
	for _, value := range []string{c.Tailscale.ProxyTag, c.Tailscale.ServiceTag} {
		if value == "" {
			continue
		}
		tag, ok := strings.CutPrefix(value, "tag:")
		if !ok || tag == "" || len(validation.IsDNS1123Label(tag)) > 0 {
			return fmt.Errorf("invalid private ingress Tailscale tag")
		}
	}
	if c.IngressClass != "" && c.IngressClass != "tailscale" {
		return fmt.Errorf("private ingress class must be tailscale")
	}
	if c.TokenAudience != "" && c.TokenAudience != "gram-netingress" {
		return fmt.Errorf("private ingress token audience must be gram-netingress")
	}
	if c.Tailscale.KubernetesAPICIDR != "" {
		if _, err := netip.ParsePrefix(c.Tailscale.KubernetesAPICIDR); err != nil {
			return fmt.Errorf("invalid private ingress Kubernetes API CIDR")
		}
	}
	for _, cidr := range c.Tailscale.ClusterCIDRs {
		if _, err := netip.ParsePrefix(cidr); err != nil {
			return fmt.Errorf("invalid private ingress cluster CIDR")
		}
	}
	return nil
}

// ValidateAttestorImageReference requires a complete OCI image reference pinned
// to a valid sha256 digest.
func ValidateAttestorImageReference(value string) error {
	parsed, err := reference.Parse(value)
	if err != nil {
		return fmt.Errorf("private ingress attestor image must be a valid image reference with a sha256 digest")
	}
	if _, ok := parsed.(reference.Named); !ok {
		return fmt.Errorf("private ingress attestor image must be a valid image reference with a sha256 digest")
	}
	digested, ok := parsed.(reference.Digested)
	if !ok || digested.Digest().Algorithm().String() != "sha256" {
		return fmt.Errorf("private ingress attestor image must be a valid image reference with a sha256 digest")
	}
	return nil
}

// MutationReady reports configuration readiness for apply and rotation only.
// Callers must independently verify clients and admission integration readiness.
// Observe and Delete must not depend on this result.
func (c RuntimeConfig) MutationReady() bool {
	return c.ProviderMutationsEnabled && c.Validate() == nil &&
		c.ReconcileTaskQueue != "" && c.Tailscale.OperatorNamespace != "" &&
		c.Tailscale.BackendNamespace != "" && len(c.Tailscale.BackendPodLabels) > 0 &&
		c.Tailscale.ProxyTag != "" && c.Tailscale.ServiceTag != "" &&
		c.Tailscale.AttestorCASecret != "" && c.Tailscale.KubernetesAPICIDR != "" &&
		c.Tailscale.KubernetesAPIPort != 0 && len(c.Tailscale.ClusterCIDRs) > 0 &&
		c.AttestorImage != "" && c.BackendService != "" && c.BackendPort != 0 &&
		c.IngressClass != "" && c.TokenAudience != ""
}
