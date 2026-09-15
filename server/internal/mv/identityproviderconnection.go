package mv

import (
	"encoding/json"
	"fmt"

	gen "github.com/speakeasy-api/gram/server/gen/identity_providers"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/identityproviders/repo"
)

type identityProviderVerifyEvidence struct {
	CheckedAt string                           `json:"checked_at"`
	Reads     []identityProviderCapabilityRead `json:"reads"`
}

type identityProviderCapabilityRead struct {
	Capability string  `json:"capability"`
	Resource   string  `json:"resource"`
	OK         bool    `json:"ok"`
	Count      *int    `json:"count,omitempty"`
	Detail     *string `json:"detail,omitempty"`
}

// BuildIdentityProviderConnectionView converts a connection row into its API
// shape without exposing provider credentials or private signing material.
func BuildIdentityProviderConnectionView(row repo.GetIdentityProviderConnectionByOrganizationRow, jwksURL string) (*gen.IdentityProviderConnection, error) {
	var verifyEvidence *gen.IdentityProviderVerifyEvidence
	if len(row.VerifyEvidence) > 0 {
		var stored identityProviderVerifyEvidence
		if err := json.Unmarshal(row.VerifyEvidence, &stored); err != nil {
			return nil, fmt.Errorf("decode identity provider verification evidence: %w", err)
		}

		reads := make([]*gen.IdentityProviderCapabilityRead, len(stored.Reads))
		for i, read := range stored.Reads {
			reads[i] = &gen.IdentityProviderCapabilityRead{
				Capability: read.Capability,
				Resource:   read.Resource,
				OK:         read.OK,
				Count:      read.Count,
				Detail:     read.Detail,
			}
		}
		verifyEvidence = &gen.IdentityProviderVerifyEvidence{
			CheckedAt: stored.CheckedAt,
			Reads:     reads,
		}
	}

	return &gen.IdentityProviderConnection{
		ID:               row.ID.String(),
		Kind:             row.Kind,
		TenantIdentifier: row.TenantIdentifier,
		DisplayName:      conv.FromPGText[string](row.DisplayName),
		Status:           row.Status,
		StatusDetail:     conv.FromPGText[string](row.StatusDetail),
		Capabilities:     row.Capabilities,
		GrantedScopes:    row.GrantedScopes,
		JwksURL:          jwksURL,
		SigningKeyKid:    row.SigningKeyKid,
		LastVerifiedAt:   conv.PtrEmpty(conv.FromPGTimestamptz(row.LastVerifiedAt)),
		VerifyEvidence:   verifyEvidence,
		CreatedAt:        conv.FromPGTimestamptz(row.CreatedAt),
		UpdatedAt:        conv.FromPGTimestamptz(row.UpdatedAt),
	}, nil
}
