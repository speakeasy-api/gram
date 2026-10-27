package assistants

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestUpdateAssistantWithoutUserEnablesHostedWrapperAsCreator(t *testing.T) {
	t.Parallel()
	db, err := assistantsInfra.CloneTestDatabase(t, "hosted_wrapper_actor")
	require.NoError(t, err)
	project := newProvisioningProject(t, db, "hosted-wrapper-actor")
	core := newProvisioningCore(t, db)
	record, err := core.CreateAssistant(t.Context(), "org-test", project, "user-1", "Background edits", "openai/gpt-4o-mini", "", nil, nil, 300, 1, StatusActive, true, nil)
	require.NoError(t, err)
	ts, err := toolsetsrepo.New(db).CreateToolset(t.Context(), toolsetsrepo.CreateToolsetParams{
		OrganizationID: "org-test", ProjectID: project, Name: "Linear", Slug: "linear",
		McpSlug: pgtype.Text{String: "org-test-linear-background", Valid: true}, McpEnabled: false,
	})
	require.NoError(t, err)
	syncCanonicalWrapper(t, db, ts)

	// No auth context: enabling the hosted wrapper is attributed to the assistant's creator.
	_, err = core.UpdateAssistant(t.Context(), project, record.ID, nil, nil, nil,
		[]*types.AssistantToolsetRef{{ToolsetSlug: ts.Slug, EnvironmentSlug: nil}}, nil, nil, nil, nil, allowAllMCPEnable)
	require.NoError(t, err)

	server, err := mcpserversrepo.New(db).GetMCPServerByIDAndProjectID(t.Context(), mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: ts.ID, ProjectID: project})
	require.NoError(t, err)
	require.Equal(t, "private", server.Visibility)

	created, err := audittest.LatestAuditLogByAction(t.Context(), db, audit.ActionMcpServerUpdate)
	require.NoError(t, err)
	require.Equal(t, "user-1", created.ActorID)
	require.Equal(t, ts.ID.String(), created.SubjectID)
}

func TestUpdateAssistantByAgentEnablesHostedWrapperAsAgent(t *testing.T) {
	t.Parallel()
	db, err := assistantsInfra.CloneTestDatabase(t, "hosted_wrapper_agent_actor")
	require.NoError(t, err)
	project := newProvisioningProject(t, db, "hosted-wrapper-agent-actor")
	core := newProvisioningCore(t, db)
	record, err := core.CreateAssistant(t.Context(), "org-test", project, "user-1", "Agent edits", "openai/gpt-4o-mini", "", nil, nil, 300, 1, StatusActive, true, nil)
	require.NoError(t, err)
	ts, err := toolsetsrepo.New(db).CreateToolset(t.Context(), toolsetsrepo.CreateToolsetParams{
		OrganizationID: "org-test", ProjectID: project, Name: "Linear", Slug: "linear",
		McpSlug: pgtype.Text{String: "org-test-linear-agent", Valid: true}, McpEnabled: false,
	})
	require.NoError(t, err)
	syncCanonicalWrapper(t, db, ts)

	agentID := uuid.NewString()
	ctx := agentContext(t.Context(), project, agentID)
	require.Empty(t, assistantActor(ctx, "user-1").UserID, "an agent write is not attributed to the creator")

	_, err = core.UpdateAssistant(ctx, project, record.ID, nil, nil, nil,
		[]*types.AssistantToolsetRef{{ToolsetSlug: ts.Slug, EnvironmentSlug: nil}}, nil, nil, nil, nil, allowAllMCPEnable)
	require.NoError(t, err)

	created, err := audittest.LatestAuditLogByAction(t.Context(), db, audit.ActionMcpServerUpdate)
	require.NoError(t, err)
	require.Equal(t, agentID, created.ActorID)
	require.Equal(t, string(urn.PrincipalTypeAgent), created.ActorType)
	require.Equal(t, ts.ID.String(), created.SubjectID)
}

func agentContext(ctx context.Context, projectID uuid.UUID, agentID string) context.Context {
	return contextvalues.WithAuthenticatedActor(ctx, &contextvalues.AuthContext{
		ActiveOrganizationID:  "org-test",
		UserID:                "",
		ExternalUserID:        "",
		APIKeyID:              "",
		SessionID:             nil,
		ProjectID:             &projectID,
		OrganizationSlug:      "org-test",
		Email:                 nil,
		AccountType:           "enterprise",
		HasActiveSubscription: false,
		Whitelisted:           false,
		ProjectSlug:           nil,
		APIKeyScopes:          nil,
		IsAdmin:               false,
	}, urn.NewPrincipal(urn.PrincipalTypeAgent, agentID))
}
