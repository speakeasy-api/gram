package mcp_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	platformusersessions "github.com/speakeasy-api/gram/server/internal/platformtools/usersessions"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// A platform tool reached through a gateway runs in its member's project.
// Agent gateway and organization keys name no project, and a caller bound to
// another project of the organization reaches the member only because member
// admission authorized it, so neither binding may reach the platform runtime.
func TestServePublic_MetaEndpoint_ExecutesPlatformToolInMemberProject(t *testing.T) {
	t.Parallel()

	for _, binding := range []string{"organization", "other project"} {
		t.Run(binding, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestMCPService(t)
			owner, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			require.NotNil(t, owner.ProjectID)

			slug := "meta-" + uuid.NewString()
			meta := createMetaMcpEndpoint(t, ctx, ti.conn, *owner.ProjectID, owner.ActiveOrganizationID, slug, uuid.Nil)

			toolset := createPublicMCPToolset(t, ctx, ti, owner, "platform-member-"+uuid.NewString()[:8])
			descriptor := platformusersessions.NewListUserSessionsTool(ti.conn).Descriptor()
			_, err := toolsetsrepo.New(ti.conn).CreateToolsetVersion(ctx, toolsetsrepo.CreateToolsetVersionParams{
				ToolsetID: toolset.ID, Version: 1,
				ToolUrns:     []urn.Tool{descriptor.ToolURN()},
				ResourceUrns: []urn.Resource{}, PredecessorID: uuid.NullUUID{},
			})
			require.NoError(t, err)

			serverID, err := uuid.NewV7()
			require.NoError(t, err)
			memberSlug := "platform-" + uuid.NewString()[:8]
			_, err = mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
				ID:                  serverID,
				ProjectID:           *owner.ProjectID,
				Name:                conv.ToPGText("platform member"),
				Slug:                conv.ToPGText(memberSlug),
				EnvironmentID:       uuid.NullUUID{UUID: uuid.Nil, Valid: false},
				UserSessionIssuerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
				RemoteMcpServerID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
				ToolsetID:           uuid.NullUUID{UUID: toolset.ID, Valid: true},
				Visibility:          mcpservers.VisibilityPublic,
			})
			require.NoError(t, err)
			_, err = metamcprepo.New(ti.conn).CreateMetaMCPMember(ctx, metamcprepo.CreateMetaMCPMemberParams{
				ProjectID:       *owner.ProjectID,
				MetaMcpServerID: meta.ID,
				McpServerID:     serverID,
				SortOrder:       0,
			})
			require.NoError(t, err)

			caller := *owner
			caller.ProjectID = nil
			caller.ProjectSlug = nil
			if binding == "other project" {
				other, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
					Name: "Other project", Slug: "other-" + uuid.NewString()[:8], OrganizationID: owner.ActiveOrganizationID,
				})
				require.NoError(t, err)
				caller.ProjectID = &other.ID
				caller.ProjectSlug = &other.Slug
			}
			wantProjectID := caller.ProjectID
			callerCtx := contextvalues.SetAuthContext(ctx, &caller)

			envelope := callMetaTool(t, callerCtx, ti, slug, "execute_tool", map[string]any{
				"name":      memberSlug + "--" + descriptor.Name,
				"arguments": map[string]any{},
			})
			text, isError := metaToolResultText(t, envelope)
			require.False(t, isError, text)
			require.Contains(t, text, "items")
			require.Equal(t, wantProjectID, caller.ProjectID, "member dispatch must not mutate the caller's project binding")
		})
	}
}
