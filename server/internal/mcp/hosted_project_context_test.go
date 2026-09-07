package mcp_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	platformusersessions "github.com/speakeasy-api/gram/server/internal/platformtools/usersessions"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestHostedPlatformTool_ProjectContext(t *testing.T) {
	t.Parallel()

	for _, visibility := range []string{"public", "private"} {
		for _, binding := range []string{"organization", "matching", "conflicting", "foreign organization", "anonymous"} {
			t.Run(visibility+"/"+binding, func(t *testing.T) {
				t.Parallel()
				ctx, ti := newTestMCPService(t)
				owner, ok := contextvalues.GetAuthContext(ctx)
				require.True(t, ok)
				require.NotNil(t, owner.ProjectID)

				target := *owner
				if binding == "foreign organization" {
					target.ActiveOrganizationID = "test-foreign-organization"
					_, err := orgrepo.New(ti.conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
						ID: target.ActiveOrganizationID, Name: "Foreign organization", Slug: "foreign-organization",
						WorkosID: pgtype.Text{}, Whitelisted: pgtype.Bool{},
					})
					require.NoError(t, err)
					project, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
						Name: "Foreign project", Slug: "foreign-project", OrganizationID: target.ActiveOrganizationID,
					})
					require.NoError(t, err)
					target.ProjectID = &project.ID
				}

				toolsets := toolsetsrepo.New(ti.conn)
				toolset := createPublicMCPToolset(t, ctx, toolsets, &target, "project-context")
				descriptor := platformusersessions.NewListUserSessionsTool(ti.conn).Descriptor()
				_, err := toolsets.CreateToolsetVersion(ctx, toolsetsrepo.CreateToolsetVersionParams{
					ToolsetID: toolset.ID, Version: 1,
					ToolUrns:     []urn.Tool{descriptor.ToolURN()},
					ResourceUrns: []urn.Resource{}, PredecessorID: uuid.NullUUID{},
				})
				require.NoError(t, err)
				createToolsetMcpEndpoint(t, ctx, ti.conn, *target.ProjectID, toolset.ID, "project-context", visibility, uuid.NullUUID{}, uuid.Nil)

				keyOwner := *owner
				keyOwner.ProjectID = nil
				keyOwner.ProjectSlug = nil
				switch binding {
				case "matching":
					keyOwner.ProjectID = owner.ProjectID
				case "conflicting":
					other, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
						Name: "Other project", Slug: "other-project", OrganizationID: owner.ActiveOrganizationID,
					})
					require.NoError(t, err)
					keyOwner.ProjectID = &other.ID
				}

				requestCtx := t.Context()
				var key string
				if binding != "anonymous" {
					key = ti.createTestAPIKey(contextvalues.SetAuthContext(ctx, &keyOwner), t)
					request := httptest.NewRequest(http.MethodPost, "/mcp/project-context", nil)
					request.Header.Set("Authorization", "Bearer "+key)
					requestCtx, err = ti.service.TryPublicIdentityAuth(requestCtx, request, false, toolset.ID)
					require.NoError(t, err)
				}
				caller, _ := contextvalues.GetAuthContext(requestCtx)

				// Public requests exercise an already-authenticated context shared
				// with the caller; private requests authenticate the actual key.
				if visibility == "public" {
					key = ""
				}
				response, err := servePublicHTTP(t, requestCtx, ti, "project-context", makeToolsCallBody(descriptor.Name), key, nil)
				if binding == "conflicting" {
					require.ErrorContains(t, err, "api key project does not match toolset project")
				} else if visibility == "private" && (binding == "anonymous" || binding == "foreign organization") {
					require.Error(t, err)
				} else {
					require.NoError(t, err)
					require.Equal(t, http.StatusOK, response.Code)
					var rpc struct {
						Result json.RawMessage `json:"result"`
						Error  json.RawMessage `json:"error"`
					}
					require.NoError(t, json.Unmarshal(response.Body.Bytes(), &rpc))
					if binding == "anonymous" || binding == "foreign organization" {
						require.NotEmpty(t, rpc.Error)
						require.Empty(t, rpc.Result)
					} else {
						require.Empty(t, rpc.Error, "platform execution failed: %s", response.Body.String())
						require.Contains(t, string(rpc.Result), "items")
						require.NotContains(t, string(rpc.Result), `"isError":true`)
					}
				}
				if caller != nil {
					require.Equal(t, keyOwner.ProjectID, caller.ProjectID, "request must not mutate the caller's project binding")
				}
			})
		}
	}
}
