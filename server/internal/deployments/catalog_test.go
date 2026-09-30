package deployments_test

import (
	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/deployments"
	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/deployments/repo"
	"github.com/speakeasy-api/gram/server/internal/externalmcp"
	"github.com/speakeasy-api/gram/server/internal/feature"
	registryrepo "github.com/speakeasy-api/gram/server/internal/mcpregistry/repo"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestDeploymentsService_RejectNewCatalogSelection(t *testing.T) {
	for _, method := range []string{"create", "evolve"} {
		t.Run(method, func(t *testing.T) {
			ctx, ti := newTestDeploymentService(t, assetstest.NewTestBlobStore(t))
			require.NoError(t, externalmcp.EnsureNativeCatalogSource(ctx, ti.conn))
			form := &gen.AddExternalMCPForm{RegistryID: new(externalmcp.NativeCatalogRegistryID.String()), Name: "Test", Slug: "test", RegistryServerSpecifier: "example/server"}
			var err error
			if method == "create" {
				_, err = ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{IdempotencyKey: uuid.NewString(), ExternalMcps: []*gen.AddExternalMCPForm{form}, NonBlocking: new(true)})
			} else {
				_, err = ti.service.Evolve(ctx, &gen.EvolvePayload{UpsertExternalMcps: []*gen.AddExternalMCPForm{form}, NonBlocking: new(true)})
			}
			require.ErrorIs(t, err, externalmcp.ErrCatalogSourceNotFound)
		})
	}
}

func TestDeploymentsService_EvolveRetainsCatalogIdentityAfterFlip(t *testing.T) {
	for _, upsert := range []bool{false, true} {
		t.Run(map[bool]string{false: "carried", true: "matching upsert"}[upsert], func(t *testing.T) {
			ctx, ti := newTestDeploymentService(t, assetstest.NewTestBlobStore(t))
			require.NoError(t, externalmcp.EnsureNativeCatalogSource(ctx, ti.conn))
			auth, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			ti.feature.SetFlag(feature.FlagGramMCPCatalog, auth.ActiveOrganizationID, true)
			// Seed accepted work without running a network-dependent extraction workflow.
			q := repo.New(ti.conn)
			depID, err := q.InsertDeployment(ctx, repo.InsertDeploymentParams{ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID, UserID: auth.UserID, IdempotencyKey: uuid.NewString()})
			require.NoError(t, err)
			require.NoError(t, q.CreateDeploymentStatus(ctx, repo.CreateDeploymentStatusParams{DeploymentID: depID, Status: "completed"}))
			_, err = q.UpsertDeploymentExternalMCP(ctx, repo.UpsertDeploymentExternalMCPParams{DeploymentID: depID, RegistryID: uuid.NullUUID{UUID: externalmcp.NativeCatalogRegistryID, Valid: true}, Name: "Test", Slug: "test", RegistryServerSpecifier: "example/server"})
			require.NoError(t, err)
			ti.feature.SetFlag(feature.FlagGramMCPCatalog, auth.ActiveOrganizationID, false)
			form := &gen.EvolvePayload{NonBlocking: new(true)}
			if upsert {
				form.UpsertExternalMcps = []*gen.AddExternalMCPForm{{RegistryID: new(externalmcp.NativeCatalogRegistryID.String()), Name: "Renamed", Slug: "test", RegistryServerSpecifier: "example/server"}}
			} else {
				// An unrelated exclusion forces a clone without reselecting the saved MCP.
				form.ExcludeExternalMcps = []string{"unrelated"}
			}
			result, err := ti.service.Evolve(ctx, form)
			require.NoError(t, err)
			attachments, err := q.ListDeploymentExternalMCPs(ctx, uuid.MustParse(result.Deployment.ID))
			require.NoError(t, err)
			require.Len(t, attachments, 1)
			require.Equal(t, externalmcp.NativeCatalogRegistryID, attachments[0].RegistryID.UUID)
			require.Equal(t, "example/server", attachments[0].RegistryServerSpecifier)
			// The same slug must not grandfather a different catalog server.
			_, err = ti.service.Evolve(ctx, &gen.EvolvePayload{NonBlocking: new(true), UpsertExternalMcps: []*gen.AddExternalMCPForm{{RegistryID: new(externalmcp.NativeCatalogRegistryID.String()), Name: "Other", Slug: "test", RegistryServerSpecifier: "example/other"}}})
			require.ErrorIs(t, err, externalmcp.ErrCatalogSourceNotFound)
		})
	}
}

func TestDeploymentsService_RejectUnpublishedNativeAttachment(t *testing.T) {
	for _, method := range []string{"create", "evolve"} {
		t.Run(method, func(t *testing.T) {
			ctx, ti := newTestDeploymentService(t, assetstest.NewTestBlobStore(t))
			require.NoError(t, externalmcp.EnsureNativeCatalogSource(ctx, ti.conn))
			auth, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			ti.feature.SetFlag(feature.FlagGramMCPCatalog, auth.ActiveOrganizationID, true)
			raw := []byte(`{"server":{"name":"io.example/selection","description":"Selection","version":"1.0.0","remotes":[{"type":"streamable-http","url":"https://example.com/mcp"}]},"_meta":{"io.modelcontextprotocol.registry/official":{"status":"active"}}}`)
			require.NoError(t, registryrepo.New(ti.conn).InsertRegistryEntryFixture(ctx, registryrepo.InsertRegistryEntryFixtureParams{ID: uuid.New(), Data: raw, Published: false}))
			form := &gen.AddExternalMCPForm{RegistryID: new(externalmcp.NativeCatalogRegistryID.String()), Name: "Selection", Slug: "selection", RegistryServerSpecifier: "io.example/selection"}
			var err error
			if method == "create" {
				_, err = ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{IdempotencyKey: uuid.NewString(), ExternalMcps: []*gen.AddExternalMCPForm{form}, NonBlocking: new(true)})
			} else {
				_, err = ti.service.Evolve(ctx, &gen.EvolvePayload{UpsertExternalMcps: []*gen.AddExternalMCPForm{form}, NonBlocking: new(true)})
			}
			require.Error(t, err, "new attachment must recheck publication")
		})
	}
}
