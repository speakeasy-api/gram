package admin

import (
	"context"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	mcpserversRepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	remotemcpRepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
)

func newMCPVerifyTestService(t *testing.T) (context.Context, *Service, *pgxpool.Pool) {
	t.Helper()
	ctx, svc, conn := newTestAdminService(t)
	dashboardURL, err := url.Parse("https://app.example.com")
	require.NoError(t, err)
	svc.dashboardURL = dashboardURL
	return ctx, svc, conn
}

// seedRemoteBackedMCPServer adds a remote MCP source and the MCP server that
// fronts it, as the dashboard's add-server flow does.
func seedRemoteBackedMCPServer(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID uuid.UUID, remoteURL, slug string) uuid.UUID {
	t.Helper()

	remote, err := remotemcpRepo.New(conn).CreateServer(ctx, remotemcpRepo.CreateServerParams{
		ID:            uuid.New(),
		ProjectID:     projectID,
		Name:          pgtype.Text{String: "", Valid: false},
		Slug:          pgtype.Text{String: "", Valid: false},
		TransportType: "streamable-http",
		Url:           remoteURL,
	})
	require.NoError(t, err)

	server, err := mcpserversRepo.New(conn).CreateMCPServer(ctx, mcpserversRepo.CreateMCPServerParams{
		ID:                    uuid.New(),
		ProjectID:             projectID,
		Name:                  pgtype.Text{String: slug, Valid: true},
		Slug:                  pgtype.Text{String: slug, Valid: true},
		EnvironmentID:         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		UserSessionIssuerID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		RemoteMcpServerID:     uuid.NullUUID{UUID: remote.ID, Valid: true},
		TunneledMcpServerID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ToolsetID:             uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		UnproxiedMcpServerID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ToolVariationsGroupID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Visibility:            "disabled",
		NetworkAccessMode:     pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)

	serverID := server.ID
	return serverID
}

func getVerificationLink(t *testing.T, ctx context.Context, svc *Service, rawURL string, name *string) (*gen.AdminMcpVerificationLink, error) {
	t.Helper()
	return svc.GetMcpVerificationLink(ctx, &gen.GetMcpVerificationLinkPayload{AdminSessionToken: nil, URL: rawURL, Name: name})
}

func TestGetMcpVerificationLink_UnavailableWithoutProject(t *testing.T) {
	t.Parallel()
	ctx, svc, _ := newMCPVerifyTestService(t)

	_, err := getVerificationLink(t, ctx, svc, "https://mcp.example.test/mcp", nil)
	requireMCPDebugOopsCode(t, err, oops.CodeUnavailable)
}

func TestGetMcpVerificationLink_UnavailableWhenProjectIsInAnotherOrganization(t *testing.T) {
	t.Parallel()
	ctx, svc, conn := newMCPVerifyTestService(t)
	seedOrg(t, ctx, conn, orgFixture{id: "org_speakeasy", name: "Speakeasy", slug: "speakeasy"})
	seedOrg(t, ctx, conn, orgFixture{id: "org_other", name: "Other", slug: "other"})
	projectID := seedProject(t, ctx, conn, "org_other", "theirs")
	svc.SetMCPVerificationProject("org_speakeasy", projectID)

	_, err := getVerificationLink(t, ctx, svc, "https://mcp.example.test/mcp", nil)
	requireMCPDebugOopsCode(t, err, oops.CodeUnavailable)
}

func TestGetMcpVerificationLink_RejectsNonHTTPURL(t *testing.T) {
	t.Parallel()
	ctx, svc, conn := newMCPVerifyTestService(t)
	seedOrg(t, ctx, conn, orgFixture{id: "org_speakeasy", name: "Speakeasy", slug: "speakeasy"})
	svc.SetMCPVerificationProject("org_speakeasy", seedProject(t, ctx, conn, "org_speakeasy", "default"))

	_, err := getVerificationLink(t, ctx, svc, "ftp://mcp.example.test/mcp", nil)
	requireMCPDebugOopsCode(t, err, oops.CodeBadRequest)
}

func TestGetMcpVerificationLink_NewServerOpensPrefilledAddPage(t *testing.T) {
	t.Parallel()
	ctx, svc, conn := newMCPVerifyTestService(t)
	seedOrg(t, ctx, conn, orgFixture{id: "org_speakeasy", name: "Speakeasy", slug: "speakeasy"})
	svc.SetMCPVerificationProject("org_speakeasy", seedProject(t, ctx, conn, "org_speakeasy", "default"))
	name := " Example "

	link, err := getVerificationLink(t, ctx, svc, " https://mcp.example.test/mcp?tenant=a ", &name)
	require.NoError(t, err)
	require.False(t, link.Existing)
	require.Equal(t, "default", link.ProjectName)

	got, err := url.Parse(link.URL)
	require.NoError(t, err)
	require.Equal(t, "app.example.com", got.Host)
	require.Equal(t, "/speakeasy/projects/default/mcp/add/remote", got.Path)
	require.Equal(t, "https://mcp.example.test/mcp?tenant=a", got.Query().Get("url"))
	require.Equal(t, "Example", got.Query().Get("name"))
}

func TestGetMcpVerificationLink_ExistingServerOpensPlayground(t *testing.T) {
	t.Parallel()
	ctx, svc, conn := newMCPVerifyTestService(t)
	seedOrg(t, ctx, conn, orgFixture{id: "org_speakeasy", name: "Speakeasy", slug: "speakeasy"})
	projectID := seedProject(t, ctx, conn, "org_speakeasy", "default")
	svc.SetMCPVerificationProject("org_speakeasy", projectID)
	serverID := seedRemoteBackedMCPServer(t, ctx, conn, projectID, "https://mcp.example.test/mcp", "example")
	seedRemoteBackedMCPServer(t, ctx, conn, projectID, "https://other.example.test/mcp", "other")

	link, err := getVerificationLink(t, ctx, svc, "https://mcp.example.test/mcp", nil)
	require.NoError(t, err)
	require.True(t, link.Existing)

	got, err := url.Parse(link.URL)
	require.NoError(t, err)
	require.Equal(t, "/speakeasy/projects/default/playground", got.Path)
	require.Equal(t, serverID.String(), got.Query().Get("mcpServer"))
}

func TestGetMcpVerificationLink_IgnoresServersInOtherProjectsAndDeletedServers(t *testing.T) {
	t.Parallel()
	ctx, svc, conn := newMCPVerifyTestService(t)
	seedOrg(t, ctx, conn, orgFixture{id: "org_speakeasy", name: "Speakeasy", slug: "speakeasy"})
	projectID := seedProject(t, ctx, conn, "org_speakeasy", "default")
	otherProjectID := seedProject(t, ctx, conn, "org_speakeasy", "elsewhere")
	svc.SetMCPVerificationProject("org_speakeasy", projectID)
	seedRemoteBackedMCPServer(t, ctx, conn, otherProjectID, "https://mcp.example.test/mcp", "elsewhere")
	deleted := seedRemoteBackedMCPServer(t, ctx, conn, projectID, "https://mcp.example.test/mcp", "deleted")
	deleteMCPServer(t, ctx, conn, projectID, deleted)

	link, err := getVerificationLink(t, ctx, svc, "https://mcp.example.test/mcp", nil)
	require.NoError(t, err)
	require.False(t, link.Existing)
}
