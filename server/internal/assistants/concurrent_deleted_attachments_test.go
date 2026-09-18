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
	assistantsrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

func TestServiceConcurrentDeletedAttachments(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"toolset", "mcp_server"} {
		for _, mutation := range []string{"create", "update"} {
			t.Run(kind+"/"+mutation, func(t *testing.T) {
				t.Parallel()
				svc, ctx, projectID, conn := newRBACServiceWithConn(t, "concurrent_deleted_"+kind+"_"+mutation)
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

				// The deletion owns the target row before attachment validation starts.
				// Validation must wait, then recheck the committed deletion rather than
				// writing attachments from the previously visible version of the target.
				tx := testenv.BeginTx(t, ctx, conn)
				if kind == "toolset" {
					_, err = toolsetsrepo.New(tx).DeleteToolset(ctx, toolsetsrepo.DeleteToolsetParams{ProjectID: projectID, Slug: ts.Slug})
				} else {
					_, err = mcpserversrepo.New(tx).DeleteMCPServer(ctx, mcpserversrepo.DeleteMCPServerParams{ProjectID: projectID, ID: targetID})
				}
				require.NoError(t, err)
				finished := make(chan error, 1)
				defer func() {
					cancel()
					for range finished {
					}
				}()
				go func() {
					defer close(finished)
					if mutation == "create" {
						_, err := svc.CreateAssistant(ctx, payload)
						finished <- err
					} else {
						name := "Changed"
						_, err := svc.UpdateAssistant(ctx, &gen.UpdateAssistantPayload{
							ID: assistantID, Name: &name, Toolsets: payload.Toolsets, McpServers: payload.McpServers,
						})
						finished <- err
					}
				}()
				testenv.WaitForBlockedBackend(t, ctx, conn)
				require.NoError(t, tx.Commit(ctx))
				select {
				case err := <-finished:
					requireOopsCode(t, err, oops.CodeBadRequest)
				case <-ctx.Done():
					t.Fatal("assistant mutation did not finish after deletion committed")
				}
				listed, err := svc.ListAssistants(ctx, &gen.ListAssistantsPayload{})
				require.NoError(t, err)
				if mutation == "create" {
					require.Empty(t, listed.Assistants)
				} else {
					counts, err := testrepo.New(conn).CountAssistantAttachments(ctx, testrepo.CountAssistantAttachmentsParams{
						ProjectID: projectID, AssistantID: uuid.MustParse(assistantID),
					})
					require.NoError(t, err)
					require.Len(t, listed.Assistants, 1)
					require.Equal(t, "Original", listed.Assistants[0].Name)
					if kind == "toolset" {
						require.EqualValues(t, 1, counts.Toolsets)
						require.Zero(t, counts.McpServers)
						require.Len(t, listed.Assistants[0].Toolsets, 1)
						require.Equal(t, "live-tools", listed.Assistants[0].Toolsets[0].ToolsetSlug)
						require.Empty(t, listed.Assistants[0].McpServers)
					} else {
						require.Zero(t, counts.Toolsets)
						require.EqualValues(t, 1, counts.McpServers)
						require.Empty(t, listed.Assistants[0].Toolsets)
						require.Len(t, listed.Assistants[0].McpServers, 1)
						require.Equal(t, "live-server", listed.Assistants[0].McpServers[0].McpServerSlug)
					}
				}
			})
		}
	}
}

