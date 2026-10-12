package assistants

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

func TestUpdateAssistantWithoutUserEnablesHostedWrapperAsCreator(t *testing.T) {
	t.Parallel()
	db, err := assistantsInfra.CloneTestDatabase(t, "hosted_wrapper_actor")
	require.NoError(t, err)
	project := newProvisioningProject(t, db, "hosted-wrapper-actor")
	core := newProvisioningCore(t, db)
	record, err := core.CreateAssistant(t.Context(), "org-test", project, "user-1", "Background edits", "openai/gpt-4o-mini", "", nil, nil, 300, 1, StatusActive, true)
	require.NoError(t, err)
	ts, err := toolsetsrepo.New(db).CreateToolset(t.Context(), toolsetsrepo.CreateToolsetParams{
		OrganizationID: "org-test", ProjectID: project, Name: "Linear", Slug: "linear",
		McpSlug: pgtype.Text{String: "org-test-linear-background", Valid: true}, McpEnabled: false,
	})
	require.NoError(t, err)

	// No auth context: the hosted wrapper sync is attributed to the assistant's creator.
	_, err = core.UpdateAssistant(t.Context(), project, record.ID, nil, nil, nil,
		[]*types.AssistantToolsetRef{{ToolsetSlug: ts.Slug, EnvironmentSlug: nil}}, nil, nil, nil, nil)
	require.NoError(t, err)

	server, err := mcpserversrepo.New(db).GetMCPServerByIDAndProjectID(t.Context(), mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: ts.ID, ProjectID: project})
	require.NoError(t, err)
	require.Equal(t, "private", server.Visibility)

	created, err := audittest.LatestAuditLogByAction(t.Context(), db, audit.ActionMcpServerCreate)
	require.NoError(t, err)
	require.Equal(t, "user-1", created.ActorID)
	require.Equal(t, ts.ID.String(), created.SubjectID)
}
