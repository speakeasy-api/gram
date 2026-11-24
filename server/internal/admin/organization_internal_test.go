package admin

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestUpdateOrganization_SetsAndClearsInternal(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	seedOrg(t, ctx, conn, orgFixture{id: "org_internal_a", name: "Dogfood Sandbox", slug: "dogfood-sandbox"})

	before, err := svc.GetOrganization(ctx, &gen.GetOrganizationPayload{IDOrSlug: "org_internal_a"})
	require.NoError(t, err)
	require.False(t, before.Internal, "organizations start out not internal")

	res, err := svc.UpdateOrganization(ctx, &gen.UpdateOrganizationPayload{ID: "org_internal_a", Internal: new(true)})
	require.NoError(t, err)
	require.True(t, res.Internal)

	got, err := svc.GetOrganization(ctx, &gen.GetOrganizationPayload{IDOrSlug: "org_internal_a"})
	require.NoError(t, err)
	require.True(t, got.Internal)

	list, err := svc.ListOrganizations(ctx, &gen.ListOrganizationsPayload{Q: new("org_internal_a")})
	require.NoError(t, err)
	require.Len(t, list.Organizations, 1)
	require.True(t, list.Organizations[0].Internal)

	res, err = svc.UpdateOrganization(ctx, &gen.UpdateOrganizationPayload{ID: "org_internal_a", Internal: new(false)})
	require.NoError(t, err)
	require.False(t, res.Internal)
}

func TestUpdateOrganization_InternalLeavesOtherFieldsAlone(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	seedOrg(t, ctx, conn, orgFixture{id: "org_internal_keep", name: "QA Playground", slug: "qa-playground", accountType: "enterprise", whitelisted: true})

	res, err := svc.UpdateOrganization(ctx, &gen.UpdateOrganizationPayload{ID: "org_internal_keep", Internal: new(true)})
	require.NoError(t, err)
	require.Equal(t, "enterprise", res.AccountType)
	require.True(t, res.Whitelisted)
}

func TestUpdateOrganization_InternalUnknownOrganization(t *testing.T) {
	t.Parallel()

	ctx, svc, _ := newTestAdminService(t)

	_, err := svc.UpdateOrganization(ctx, &gen.UpdateOrganizationPayload{ID: "org_missing", Internal: new(true)})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestUpdateOrganization_InternalCannotBeCombined(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	seedOrg(t, ctx, conn, orgFixture{id: "org_internal_combo", name: "Combo Co", slug: "combo-co", accountType: "free", whitelisted: false})

	for _, tc := range []struct {
		name    string
		payload *gen.UpdateOrganizationPayload
	}{
		{"account_type", &gen.UpdateOrganizationPayload{ID: "org_internal_combo", Internal: new(true), AccountType: new("enterprise")}},
		{"whitelisted", &gen.UpdateOrganizationPayload{ID: "org_internal_combo", Internal: new(true), Whitelisted: new(true)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.UpdateOrganization(ctx, tc.payload)
			requireOopsCode(t, err, oops.CodeBadRequest)
			require.ErrorContains(t, err, "internal cannot be combined with account_type or whitelisted")
		})
	}

	got, err := svc.GetOrganization(ctx, &gen.GetOrganizationPayload{IDOrSlug: "org_internal_combo"})
	require.NoError(t, err)
	require.False(t, got.Internal)
	require.Equal(t, "free", got.AccountType)
	require.False(t, got.Whitelisted)
}

func TestUpdateOrganization_RequiresAField(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	seedOrg(t, ctx, conn, orgFixture{id: "org_internal_empty", name: "Empty Co", slug: "empty-co"})

	_, err := svc.UpdateOrganization(ctx, &gen.UpdateOrganizationPayload{ID: "org_internal_empty"})
	requireOopsCode(t, err, oops.CodeBadRequest)
	require.ErrorContains(t, err, "at least one of account_type, whitelisted or internal must be supplied")
}

func TestUpdateOrganization_InternalWritesNoAuditLog(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	seedOrg(t, ctx, conn, orgFixture{id: "org_internal_quiet", name: "Quiet Co", slug: "quiet-co"})

	before, err := audittest.AuditLogCount(ctx, conn)
	require.NoError(t, err)

	_, err = svc.UpdateOrganization(ctx, &gen.UpdateOrganizationPayload{ID: "org_internal_quiet", Internal: new(true)})
	require.NoError(t, err)

	after, err := audittest.AuditLogCount(ctx, conn)
	require.NoError(t, err)
	require.Equal(t, before, after, "the internal label is staff metadata and must stay out of the customer-readable audit log")
}