func TestServiceMixedAttachmentsConcurrentMCPBackendUpdate(t *testing.T) {
	t.Parallel()
	svc, ctx, projectID, conn := newRBACServiceWithConn(t, "mixed_attachments_backend_update")
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeProjectWrite,
		Selector: authz.NewSelector(authz.ScopeProjectWrite, projectID.String()),
	})
	toolsets := toolsetsrepo.New(conn)
	original, err := toolsets.CreateToolset(ctx, toolsetsrepo.CreateToolsetParams{
		OrganizationID: "org-test", ProjectID: projectID, Name: "Original tools", Slug: "original-tools",
	})
	require.NoError(t, err)
	target, err := toolsets.CreateToolset(ctx, toolsetsrepo.CreateToolsetParams{
		OrganizationID: "org-test", ProjectID: projectID, Name: "Target tools", Slug: "target-tools",
		McpSlug: pgtype.Text{String: "target-tools-endpoint", Valid: true}, McpEnabled: true,
	})
	require.NoError(t, err)
	server, err := mcpserversrepo.New(conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID: uuid.New(), ProjectID: projectID, Name: pgtype.Text{String: "Example server", Valid: true},
		Slug:      pgtype.Text{String: "example-server", Valid: true},
		ToolsetID: uuid.NullUUID{UUID: original.ID, Valid: true}, Visibility: "private",
	})
	require.NoError(t, err)
	_, err = mcpendpointsrepo.New(conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID: projectID, McpServerID: uuid.NullUUID{UUID: server.ID, Valid: true}, Slug: "example-server-endpoint",
	})
	require.NoError(t, err)

	tx := testenv.BeginTx(t, ctx, conn)
	servers := mcpserversrepo.New(tx)
	_, err = servers.LockMCPServerByIDAndProjectID(ctx, mcpserversrepo.LockMCPServerByIDAndProjectIDParams{
		ID: server.ID, ProjectID: projectID,
	})
	require.NoError(t, err)
	finished := make(chan error, 1)
	defer func() {
		cancel()
		for range finished {
		}
	}()
	go func() {
		defer close(finished)
		_, err := svc.CreateAssistant(ctx, &gen.CreateAssistantPayload{
			Name: "Mixed attachments", Model: "openai/gpt-4o-mini",
			Toolsets:   []*types.AssistantToolsetRef{{ToolsetSlug: target.Slug}},
			McpServers: []*types.AssistantMCPServerRef{{McpServerSlug: server.Slug.String}},
		})
		finished <- err
	}()
	// The assistant has locked the target toolset and now waits on this server.
	testenv.WaitForBlockedBackend(t, ctx, conn)
	updateCtx, cancelUpdate := context.WithTimeout(ctx, 5*time.Second)
	defer cancelUpdate()
	// Switching from a distinct backend forces the FK's KEY SHARE check on
	// target. The assistant's toolset lock must remain compatible with it.
	_, err = servers.UpdateMCPServer(updateCtx, mcpserversrepo.UpdateMCPServerParams{
		ID: server.ID, ProjectID: projectID, Name: server.Name, Slug: server.Slug,
		ToolsetID: uuid.NullUUID{UUID: target.ID, Valid: true}, Visibility: server.Visibility,
	})
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))
	select {
	case err := <-finished:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal("assistant creation did not finish after backend update committed")
	}
	listed, err := svc.ListAssistants(ctx, &gen.ListAssistantsPayload{})
	require.NoError(t, err)
	require.Len(t, listed.Assistants, 1)
	require.Len(t, listed.Assistants[0].Toolsets, 1)
	require.Len(t, listed.Assistants[0].McpServers, 1)
}

func TestResolveMcpServersForWriteAllowsEndpointForeignKey(t *testing.T) {
	t.Parallel()
	_, ctx, projectID, conn := newRBACServiceWithConn(t, "resolve_server_endpoint_fk")
	ts, err := toolsetsrepo.New(conn).CreateToolset(ctx, toolsetsrepo.CreateToolsetParams{
		OrganizationID: "org-test", ProjectID: projectID, Name: "Example tools", Slug: "example-tools",
	})
	require.NoError(t, err)
	server, err := mcpserversrepo.New(conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID: uuid.New(), ProjectID: projectID, Name: pgtype.Text{String: "Example server", Valid: true},
		Slug: pgtype.Text{String: "example-server", Valid: true}, Visibility: "private", ToolsetID: uuid.NullUUID{UUID: ts.ID, Valid: true},
	})
	require.NoError(t, err)
	tx := testenv.BeginTx(t, ctx, conn)
	resolved, err := assistantsrepo.New(tx).ResolveMcpServersForWrite(ctx, assistantsrepo.ResolveMcpServersForWriteParams{
		ProjectID: projectID, Slugs: []string{server.Slug.String},
	})
	require.NoError(t, err)
	require.Len(t, resolved, 1)

	// A separate transaction's FK KEY SHARE check must not wait on the resolver.
	insertCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	_, err = mcpendpointsrepo.New(conn).CreateMCPEndpoint(insertCtx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID: projectID, McpServerID: uuid.NullUUID{UUID: server.ID, Valid: true}, Slug: "example-server-endpoint",
	})
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))
}
