package mv

import (
	"time"

	gen "github.com/speakeasy-api/gram/server/gen/network_ingress"
	"github.com/speakeasy-api/gram/server/internal/conv"
	networkingressrepo "github.com/speakeasy-api/gram/server/internal/networkingress/repo"
)

// BuildNetworkIngressView deliberately omits ciphertext, provider resource
// identities, and attestor workload identity. The API exposes only whether
// credentials are configured and redacted lifecycle observations.
func BuildNetworkIngressView(ingress networkingressrepo.NetworkIngress) *gen.NetworkIngress {
	return &gen.NetworkIngress{
		ID:                    ingress.ID.String(),
		OrganizationID:        ingress.OrganizationID,
		Provider:              ingress.Provider,
		Hostname:              ingress.Hostname,
		EndpointNamespaceKind: ingress.EndpointNamespaceKind,
		CustomDomainID:        conv.FromNullableUUID(ingress.CustomDomainID),
		Enabled:               ingress.Enabled,
		IdentityRequired:      ingress.IdentityRequired,
		CredentialsConfigured: ingress.CredentialsEncrypted.Valid && ingress.CredentialsEncrypted.String != "",
		Status:                ingress.Status,
		DNSName:               conv.FromPGText[string](ingress.DnsName),
		LastError:             conv.FromPGText[string](ingress.LastError),
		HealthCheckedAt:       conv.PtrEmpty(conv.FromPGTimestamptz(ingress.HealthCheckedAt)),
		ConnectedSince:        conv.PtrEmpty(conv.FromPGTimestamptz(ingress.ConnectedSince)),
		CreatedAt:             ingress.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:             ingress.UpdatedAt.Time.Format(time.RFC3339),
	}
}
