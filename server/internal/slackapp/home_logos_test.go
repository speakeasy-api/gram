package slackapp

import (
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	assetsrepo "github.com/speakeasy-api/gram/server/internal/assets/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	metadatarepo "github.com/speakeasy-api/gram/server/internal/mcpmetadata/repo"
	mcprepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	remoterepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestHomeLoadsExistingMCPLogo(t *testing.T) {
	t.Parallel()
	f := newHomeFixture(t)
	ctx := t.Context()
	name := pgtype.Text{String: "Linear", Valid: true}
	slug := pgtype.Text{String: "linear", Valid: true}
	remote, err := remoterepo.New(f.db).CreateServer(ctx, remoterepo.CreateServerParams{ID: uuid.New(), ProjectID: f.projectID, Name: name, Slug: slug, TransportType: "streamable-http", Url: "https://example.com/mcp"})
	require.NoError(t, err)
	server, err := mcprepo.New(f.db).CreateMCPServer(ctx, mcprepo.CreateMCPServerParams{ID: uuid.New(), ProjectID: f.projectID, Name: name, Slug: slug, RemoteMcpServerID: uuid.NullUUID{UUID: remote.ID, Valid: true}, Visibility: "public"})
	require.NoError(t, err)
	selectors, err := authz.NewSelector(authz.ScopeMCPRead, server.ID.String()).MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(f.db).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{OrganizationID: homeTestOrg, PrincipalUrn: urn.NewPrincipal(urn.PrincipalTypeUser, homeTestApprover), Scope: string(authz.ScopeMCPRead), Selectors: selectors})
	require.NoError(t, err)
	apiURL, err := url.Parse("https://api.example.com")
	require.NoError(t, err)
	source := NewHomeSource(testenv.NewLogger(t), f.db, allFlags{}).WithServerURL(apiURL)
	for _, format := range []struct {
		mime  string
		image bool
	}{{"image/png", true}, {"image/svg+xml", false}} {
		asset, err := assetsrepo.New(f.db).CreateAsset(ctx, assetsrepo.CreateAssetParams{Name: "logo", Url: "s3://fixtures/logo", ProjectID: f.projectID, OrganizationID: homeTestOrg, Sha256: strings.Repeat("a", 64) + format.mime, Kind: "image", ContentType: format.mime, ContentLength: 10})
		require.NoError(t, err)
		_, err = metadatarepo.New(f.db).UpsertMetadataByMcpServerID(ctx, metadatarepo.UpsertMetadataByMcpServerIDParams{McpServerID: server.ID, ProjectID: f.projectID, LogoID: uuid.NullUUID{UUID: asset.ID, Valid: true}})
		require.NoError(t, err)
		data := source.Load(ctx, homeTestOrg, homeTestApprover, "https://app.example.com")
		require.False(t, data.ServersError)
		require.Len(t, data.Servers, 1)
		if format.image {
			require.Equal(t, "https://api.example.com/rpc/assets.serveImage?id="+asset.ID.String(), data.Servers[0].LogoURL)
		} else {
			require.Empty(t, data.Servers[0].LogoURL, "Slack does not support SVG image accessories")
		}
	}
}
