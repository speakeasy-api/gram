package mv

import (
	"time"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

func BuildRemoteSessionIssuerView(row repo.RemoteSessionIssuer) *types.RemoteSessionIssuer {
	projectID := ""
	if row.ProjectID.Valid {
		projectID = row.ProjectID.UUID.String()
	}

	organizationID := ""
	if row.OrganizationID.Valid {
		organizationID = row.OrganizationID.String
	}

	return &types.RemoteSessionIssuer{
		ID:                                row.ID.String(),
		ProjectID:                         projectID,
		OrganizationID:                    organizationID,
		Slug:                              row.Slug,
		Issuer:                            row.Issuer,
		Name:                              conv.FromPGText[string](row.Name),
		LogoAssetID:                       conv.FromNullableUUID(row.LogoAssetID),
		ClientSetupDocumentationURL:       conv.FromPGText[string](row.ClientSetupDocumentationUrl),
		AuthorizationEndpoint:             conv.FromPGText[string](row.AuthorizationEndpoint),
		TokenEndpoint:                     conv.FromPGText[string](row.TokenEndpoint),
		RegistrationEndpoint:              conv.FromPGText[string](row.RegistrationEndpoint),
		JwksURI:                           conv.FromPGText[string](row.JwksUri),
		ServiceDocumentation:              conv.FromPGText[string](row.ServiceDocumentation),
		OpPolicyURI:                       conv.FromPGText[string](row.OpPolicyUri),
		OpTosURI:                          conv.FromPGText[string](row.OpTosUri),
		ScopesSupported:                   row.ScopesSupported,
		GrantTypesSupported:               row.GrantTypesSupported,
		ResponseTypesSupported:            row.ResponseTypesSupported,
		TokenEndpointAuthMethodsSupported: row.TokenEndpointAuthMethodsSupported,
		Oidc:                              row.Oidc,
		Passthrough:                       row.Passthrough,
		ClientIDMetadataDocumentSupported: row.ClientIDMetadataDocumentSupported,
		CreatedAt:                         row.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:                         row.UpdatedAt.Time.Format(time.RFC3339),
	}
}
