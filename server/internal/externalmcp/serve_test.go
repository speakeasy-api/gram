package externalmcp_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_registries"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/externalmcp/repo"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
)

func publishRegistry(t *testing.T, ctx context.Context, ti *testInstance, name, slug, visibility string) string {
	t.Helper()

	registry, err := ti.service.Publish(ctx, &gen.PublishPayload{
		Name:       name,
		Slug:       slug,
		Visibility: visibility,
		ToolsetIds: []string{},
	})
	require.NoError(t, err)
	return registry.ID
}

func TestServeOwnPrivateRegistry(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	publishRegistry(t, ctx, ti, "My Private", "serve-own-private", "private")

	result, err := ti.service.Serve(ctx, &gen.ServePayload{
		RegistrySlug: "serve-own-private",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Empty(t, result.Servers)
}

func TestServePublicRegistry(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	publishRegistry(t, ctx, ti, "Public Catalog", "serve-public", "public")

	result, err := ti.service.Serve(ctx, &gen.ServePayload{
		RegistrySlug: "serve-public",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestServePrivateRegistryForbiddenWithoutGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	// Create a foreign org that owns a private registry
	foreignOrgID := "serve-foreign-org"
	orgQueries := orgRepo.New(ti.conn)
	_, err := orgQueries.UpsertOrganizationMetadata(ctx, orgRepo.UpsertOrganizationMetadataParams{
		ID:              foreignOrgID,
		Name:            "Foreign Org",
		Slug:            "foreign-org",
		SsoConnectionID: pgtype.Text{},
	})
	require.NoError(t, err)

	// Insert a private registry owned by the foreign org directly via SQL
	_, err = ti.repo.CreateInternalRegistry(ctx, repo.CreateInternalRegistryParams{
		Name:           "Foreign Private",
		Slug:           conv.ToPGText("serve-forbidden"),
		Visibility:     "private",
		OrganizationID: conv.ToPGText(foreignOrgID),
		ProjectID:      uuid.NullUUID{},
	})
	require.NoError(t, err)

	// Our test user (different org) should be forbidden
	_, err = ti.service.Serve(ctx, &gen.ServePayload{
		RegistrySlug: "serve-forbidden",
	})
	require.Error(t, err)
}

func TestServePrivateRegistryWithGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	// Create a sub org and peer it
	subOrgID := "serve-grant-sub-1"
	orgQueries := orgRepo.New(ti.conn)
	_, err := orgQueries.UpsertOrganizationMetadata(ctx, orgRepo.UpsertOrganizationMetadataParams{
		ID:              subOrgID,
		Name:            "Serve Grant Sub",
		Slug:            "serve-grant-sub",
		SsoConnectionID: pgtype.Text{},
	})
	require.NoError(t, err)

	_, err = ti.service.CreatePeer(ctx, &gen.CreatePeerPayload{
		SubOrganizationID: subOrgID,
	})
	require.NoError(t, err)

	registryID := publishRegistry(t, ctx, ti, "Granted Catalog", "serve-granted", "private")

	// Grant access to the sub org
	err = ti.service.Grant(ctx, &gen.GrantPayload{
		RegistryID:     registryID,
		OrganizationID: subOrgID,
	})
	require.NoError(t, err)

	// Now create a service context for the sub org and verify they can serve
	// Note: since newTestService always creates the same mock user/org,
	// we verify the grant exists at the DB level instead
	hasGrant, err := ti.repo.CheckRegistryGrant(ctx, repo.CheckRegistryGrantParams{
		RegistryID:     uuid.MustParse(registryID),
		OrganizationID: subOrgID,
	})
	require.NoError(t, err)
	require.True(t, hasGrant)
}

func TestServeNonexistentRegistry(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.Serve(ctx, &gen.ServePayload{
		RegistrySlug: "does-not-exist",
	})
	require.Error(t, err)
}
