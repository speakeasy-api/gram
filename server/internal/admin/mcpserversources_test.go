package admin

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestProjectMCPServerSources_Ownership(t *testing.T) {
	for _, change := range []string{"wrong organization", "wrong project", "wrong server", "disabled organization", "deleted project", "deleted server", "deleted toolset", "foreign toolset"} {
		t.Run(change, func(t *testing.T) {
			t.Parallel()
			ctx, svc, conn := newTestAdminService(t)
			seedOrg(t, ctx, conn, orgFixture{id: "org_sources", name: "Sources", slug: "sources"})
			projectID := seedProject(t, ctx, conn, "org_sources", "sources")
			toolsetID := seedToolset(t, ctx, conn, "org_sources", projectID, "sources", false)
			serverID := seedMCPServer(t, ctx, conn, projectID, toolsetID, "sources")
			orgID := "org_sources"
			var query string
			var id any
			switch change {
			case "wrong organization":
				orgID = "org_other"
			case "wrong project":
				projectID = uuid.New()
			case "wrong server":
				serverID = uuid.New()
			case "disabled organization":
				query, id = "UPDATE organization_metadata SET disabled_at = now() WHERE id = $1", orgID
			case "deleted project":
				query, id = "UPDATE projects SET deleted_at = now() WHERE id = $1", projectID
			case "deleted server":
				query, id = "UPDATE mcp_servers SET deleted_at = now() WHERE id = $1", serverID
			case "deleted toolset":
				query, id = "UPDATE toolsets SET deleted_at = now() WHERE id = $1", toolsetID
			case "foreign toolset":
				other := seedProject(t, ctx, conn, orgID, "other")
				_, err := conn.Exec(ctx, "UPDATE toolsets SET project_id = $1 WHERE id = $2", other, toolsetID)
				require.NoError(t, err)
			}
			if query != "" {
				_, err := conn.Exec(ctx, query, id)
				require.NoError(t, err)
			}
			_, err := svc.projectMCPServerSources(ctx, orgID, projectID, serverID)
			require.Error(t, err)
			var publicErr *oops.ShareableError
			require.ErrorAs(t, err, &publicErr)
			require.Equal(t, oops.CodeNotFound, publicErr.Code)
		})
	}
}

func TestProjectMCPServerSources_EffectiveMembershipAndAssetOwnership(t *testing.T) {
	t.Parallel()
	ctx, svc, conn := newTestAdminService(t)
	const orgID = "org_sources"
	seedOrg(t, ctx, conn, orgFixture{id: orgID, name: "Sources", slug: "sources"})
	projectID := seedProject(t, ctx, conn, orgID, "sources")
	otherProject := seedProject(t, ctx, conn, orgID, "other")
	toolsetID := seedToolset(t, ctx, conn, orgID, projectID, "sources", false)
	serverID := seedMCPServer(t, ctx, conn, projectID, toolsetID, "sources")
	deploymentID := seedCompletedDeployment(t, ctx, conn, orgID, projectID)
	var selected []string
	var expected uuid.UUID
	for _, name := range []string{"selected", "excluded", "foreign", "deleted"} {
		assetID, documentID := uuid.New(), uuid.New()
		owner := projectID
		if name == "foreign" {
			owner = otherProject
		}
		_, err := conn.Exec(ctx, `INSERT INTO assets (id, project_id, name, url, kind, content_type, content_length, sha256) VALUES ($1,$2,$3,$4,'openapiv3','application/yaml',1,$3)`, assetID, owner, name, "s3://sources/"+name)
		require.NoError(t, err)
		if name == "deleted" {
			_, err = conn.Exec(ctx, "UPDATE assets SET deleted_at = now() WHERE id = $1", assetID)
			require.NoError(t, err)
		}
		_, err = conn.Exec(ctx, `INSERT INTO deployments_openapiv3_assets (id, deployment_id, asset_id, name, slug) VALUES ($1,$2,$3,$4,$4)`, documentID, deploymentID, assetID, name)
		require.NoError(t, err)
		for _, suffix := range []string{"one", "two"} {
			toolName := name + "_" + suffix
			toolURN := "tools:http:sources:" + toolName
			_, err = conn.Exec(ctx, `INSERT INTO http_tool_definitions (project_id, deployment_id, openapiv3_document_id, tool_urn, name, summary, description, server_env_var, http_method, path, schema_version, schema) VALUES ($1,$2,$3,$4,$5,'','','SERVER_URL','GET','/','1','{}')`, projectID, deploymentID, documentID, toolURN, toolName)
			require.NoError(t, err)
			if name != "excluded" {
				selected = append(selected, toolURN)
			}
		}
		if name == "selected" {
			expected = assetID
		}
	}
	_, err := conn.Exec(ctx, `INSERT INTO toolset_versions (toolset_id, version, tool_urns) VALUES ($1,1,$2)`, toolsetID, selected)
	require.NoError(t, err)
	sources, err := svc.projectMCPServerSources(ctx, orgID, projectID, serverID)
	require.NoError(t, err)
	require.Equal(t, []adminOpenAPISource{{AssetID: expected, Name: "selected", URL: "s3://sources/selected", ContentType: "application/yaml"}}, sources)
	_, err = conn.Exec(ctx, `INSERT INTO toolset_versions (toolset_id, version, tool_urns) VALUES ($1,2,'{}')`, toolsetID)
	require.NoError(t, err)
	sources, err = svc.projectMCPServerSources(ctx, orgID, projectID, serverID)
	require.NoError(t, err)
	require.Empty(t, sources)
}

func TestProjectMCPServerSources_NonToolsetBackend(t *testing.T) {
	t.Parallel()
	ctx, svc, conn := newTestAdminService(t)
	const orgID = "org_sources"
	seedOrg(t, ctx, conn, orgFixture{id: orgID, name: "Sources", slug: "sources"})
	projectID := seedProject(t, ctx, conn, orgID, "sources")
	serverID := uuid.New()
	_, err := conn.Exec(ctx, `INSERT INTO mcp_servers (id, project_id, builtin_backend, visibility) VALUES ($1,$2,'approvals','private')`, serverID, projectID)
	require.NoError(t, err)
	sources, err := svc.projectMCPServerSources(ctx, orgID, projectID, serverID)
	require.NoError(t, err)
	require.Empty(t, sources)
}
