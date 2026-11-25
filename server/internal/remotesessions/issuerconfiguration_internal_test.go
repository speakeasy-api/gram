package remotesessions

import (
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/stretchr/testify/require"
)

func TestIssuerBindingConfigurationChanged(t *testing.T) {
	t.Parallel()
	base := repo.RemoteSessionIssuer{Issuer: "https://issuer.example.com"}
	for _, tc := range []struct {
		name   string
		change func(*repo.RemoteSessionIssuer)
	}{
		{"identity", func(row *repo.RemoteSessionIssuer) { row.Issuer = "https://other.example.com" }},
		{"authorization", func(row *repo.RemoteSessionIssuer) {
			row.AuthorizationEndpoint = conv.ToPGText("https://issuer.example.com/authorize")
		}},
		{"token", func(row *repo.RemoteSessionIssuer) {
			row.TokenEndpoint = conv.ToPGText("https://issuer.example.com/token")
		}},
		{"revocation", func(row *repo.RemoteSessionIssuer) {
			row.RevocationEndpoint = conv.ToPGText("https://issuer.example.com/revoke")
		}},
		{"registration", func(row *repo.RemoteSessionIssuer) {
			row.RegistrationEndpoint = conv.ToPGText("https://issuer.example.com/register")
		}},
		{"jwks", func(row *repo.RemoteSessionIssuer) { row.JwksUri = conv.ToPGText("https://issuer.example.com/jwks") }},
		{"userinfo", func(row *repo.RemoteSessionIssuer) {
			row.UserinfoEndpoint = conv.ToPGText("https://issuer.example.com/userinfo")
		}},
		{"introspection", func(row *repo.RemoteSessionIssuer) {
			row.IntrospectionEndpoint = conv.ToPGText("https://issuer.example.com/introspect")
		}},
		{"tunnel", func(row *repo.RemoteSessionIssuer) { row.TunneledMcpServerID = conv.ToNullUUID(uuid.New()) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			changed := base
			tc.change(&changed)
			require.True(t, issuerBindingConfigurationChanged(base, changed), "adding routing configuration")
			require.True(t, issuerBindingConfigurationChanged(changed, base), "removing routing configuration")
			require.False(t, issuerBindingConfigurationChanged(changed, changed), "no-op routing patch")
		})
	}
	presentation := base
	presentation.Name = conv.ToPGText("Renamed provider")
	presentation.Slug = "renamed"
	presentation.LogoAssetID = conv.ToNullUUID(uuid.New())
	presentation.ClientSetupDocumentationUrl = conv.ToPGText("https://docs.example.com")
	presentation.ServiceDocumentation = conv.ToPGText("https://docs.example.com/service")
	presentation.OpPolicyUri = conv.ToPGText("https://docs.example.com/policy")
	presentation.OpTosUri = conv.ToPGText("https://docs.example.com/terms")
	require.False(t, issuerBindingConfigurationChanged(base, presentation))
}
