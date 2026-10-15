package deployments_test

import (
	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/deployments"
	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/deployments/repo"
	"github.com/speakeasy-api/gram/server/internal/externalmcp"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcpregistry"
	registryrepo "github.com/speakeasy-api/gram/server/internal/mcpregistry/repo"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestDeploymentsService_RejectNewCatalogSelection(t *testing.T) {
	t.Parallel()
	for _, method := range []string{"create", "evolve"} {
		t.Run(method, func(t *testing.T) {
			t.Parallel()
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

func TestDeploymentsService_RejectUnpublishedNativeAttachment(t *testing.T) {
	t.Parallel()
	for _, method := range []string{"create", "evolve"} {
		t.Run(method, func(t *testing.T) {
			t.Parallel()
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
			require.ErrorIs(t, err, mcpregistry.ErrNotFound, "new attachment must recheck publication")
		})
	}
}

func TestDeploymentsService_CatalogRetryKeepsAcceptedDeployment(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestDeploymentService(t, assetstest.NewTestBlobStore(t))
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	q := repo.New(ti.conn)
	key := uuid.NewString()
	id, err := q.InsertDeployment(ctx, repo.InsertDeploymentParams{ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID, UserID: auth.UserID, IdempotencyKey: key})
	require.NoError(t, err)
	require.NoError(t, q.CreateDeploymentStatus(ctx, repo.CreateDeploymentStatusParams{DeploymentID: id, Status: "completed"}))
	result, err := ti.service.CreateDeployment(ctx, &gen.CreateDeploymentPayload{IdempotencyKey: key, NonBlocking: new(true), ExternalMcps: []*gen.AddExternalMCPForm{{RegistryID: new(externalmcp.NativeCatalogRegistryID.String()), Name: "Test", Slug: "test", RegistryServerSpecifier: "example/server"}}})
	require.NoError(t, err)
	require.Equal(t, id.String(), result.Deployment.ID)
}
