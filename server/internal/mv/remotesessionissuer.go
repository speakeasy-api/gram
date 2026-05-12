package mv

import (
	"time"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// BuildRemoteSessionIssuerView converts a repo remote_session_issuers row into
// the API response type.
func BuildRemoteSessionIssuerView(issuer repo.RemoteSessionIssuer) *types.RemoteSessionIssuer {
	return &types.RemoteSessionIssuer{
		ID:                                issuer.ID.String(),
		ProjectID:                         issuer.ProjectID.String(),
		Slug:                              issuer.Slug,
		Issuer:                            issuer.Issuer,
		AuthorizationEndpoint:             conv.FromPGText[string](issuer.AuthorizationEndpoint),
		TokenEndpoint:                     conv.FromPGText[string](issuer.TokenEndpoint),
		RegistrationEndpoint:              conv.FromPGText[string](issuer.RegistrationEndpoint),
		JwksURI:                           conv.FromPGText[string](issuer.JwksUri),
		ScopesSupported:                   issuer.ScopesSupported,
		GrantTypesSupported:               issuer.GrantTypesSupported,
		ResponseTypesSupported:            issuer.ResponseTypesSupported,
		TokenEndpointAuthMethodsSupported: issuer.TokenEndpointAuthMethodsSupported,
		Oidc:                              issuer.Oidc,
		Passthrough:                       issuer.Passthrough,
		CreatedAt:                         issuer.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:                         issuer.UpdatedAt.Time.Format(time.RFC3339),
	}
}
