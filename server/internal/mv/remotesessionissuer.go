package mv

import (
	"time"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

func BuildRemoteSessionIssuerView(row repo.RemoteSessionIssuer) *types.RemoteSessionIssuer {
	return &types.RemoteSessionIssuer{
		ID:                                row.ID.String(),
		ProjectID:                         row.ProjectID.String(),
		Slug:                              row.Slug,
		Issuer:                            row.Issuer,
		AuthorizationEndpoint:             conv.FromPGText[string](row.AuthorizationEndpoint),
		TokenEndpoint:                     conv.FromPGText[string](row.TokenEndpoint),
		RegistrationEndpoint:              conv.FromPGText[string](row.RegistrationEndpoint),
		JwksURI:                           conv.FromPGText[string](row.JwksUri),
		ScopesSupported:                   row.ScopesSupported,
		GrantTypesSupported:               row.GrantTypesSupported,
		ResponseTypesSupported:            row.ResponseTypesSupported,
		TokenEndpointAuthMethodsSupported: row.TokenEndpointAuthMethodsSupported,
		Oidc:                              row.Oidc,
		Passthrough:                       row.Passthrough,
		CreatedAt:                         row.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:                         row.UpdatedAt.Time.Format(time.RFC3339),
	}
}
