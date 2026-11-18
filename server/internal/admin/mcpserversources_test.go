package admin

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	deploymentsrepo "github.com/speakeasy-api/gram/server/internal/deployments/repo"
	mcpregistryrepo "github.com/speakeasy-api/gram/server/internal/mcpregistry/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestProjectMCPServerSources_Ownership(t *testing.T) {
	t.Parallel()
	for _, change := range []string{"wrong organization", "wrong project", "wrong server", "disabled organization", "deleted project", "deleted server", "deleted toolset", "foreign toolset"} {
		t.Run(change, func(t *testing.T) {
			t.Parallel()
			ctx, svc, conn := newTestAdminService(t)
			seedOrg(t, ctx, conn, orgFixture{id: "org_sources", name: "Sources", slug: "sources"})
			projectID := seedProject(t, ctx, conn, "org_sources", "sources")
			toolsetProjectID := projectID
			if change == "foreign toolset" {
				toolsetProjectID = seedProject(t, ctx, conn, "org_sources", "other")
			}
			toolsetID := seedToolset(t, ctx, conn, "org_sources", toolsetProjectID, "sources", false)
			serverID := seedMCPServer(t, ctx, conn, projectID, toolsetID, "sources")
			orgID := "org_sources"
			switch change {
			case "wrong organization":
				orgID = "org_other"
			case "wrong project":
				projectID = uuid.New()
			case "wrong server":
				serverID = uuid.New()
			case "disabled organization":
				_, err := repo.New(conn).AdminDisableOrganization(ctx, orgID)
				require.NoError(t, err)
			case "deleted project":
				_, err := projectsrepo.New(conn).DeleteProject(ctx, projectID)
				require.NoError(t, err)
			case "deleted server":
				deleteMCPServer(t, ctx, conn, projectID, serverID)
			case "deleted toolset":
				_, err := toolsetsrepo.New(conn).DeleteToolset(ctx, toolsetsrepo.DeleteToolsetParams{ProjectID: projectID, Slug: "sources"})
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
		assetID := uuid.New()
		owner := projectID
		if name == "foreign" {
			owner = otherProject
		}
		err := testrepo.New(conn).InsertDeploymentAssetFixture(ctx, testrepo.InsertDeploymentAssetFixtureParams{ID: assetID, ProjectID: uuid.NullUUID{UUID: owner, Valid: true}, Name: name, Url: "s3://sources/" + name, Kind: "openapiv3", ContentType: "application/yaml", Sha256: name})
		require.NoError(t, err)
		if name == "deleted" {
			err = mcpregistryrepo.New(conn).SoftDeleteAssetFixture(ctx, assetID)
			require.NoError(t, err)
		}
		document, err := deploymentsrepo.New(conn).UpsertDeploymentOpenAPIv3Asset(ctx, deploymentsrepo.UpsertDeploymentOpenAPIv3AssetParams{DeploymentID: deploymentID, AssetID: assetID, Name: name, Slug: name})
		require.NoError(t, err)
		for _, suffix := range []string{"one", "two"} {
			toolName := name + "_" + suffix
			toolURN := "tools:http:sources:" + toolName
			tool, err := urn.ParseTool(toolURN)
			require.NoError(t, err)
			_, err = deploymentsrepo.New(conn).CreateOpenAPIv3ToolDefinition(ctx, deploymentsrepo.CreateOpenAPIv3ToolDefinitionParams{ProjectID: projectID, DeploymentID: deploymentID, Openapiv3DocumentID: uuid.NullUUID{UUID: document.ID, Valid: true}, ToolUrn: tool, Name: toolName, ServerEnvVar: "SERVER_URL", HttpMethod: "GET", Path: "/", SchemaVersion: "1", Schema: []byte("{}"), Tags: []string{}, Security: []byte("[]"), HeaderSettings: []byte("{}"), QuerySettings: []byte("{}"), PathSettings: []byte("{}")})
			require.NoError(t, err)
			if name != "excluded" {
				selected = append(selected, toolURN)
			}
		}
		if name == "selected" {
			expected = assetID
		}
	}
	err := testrepo.New(conn).InsertToolsetVersionFixture(ctx, testrepo.InsertToolsetVersionFixtureParams{ToolsetID: toolsetID, ProjectID: projectID, OrganizationID: orgID, Version: 1, ToolUrns: selected})
	require.NoError(t, err)
	sources, err := svc.projectMCPServerSources(ctx, orgID, projectID, serverID)
	require.NoError(t, err)
	require.Equal(t, []adminOpenAPISource{{AssetID: expected, Name: "selected", URL: "s3://sources/selected", ContentType: "application/yaml"}}, sources)
	err = testrepo.New(conn).InsertToolsetVersionFixture(ctx, testrepo.InsertToolsetVersionFixtureParams{ToolsetID: toolsetID, ProjectID: projectID, OrganizationID: orgID, Version: 2, ToolUrns: []string{}})
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
	err := testrepo.New(conn).InsertRetiredApprovalsServerFixture(ctx, testrepo.InsertRetiredApprovalsServerFixtureParams{ID: serverID, ProjectID: projectID})
	require.NoError(t, err)
	sources, err := svc.projectMCPServerSources(ctx, orgID, projectID, serverID)
	require.NoError(t, err)
	require.Empty(t, sources)
}
