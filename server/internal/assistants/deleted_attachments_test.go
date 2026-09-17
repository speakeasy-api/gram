package assistants

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/assistants"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

func TestServiceLegacyDeletedAttachments(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"toolset", "mcp_server"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			svc, ctx, projectID, conn := newRBACServiceWithConn(t, "legacy_deleted_"+kind)
			ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
				Scope:    authz.ScopeProjectWrite,
				Selector: authz.NewSelector(authz.ScopeProjectWrite, projectID.String()),
			})
			ts, err := toolsetsrepo.New(conn).CreateToolset(ctx, toolsetsrepo.CreateToolsetParams{
				OrganizationID: "org-test", ProjectID: projectID, Name: "Example tools", Slug: "example-tools",
				McpSlug: pgtype.Text{String: "example-tools-endpoint", Valid: true}, McpEnabled: true,
			})
			require.NoError(t, err)
			payload := &gen.CreateAssistantPayload{Name: "Assistant", Model: "openai/gpt-4o-mini"}
			targetID := ts.ID
			if kind == "toolset" {
				payload.Toolsets = []*types.AssistantToolsetRef{{ToolsetSlug: ts.Slug}}
			} else {
				server, err := mcpserversrepo.New(conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
					ID: uuid.New(), ProjectID: projectID, Name: pgtype.Text{String: "Example server", Valid: true},
					Slug:      pgtype.Text{String: "example-server", Valid: true},
					ToolsetID: uuid.NullUUID{UUID: ts.ID, Valid: true}, Visibility: "private",
				})
				require.NoError(t, err)
				_, err = mcpendpointsrepo.New(conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
					ProjectID: projectID, McpServerID: uuid.NullUUID{UUID: server.ID, Valid: true}, Slug: "example-server-endpoint",
				})
				require.NoError(t, err)
				targetID = server.ID
				payload.McpServers = []*types.AssistantMCPServerRef{{McpServerSlug: server.Slug.String}}
			}
			created, err := svc.CreateAssistant(ctx, payload)
			require.NoError(t, err)
			require.Equal(t, 1, len(created.Toolsets)+len(created.McpServers))

			// Reproduce attachments left behind by historical soft deletions.
			if kind == "toolset" {
				_, err = toolsetsrepo.New(conn).DeleteToolset(ctx, toolsetsrepo.DeleteToolsetParams{Slug: ts.Slug, ProjectID: projectID})
			} else {
				_, err = mcpserversrepo.New(conn).DeleteMCPServer(ctx, mcpserversrepo.DeleteMCPServerParams{ID: targetID, ProjectID: projectID})
			}
			require.NoError(t, err)

			reloaded, err := svc.GetAssistant(ctx, &gen.GetAssistantPayload{ID: created.ID})
			require.NoError(t, err)
			require.Empty(t, reloaded.Toolsets)
			require.Empty(t, reloaded.McpServers)
			listed, err := svc.ListAssistants(ctx, &gen.ListAssistantsPayload{})
			require.NoError(t, err)
			require.Len(t, listed.Assistants, 1)
			require.Empty(t, listed.Assistants[0].Toolsets)
			require.Empty(t, listed.Assistants[0].McpServers)

			name := "Updated assistant"
			updated, err := svc.UpdateAssistant(ctx, &gen.UpdateAssistantPayload{
				ID: reloaded.ID, Name: &name, Toolsets: reloaded.Toolsets, McpServers: reloaded.McpServers,
			})
			require.NoError(t, err)
			require.Equal(t, name, updated.Name)
			// Defensive reads must not weaken validation of explicit new attachments.
			_, err = svc.UpdateAssistant(ctx, &gen.UpdateAssistantPayload{
				ID: created.ID, Toolsets: payload.Toolsets, McpServers: payload.McpServers,
			})
			requireOopsCode(t, err, oops.CodeBadRequest)
		})
	}
}
