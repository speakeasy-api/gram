package assistants

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/assistants"
	"github.com/speakeasy-api/gram/server/gen/types"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

func TestServiceDeletionWaitsForAssistantAttachments(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"toolset", "mcp_server"} {
		for _, mutation := range []string{"create", "update"} {
			t.Run(kind+"/"+mutation, func(t *testing.T) {
				t.Parallel()
				svc, ctx, projectID, conn := newRBACServiceWithConn(t, "deletion_waits_"+kind+"_"+mutation)
				ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
				defer cancel()
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
				var assistantID string
				if mutation == "update" {
					live, err := toolsetsrepo.New(conn).CreateToolset(ctx, toolsetsrepo.CreateToolsetParams{
						OrganizationID: "org-test", ProjectID: projectID, Name: "Live tools", Slug: "live-tools",
						McpSlug: pgtype.Text{String: "live-tools-endpoint", Valid: true}, McpEnabled: true,
					})
					require.NoError(t, err)
					original := &gen.CreateAssistantPayload{Name: "Original", Model: payload.Model}
					if kind == "toolset" {
						original.Toolsets = []*types.AssistantToolsetRef{{ToolsetSlug: live.Slug}}
					} else {
						server, err := mcpserversrepo.New(conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
							ID: uuid.New(), ProjectID: projectID, Name: pgtype.Text{String: "Live server", Valid: true},
							Slug:      pgtype.Text{String: "live-server", Valid: true},
							ToolsetID: uuid.NullUUID{UUID: live.ID, Valid: true}, Visibility: "private",
						})
						require.NoError(t, err)
						_, err = mcpendpointsrepo.New(conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
							ProjectID: projectID, McpServerID: uuid.NullUUID{UUID: server.ID, Valid: true}, Slug: "live-server-endpoint",
						})
						require.NoError(t, err)
						original.McpServers = []*types.AssistantMCPServerRef{{McpServerSlug: server.Slug.String}}
					}
					existing, err := svc.CreateAssistant(ctx, original)
					require.NoError(t, err)
					assistantID = existing.ID
				}

				// Use the production resolvers and writers in an explicit transaction
				// so deletion can start after validation but before the attachment exists.
				tx := testenv.BeginTx(t, ctx, conn)
				resolved, err := svc.core.resolveToolsetRefsForWrite(ctx, tx, projectID, payload.Toolsets)
				require.NoError(t, err)
				resolvedServers, err := svc.core.resolveMcpServerRefsForWrite(ctx, tx, projectID, payload.McpServers)
				require.NoError(t, err)

				// Mirror the deletion services' target-lock/delete/detach sequence.
				// Keep detach as a separate statement so it sees the assistant commit.
				deletionTx := testenv.BeginTx(t, ctx, conn)
				finished := make(chan error, 1)
				defer func() {
					cancel()
					for range finished {
					}
				}()
				go func() {
					defer close(finished)
					var err error
					if kind == "toolset" {
						queries := toolsetsrepo.New(deletionTx)
						_, err = queries.DeleteToolset(ctx, toolsetsrepo.DeleteToolsetParams{ProjectID: projectID, Slug: ts.Slug})
						if err == nil {
							err = queries.DeleteAssistantToolsetsByToolset(ctx, toolsetsrepo.DeleteAssistantToolsetsByToolsetParams{ProjectID: projectID, ToolsetID: targetID})
						}
					} else {
						queries := mcpserversrepo.New(deletionTx)
						_, err = queries.LockMCPServerByIDAndProjectID(ctx, mcpserversrepo.LockMCPServerByIDAndProjectIDParams{ProjectID: projectID, ID: targetID})
						if err == nil {
							_, err = queries.DeleteMCPServer(ctx, mcpserversrepo.DeleteMCPServerParams{ProjectID: projectID, ID: targetID})
						}
						if err == nil {
							err = queries.DeleteAssistantMCPServersByMCPServer(ctx, mcpserversrepo.DeleteAssistantMCPServersByMCPServerParams{ProjectID: projectID, McpServerID: targetID})
						}
					}
					if err == nil {
						err = deletionTx.Commit(ctx)
					}
					finished <- err
				}()
				// No attachment or assistant row lock exists yet: deletion must
				// be waiting specifically on the resolver's target lock.
				testenv.WaitForBlockedBackend(t, ctx, conn)

				queries := assistantrepo.New(tx)
				if mutation == "create" {
					created, err := queries.CreateAssistant(ctx, assistantrepo.CreateAssistantParams{
						ProjectID: projectID, OrganizationID: "org-test", Name: "Changed", Model: payload.Model,
						CreatedByUserID: pgtype.Text{String: "user-test", Valid: true},
						WarmTtlSeconds:  300, MaxConcurrency: 1, Status: StatusActive,
					})
					require.NoError(t, err)
					assistantID = created.ID.String()
				} else {
					_, err := queries.UpdateAssistant(ctx, assistantrepo.UpdateAssistantParams{
						ProjectID: projectID, AssistantID: uuid.MustParse(assistantID),
						Name: pgtype.Text{String: "Changed", Valid: true},
					})
					require.NoError(t, err)
				}
				id := uuid.MustParse(assistantID)
				require.NoError(t, writeAssistantToolsets(ctx, tx, id, projectID, resolved))
				require.NoError(t, writeAssistantMcpServers(ctx, tx, id, projectID, resolvedServers))
				counts, err := testrepo.New(tx).CountAssistantAttachments(ctx, testrepo.CountAssistantAttachmentsParams{
					ProjectID: projectID, AssistantID: id,
				})
				require.NoError(t, err)
				if kind == "toolset" {
					require.EqualValues(t, 1, counts.Toolsets)
				} else {
					require.EqualValues(t, 1, counts.McpServers)
				}
				require.NoError(t, tx.Commit(ctx))
				select {
				case err := <-finished:
					require.NoError(t, err)
				case <-ctx.Done():
					t.Fatal("deletion did not finish after assistant committed")
				}
				// Check physical rows as well as the API view: hydration filters
				// deleted targets and could otherwise hide a dangling attachment.
				counts, err = testrepo.New(conn).CountAssistantAttachments(ctx, testrepo.CountAssistantAttachmentsParams{
					ProjectID: projectID, AssistantID: id,
				})
				require.NoError(t, err)
				require.Zero(t, counts.Toolsets)
				require.Zero(t, counts.McpServers)
				listed, err := svc.ListAssistants(ctx, &gen.ListAssistantsPayload{})
				require.NoError(t, err)
				require.Len(t, listed.Assistants, 1)
				require.Equal(t, assistantID, listed.Assistants[0].ID)
				require.Equal(t, "Changed", listed.Assistants[0].Name)
				require.Empty(t, listed.Assistants[0].Toolsets)
				require.Empty(t, listed.Assistants[0].McpServers)
			})
		}
	}
}
