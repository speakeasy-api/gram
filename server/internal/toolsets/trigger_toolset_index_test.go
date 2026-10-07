package toolsets_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/types"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/toolsets"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// TriggerToolsetIndex runs with a nil temporal environment throughout, so a
// toolset it considers served reports ErrToolsetIndexUnavailable and one it
// considers unserved reports ErrToolsetIndexNotRequired. That difference is
// what index_signal shows an agent.
func TestTriggerToolsetIndexTreatsAToolsetFrontedByAServerAsServed(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	logger := testenv.NewLogger(t)
	db, err := infra.CloneTestDatabase(t, "trigger_toolset_index_served")
	require.NoError(t, err)
	project := createTriggerIndexProject(t, db)

	enabled := createTriggerIndexToolset(t, db, project, "enabled", true)
	require.ErrorIs(t, toolsets.TriggerToolsetIndex(ctx, logger, db, nil, toolsetView(enabled)), toolsets.ErrToolsetIndexUnavailable,
		"an MCP-enabled toolset with tools needs an index")

	fronted := createTriggerIndexToolset(t, db, project, "fronted", false)
	server := createTriggerIndexMCPServer(t, db, project.ID, uuid.New(), fronted.ID, "private")
	require.ErrorIs(t, toolsets.TriggerToolsetIndex(ctx, logger, db, nil, toolsetView(fronted)), toolsets.ErrToolsetIndexUnavailable,
		"a live server fronts the toolset, so dynamic mode there needs an index whatever mcp_enabled says")

	_, err = mcpserversrepo.New(db).DeleteMCPServer(ctx, mcpserversrepo.DeleteMCPServerParams{ID: server.ID, ProjectID: project.ID})
	require.NoError(t, err)
	require.ErrorIs(t, toolsets.TriggerToolsetIndex(ctx, logger, db, nil, toolsetView(fronted)), toolsets.ErrToolsetIndexNotRequired,
		"a deleted server serves nothing")

	createTriggerIndexMCPServer(t, db, project.ID, uuid.New(), fronted.ID, "disabled")
	require.ErrorIs(t, toolsets.TriggerToolsetIndex(ctx, logger, db, nil, toolsetView(fronted)), toolsets.ErrToolsetIndexNotRequired,
		"a disabled server serves nothing")
}

func TestTriggerToolsetIndexIgnoresTheToolsetsOwnHostedAddress(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	logger := testenv.NewLogger(t)
	db, err := infra.CloneTestDatabase(t, "trigger_toolset_index_own_address")
	require.NoError(t, err)
	project := createTriggerIndexProject(t, db)

	toolset := createTriggerIndexToolset(t, db, project, "own-address", false)
	require.ErrorIs(t, toolsets.TriggerToolsetIndex(ctx, logger, db, nil, toolsetView(toolset)), toolsets.ErrToolsetIndexNotRequired,
		"nothing fronts a toolset that is not MCP-enabled")

	// The row sharing the toolset's id is served only while mcp_enabled is
	// true, so it does not make this toolset served.
	createTriggerIndexMCPServer(t, db, project.ID, toolset.ID, toolset.ID, "private")
	require.ErrorIs(t, toolsets.TriggerToolsetIndex(ctx, logger, db, nil, toolsetView(toolset)), toolsets.ErrToolsetIndexNotRequired,
		"the toolset's own hosted address is gated on mcp_enabled")
}

func createTriggerIndexProject(t *testing.T, db *pgxpool.Pool) projectsrepo.Project {
	t.Helper()

	project, err := projectsrepo.New(db).CreateProject(t.Context(), projectsrepo.CreateProjectParams{
		Name:           "Trigger Index Project",
		Slug:           "trigger-index-" + uuid.NewString()[:8],
		OrganizationID: "org-" + uuid.NewString()[:8],
	})
	require.NoError(t, err)
	return project
}

func createTriggerIndexToolset(t *testing.T, db *pgxpool.Pool, project projectsrepo.Project, slug string, mcpEnabled bool) toolsetsrepo.Toolset {
	t.Helper()

	toolset, err := toolsetsrepo.New(db).CreateToolset(t.Context(), toolsetsrepo.CreateToolsetParams{
		OrganizationID:         project.OrganizationID,
		ProjectID:              project.ID,
		Name:                   slug,
		Slug:                   slug,
		Description:            pgtype.Text{},
		DefaultEnvironmentSlug: pgtype.Text{},
		McpSlug:                pgtype.Text{},
		McpEnabled:             mcpEnabled,
	})
	require.NoError(t, err)
	return toolset
}

func createTriggerIndexMCPServer(t *testing.T, db *pgxpool.Pool, projectID, serverID, toolsetID uuid.UUID, visibility string) mcpserversrepo.McpServer {
	t.Helper()

	server, err := mcpserversrepo.New(db).CreateMCPServer(t.Context(), mcpserversrepo.CreateMCPServerParams{
		ID:                    serverID,
		ProjectID:             projectID,
		Name:                  pgtype.Text{},
		Slug:                  pgtype.Text{String: "server-" + uuid.NewString()[:8], Valid: true},
		EnvironmentID:         uuid.NullUUID{},
		UserSessionIssuerID:   uuid.NullUUID{},
		RemoteMcpServerID:     uuid.NullUUID{},
		TunneledMcpServerID:   uuid.NullUUID{},
		ToolsetID:             uuid.NullUUID{UUID: toolsetID, Valid: true},
		UnproxiedMcpServerID:  uuid.NullUUID{},
		ToolVariationsGroupID: uuid.NullUUID{},
		Visibility:            visibility,
		NetworkAccessMode:     pgtype.Text{},
	})
	require.NoError(t, err)
	return server
}

// toolsetView is the slice of the toolset view TriggerToolsetIndex reads: the
// ids, the stored mcp_enabled flag, and a non-empty tool list.
func toolsetView(toolset toolsetsrepo.Toolset) *types.Toolset {
	return &types.Toolset{
		ID:         toolset.ID.String(),
		ProjectID:  toolset.ProjectID.String(),
		McpEnabled: &toolset.McpEnabled,
		Tools:      []*types.Tool{{}},
	}
}
