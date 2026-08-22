package mv

import (
	"time"

	genni "github.com/speakeasy-api/gram/server/gen/network_ingress"
	"github.com/speakeasy-api/gram/server/internal/conv"
	networkingressrepo "github.com/speakeasy-api/gram/server/internal/networkingress/repo"
)

// BuildNetworkIngressView converts a network ingress row into its API response
// type. Credential material is reduced to configured/not-configured booleans;
// the encrypted columns never leave the database layer.
func BuildNetworkIngressView(ingress networkingressrepo.NetworkIngress) *genni.NetworkIngress {
	tags := ingress.Tags
	if tags == nil {
		tags = []string{}
	}
	return &genni.NetworkIngress{
		ID:                    ingress.ID.String(),
		OrganizationID:        ingress.OrganizationID,
		Provider:              ingress.Provider,
		Hostname:              ingress.Hostname,
		Tags:                  tags,
		Enabled:               ingress.Enabled,
		PrivateNetworkOnly:    ingress.PrivateNetworkOnly,
		IdentityRequired:      ingress.IdentityRequired,
		CredentialKind:        ingress.CredentialKind,
		AuthKeyConfigured:     ingress.AuthKeyEnc.Valid && ingress.AuthKeyEnc.String != "",
		OauthClientConfigured: ingress.OauthClientID.Valid && ingress.OauthClientID.String != "" && ingress.OauthClientSecretEnc.Valid && ingress.OauthClientSecretEnc.String != "",
		Status:                ingress.Status,
		NetworkName:           conv.FromPGText[string](ingress.NetworkName),
		DNSName:               conv.FromPGText[string](ingress.DnsName),
		LastError:             conv.FromPGText[string](ingress.LastError),
		LastSeenAt:            conv.PtrEmpty(conv.FromPGTimestamptz(ingress.LastSeenAt)),
		ConnectedSince:        conv.PtrEmpty(conv.FromPGTimestamptz(ingress.ConnectedSince)),
		CreatedAt:             ingress.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:             ingress.UpdatedAt.Time.Format(time.RFC3339),
	}
}
