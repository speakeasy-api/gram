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

// createForeignRegistry creates an org that is not the test user's org and
// inserts an internal registry owned by that org. Returns the registry ID.
func createForeignRegistry(t *testing.T, ctx context.Context, ti *testInstance, orgID, orgName, orgSlug, registryName, registrySlug, visibility string) string {
	t.Helper()

	orgQueries := orgRepo.New(ti.conn)
	_, err := orgQueries.UpsertOrganizationMetadata(ctx, orgRepo.UpsertOrganizationMetadataParams{
		ID:              orgID,
		Name:            orgName,
		Slug:            orgSlug,
		SsoConnectionID: pgtype.Text{},
	})
	require.NoError(t, err)

	registry, err := ti.repo.CreateInternalRegistry(ctx, repo.CreateInternalRegistryParams{
		Name:           registryName,
		Slug:           conv.ToPGText(registrySlug),
		Visibility:     visibility,
		OrganizationID: conv.ToPGText(orgID),
		ProjectID:      uuid.NullUUID{},
	})
	require.NoError(t, err)

	return registry.ID.String()
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

func TestServeForeignPrivateRegistryForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	createForeignRegistry(t, ctx, ti,
		"serve-foreign-org", "Foreign Org", "foreign-org",
		"Foreign Private", "serve-forbidden", "private",
	)

	_, err := ti.service.Serve(ctx, &gen.ServePayload{
		RegistrySlug: "serve-forbidden",
	})
	require.Error(t, err)
}

func TestServeForeignPublicRegistryAllowed(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	createForeignRegistry(t, ctx, ti,
		"serve-foreign-pub-org", "Foreign Pub Org", "foreign-pub-org",
		"Foreign Public", "serve-foreign-public", "public",
	)

	result, err := ti.service.Serve(ctx, &gen.ServePayload{
		RegistrySlug: "serve-foreign-public",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestGetServerDetailsForeignPrivateRegistryForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	registryID := createForeignRegistry(t, ctx, ti,
		"details-foreign-org", "Details Foreign Org", "details-foreign-org",
		"Details Foreign Private", "details-forbidden", "private",
	)

	_, err := ti.service.GetServerDetails(ctx, &gen.GetServerDetailsPayload{
		RegistryID:      registryID,
		ServerSpecifier: "anything",
	})
	require.Error(t, err)
}

func TestGetServerDetailsForeignPublicRegistryAllowed(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	registryID := createForeignRegistry(t, ctx, ti,
		"details-foreign-pub-org", "Details Foreign Pub Org", "details-foreign-pub-org",
		"Details Foreign Public", "details-foreign-public", "public",
	)

	// The call will fail because there's no matching toolset, but it should
	// get past the authorization check (not return forbidden).
	_, err := ti.service.GetServerDetails(ctx, &gen.GetServerDetailsPayload{
		RegistryID:      registryID,
		ServerSpecifier: "nonexistent-server",
	})
	require.Error(t, err)
	require.NotContains(t, err.Error(), "forbidden")
}

func TestListRegistriesExcludesForeignPrivate(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	// Publish an own registry so we have at least one result
	publishRegistry(t, ctx, ti, "Own Catalog", "list-own", "private")

	// Create a foreign private registry — should NOT appear in listing
	createForeignRegistry(t, ctx, ti,
		"list-foreign-org", "List Foreign Org", "list-foreign-org",
		"Foreign Hidden", "list-foreign-hidden", "private",
	)

	result, err := ti.service.ListRegistries(ctx, &gen.ListRegistriesPayload{})
	require.NoError(t, err)

	for _, r := range result.Registries {
		require.NotEqual(t, "list-foreign-hidden", *r.Slug,
			"foreign private registry should not appear in listing")
	}
}

func TestListRegistriesIncludesForeignPublic(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	createForeignRegistry(t, ctx, ti,
		"list-foreign-pub-org", "List Foreign Pub Org", "list-foreign-pub-org",
		"Foreign Visible", "list-foreign-visible", "public",
	)

	result, err := ti.service.ListRegistries(ctx, &gen.ListRegistriesPayload{})
	require.NoError(t, err)

	found := false
	for _, r := range result.Registries {
		if r.Slug != nil && *r.Slug == "list-foreign-visible" {
			found = true
			break
		}
	}
	require.True(t, found, "foreign public registry should appear in listing")
}

func TestServeNonexistentRegistry(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.Serve(ctx, &gen.ServePayload{
		RegistrySlug: "does-not-exist",
	})
	require.Error(t, err)
}
