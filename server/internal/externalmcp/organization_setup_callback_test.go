package externalmcp_test

import (
	"github.com/jackc/pgx/v5/pgtype"
	gen "github.com/speakeasy-api/gram/server/gen/mcp_registries"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/orghost"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	guides "github.com/speakeasy-api/mcp-setup-docs/go"
	"github.com/stretchr/testify/require"
	"net/url"
	"testing"
)

func TestSetupDocsOrganizationCallback(t *testing.T) {
	t.Parallel()
	for _, stored := range []string{"", "https://canonical.example.test"} {
		ctx, ti := newTestExternalMCPService(t)
		auth, _ := contextvalues.GetAuthContext(ctx)
		err := orgrepo.New(ti.conn).SetOrganizationDefaultHostForTest(ctx, orgrepo.SetOrganizationDefaultHostForTestParams{DefaultHost: pgtype.Text{String: stored, Valid: stored != ""}, ID: auth.ActiveOrganizationID})
		require.NoError(t, err)
		legacy, err := url.Parse("https://legacy.example.test")
		require.NoError(t, err)
		canonical, err := url.Parse("https://canonical.example.test")
		require.NoError(t, err)
		ti.service.SetCallbackOrigins(remotesessions.CallbackOrigins{Outbound: legacy, Registration: canonical, OrganizationHosts: orghost.New(orghost.Config{ServerURL: canonical, LegacyDefaultHost: legacy})})
		guide, alias, _ := setupDocsFixture(t)
		result, err := ti.service.GetSetupDocs(ctx, &gen.GetSetupDocsPayload{RegistrySpecifier: &alias})
		require.NoError(t, err)
		require.Len(t, result.Guides, 1)
		want := legacy.String()
		if stored != "" {
			want = canonical.String()
		}
		require.Equal(t, string(guide.RenderExternal(guides.Vars{OAuthCallbackURL: want + "/mcp/remote_login_callback"})), result.Guides[0].ExternalMarkdown)
	}
}
