package admin

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpserversRepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
)

// seedRemoteServer is an mcp_servers row backed by a remote MCP server at url,
// authenticating through issuerID (uuid.Nil for none).
func (f healthFixture) seedRemoteServer(t *testing.T, url string, issuerID uuid.UUID) uuid.UUID {
	t.Helper()

	slug := "remote-" + uuid.NewString()[:8]
	remote, err := remotemcprepo.New(f.conn).CreateServer(t.Context(), remotemcprepo.CreateServerParams{
		ID:            uuid.New(),
		ProjectID:     f.projectID,
		Name:          conv.ToPGText(slug),
		Slug:          conv.ToPGText(slug),
		TransportType: "streamable-http",
		Url:           url,
	})
	require.NoError(t, err)
	srv, err := mcpserversRepo.New(f.conn).CreateMCPServer(t.Context(), mcpserversRepo.CreateMCPServerParams{
		ID:                    uuid.New(),
		ProjectID:             f.projectID,
		Name:                  conv.ToPGText(slug),
		Slug:                  conv.ToPGText(slug),
		EnvironmentID:         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		UserSessionIssuerID:   uuid.NullUUID{UUID: issuerID, Valid: issuerID != uuid.Nil},
		RemoteMcpServerID:     uuid.NullUUID{UUID: remote.ID, Valid: true},
		TunneledMcpServerID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ToolsetID:             uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		UnproxiedMcpServerID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ToolVariationsGroupID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Visibility:            "private",
		NetworkAccessMode:     pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)
	return srv.ID
}

func (f healthFixture) setScopePin(t *testing.T, serverID uuid.UUID, scopes ...string) (*gen.AdminMcpServerResourceScopes, error) {
	t.Helper()

	if scopes == nil {
		scopes = []string{}
	}
	return f.svc.SetMcpServerScopePin(t.Context(), &gen.SetMcpServerScopePinPayload{
		AdminSessionToken: nil,
		OrganizationID:    f.orgID,
		ProjectID:         f.projectID.String(),
		McpServerID:       serverID.String(),
		Scopes:            scopes,
	})
}

func TestDescribeMcpServerHealth_ReportsResourceScopesForRemoteServer(t *testing.T) {
	t.Parallel()

	f := newHealthFixture(t, true)
	issuerID := f.seedUserSessionIssuer(t, "remote-"+uuid.NewString()[:8])
	remoteIssuerID := f.seedGlobalRemoteSessionIssuer(t, "upstream-"+uuid.NewString()[:8])
	clientID := f.seedRemoteSessionClient(t, remoteIssuerID, issuerID, "scoped-client", true)
	serverID := f.seedRemoteServer(t, "https://scopes.example.com/mcp", issuerID)
	_, err := f.setScopePin(t, serverID, "read")
	require.NoError(t, err)

	got, err := f.describe(t, serverID, 14)
	require.NoError(t, err)
	require.Equal(t, "remote", got.Server.Source)
	require.NotNil(t, got.ResourceScopes)
	require.Equal(t, "https://scopes.example.com/mcp", got.ResourceScopes.ResourceURL)
	require.Equal(t, []string{"read"}, got.ResourceScopes.PinnedScopes)
	require.Len(t, got.ResourceScopes.Clients, 1)
	require.Equal(t, clientID.String(), got.ResourceScopes.Clients[0].ClientID)
}

func TestDescribeMcpServerHealth_OmitsResourceScopesForToolsetServer(t *testing.T) {
	t.Parallel()

	f := newHealthFixture(t, true)
	serverID := seedToolset(t, t.Context(), f.conn, f.orgID, f.projectID, "hosted", true)

	got, err := f.describe(t, serverID, 14)
	require.NoError(t, err)
	require.Nil(t, got.ResourceScopes)
}

func TestSetMcpServerScopePin_SetsClearsAndAudits(t *testing.T) {
	t.Parallel()

	f := newHealthFixture(t, true)
	serverID := f.seedRemoteServer(t, "https://pin.example.com/mcp", uuid.Nil)

	got, err := f.setScopePin(t, serverID, "read", "write")
	require.NoError(t, err)
	require.Equal(t, []string{"read", "write"}, got.PinnedScopes)

	record, err := audittest.LatestAuditLogByAction(t.Context(), f.conn, audit.ActionMcpServerScopePinUpdate)
	require.NoError(t, err)
	require.Equal(t, f.orgID, record.OrganizationID)
	require.Equal(t, serverID.String(), record.SubjectID)

	got, err = f.setScopePin(t, serverID)
	require.NoError(t, err)
	require.Empty(t, got.PinnedScopes)
}

func TestSetMcpServerScopePin_ProjectInAnotherOrganizationIsNotFound(t *testing.T) {
	t.Parallel()

	f := newHealthFixture(t, true)
	serverID := f.seedRemoteServer(t, "https://theirs.example.com/mcp", uuid.Nil)
	otherOrg := "org_other_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	seedOrg(t, t.Context(), f.conn, orgFixture{id: otherOrg, name: "Other", slug: otherOrg})

	_, err := f.svc.SetMcpServerScopePin(t.Context(), &gen.SetMcpServerScopePinPayload{
		AdminSessionToken: nil,
		OrganizationID:    otherOrg,
		ProjectID:         f.projectID.String(),
		McpServerID:       serverID.String(),
		Scopes:            []string{"read"},
	})
	requireHealthCode(t, err, oops.CodeNotFound)
}

func TestSetMcpServerScopePin_ToolsetServerIsNotFound(t *testing.T) {
	t.Parallel()

	f := newHealthFixture(t, true)
	toolsetID := seedToolset(t, t.Context(), f.conn, f.orgID, f.projectID, "hosted-pin", false)
	serverID := f.seedServerWithIssuer(t, toolsetID, uuid.Nil, "hosted-pin")

	_, err := f.setScopePin(t, serverID, "read")
	requireHealthCode(t, err, oops.CodeNotFound)
}
