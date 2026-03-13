package externalmcp_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_registries"
	"github.com/speakeasy-api/gram/server/internal/externalmcp/repo"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
)

// publishTestRegistry is a helper that publishes a private registry and returns its ID.
func publishTestRegistry(t *testing.T, ctx context.Context, ti *testInstance, slug string) string {
	t.Helper()

	registry, err := ti.service.Publish(ctx, &gen.PublishPayload{
		Name:       slug,
		Slug:       slug,
		Visibility: "private",
		ToolsetIds: []string{},
	})
	require.NoError(t, err)
	return registry.ID
}

// createSubOrg is a helper that creates an org and peers it as a sub of the caller's org.
func createSubOrg(t *testing.T, ctx context.Context, ti *testInstance, orgID, name, slug string) {
	t.Helper()

	orgQueries := orgRepo.New(ti.conn)
	_, err := orgQueries.UpsertOrganizationMetadata(ctx, orgRepo.UpsertOrganizationMetadataParams{
		ID:              orgID,
		Name:            name,
		Slug:            slug,
		SsoConnectionID: pgtype.Text{},
	})
	require.NoError(t, err)

	_, err = ti.service.CreatePeer(ctx, &gen.CreatePeerPayload{
		SubOrganizationID: orgID,
	})
	require.NoError(t, err)
}

func TestGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	subOrgID := "grant-sub-1"
	createSubOrg(t, ctx, ti, subOrgID, "Grant Sub Org", "grant-sub-org")

	registryID := publishTestRegistry(t, ctx, ti, "grant-test-catalog")

	err := ti.service.Grant(ctx, &gen.GrantPayload{
		RegistryID:     registryID,
		OrganizationID: subOrgID,
	})
	require.NoError(t, err)

	// Verify the grant exists
	hasGrant, err := ti.repo.CheckRegistryGrant(ctx, repo.CheckRegistryGrantParams{
		RegistryID:     uuid.MustParse(registryID),
		OrganizationID: subOrgID,
	})
	require.NoError(t, err)
	require.True(t, hasGrant)
}

func TestGrantRequiresProjectOwnership(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	subOrgID := "grant-auth-sub-1"
	createSubOrg(t, ctx, ti, subOrgID, "Grant Auth Sub", "grant-auth-sub")

	registryID := publishTestRegistry(t, ctx, ti, "grant-auth-catalog")

	// Create a second test service (different project)
	ctx2, ti2 := newTestService(t)

	// Peer the same sub org in the second service's context
	createSubOrg(t, ctx2, ti2, "grant-auth-sub-2", "Grant Auth Sub 2", "grant-auth-sub-2")

	// Try to grant from ctx2 — different project doesn't own the registry
	err := ti2.service.Grant(ctx2, &gen.GrantPayload{
		RegistryID:     registryID,
		OrganizationID: "grant-auth-sub-2",
	})
	require.Error(t, err)
}

func TestGrantNonPeerForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	registryID := publishTestRegistry(t, ctx, ti, "grant-nonpeer-catalog")

	// Try to grant to an org that is not a peer
	err := ti.service.Grant(ctx, &gen.GrantPayload{
		RegistryID:     registryID,
		OrganizationID: "not-a-peer-org",
	})
	require.Error(t, err)
}

func TestGrantNonexistentRegistry(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	err := ti.service.Grant(ctx, &gen.GrantPayload{
		RegistryID:     "00000000-0000-0000-0000-000000000000",
		OrganizationID: "some-org",
	})
	require.Error(t, err)
}

func TestRevokeGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	subOrgID := "revoke-sub-1"
	createSubOrg(t, ctx, ti, subOrgID, "Revoke Sub Org", "revoke-sub-org")

	registryID := publishTestRegistry(t, ctx, ti, "revoke-test-catalog")

	// Grant first
	err := ti.service.Grant(ctx, &gen.GrantPayload{
		RegistryID:     registryID,
		OrganizationID: subOrgID,
	})
	require.NoError(t, err)

	// Revoke
	err = ti.service.RevokeGrant(ctx, &gen.RevokeGrantPayload{
		RegistryID:     registryID,
		OrganizationID: subOrgID,
	})
	require.NoError(t, err)
}

func TestRevokeGrantRequiresProjectOwnership(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	subOrgID := "revoke-auth-sub-1"
	createSubOrg(t, ctx, ti, subOrgID, "Revoke Auth Sub", "revoke-auth-sub")

	registryID := publishTestRegistry(t, ctx, ti, "revoke-auth-catalog")

	// Grant access
	err := ti.service.Grant(ctx, &gen.GrantPayload{
		RegistryID:     registryID,
		OrganizationID: subOrgID,
	})
	require.NoError(t, err)

	// Try to revoke from a different project
	ctx2, ti2 := newTestService(t)

	err = ti2.service.RevokeGrant(ctx2, &gen.RevokeGrantPayload{
		RegistryID:     registryID,
		OrganizationID: subOrgID,
	})
	require.Error(t, err)
}

func TestRevokeGrantNonexistentRegistry(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	err := ti.service.RevokeGrant(ctx, &gen.RevokeGrantPayload{
		RegistryID:     "00000000-0000-0000-0000-000000000000",
		OrganizationID: "some-org",
	})
	require.Error(t, err)
}
