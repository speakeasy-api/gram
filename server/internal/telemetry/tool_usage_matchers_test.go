package telemetry_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	mcpendpointsRepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	toolsetsRepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

func TestLoadToolUsageMatchers_CoverAliasAndEveryEndpoint(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := *authCtx.ProjectID

	// A legacy toolset whose canonical server gained a second address.
	legacyServer := createHostedMCPServerFixture(t, ctx, ti, "Matcher Legacy", "matcher-legacy")
	endpoints := mcpendpointsRepo.New(ti.conn)
	for _, slug := range []string{"acme-matcher-legacy", "acme-matcher-legacy-second"} {
		_, err := endpoints.CreateMCPEndpoint(ctx, mcpendpointsRepo.CreateMCPEndpointParams{
			ProjectID: projectID, McpServerID: uuid.NullUUID{UUID: legacyServer, Valid: true}, Slug: slug,
		})
		require.NoError(t, err)
	}

	// A toolset created after the flip: no alias, an endpoint on its canonical server.
	flipped, err := toolsetsRepo.New(ti.conn).CreateToolset(ctx, toolsetsRepo.CreateToolsetParams{
		OrganizationID: authCtx.ActiveOrganizationID, ProjectID: projectID, Name: "Matcher Flipped", Slug: "matcher-flipped",
		Description: pgtype.Text{}, DefaultEnvironmentSlug: pgtype.Text{}, McpSlug: pgtype.Text{}, McpEnabled: false,
	})
	require.NoError(t, err)
	addToolsetWrapperWith(t, ctx, ti, flipped.ID, flipped.ID, "matcher-flipped-server", "private", true)

	hosted, _, err := telemetry.LoadToolUsageMatchers(ctx, ti.conn, projectID)
	require.NoError(t, err)
	slugsByToolset := map[string][]string{}
	for _, matcher := range hosted {
		slugsByToolset[matcher.ToolsetSlug] = append(slugsByToolset[matcher.ToolsetSlug], matcher.McpSlug)
	}
	require.ElementsMatch(t, []string{"acme-matcher-legacy", "acme-matcher-legacy-second"}, slugsByToolset["matcher-legacy"])
	require.Equal(t, []string{"matcher-flipped-server-endpoint"}, slugsByToolset["matcher-flipped"])
}
