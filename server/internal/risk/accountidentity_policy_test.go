package risk_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

func TestCreateRiskPolicy_AccountIdentitySource(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})

	result, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Sources: []string{"account_identity"},
		Action:  "flag",
		// Entries are normalized: lowercased, trimmed, leading "@" stripped,
		// duplicates removed.
		ApprovedEmailDomains: []string{"@Acme.com", "acme.com", " beta.io "},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"account_identity"}, result.Sources)
	require.Equal(t, []string{"acme.com", "beta.io"}, result.ApprovedEmailDomains)
	require.Equal(t, "Non-Corporate Account Scanner", result.Name)
}

func TestCreateRiskPolicy_AccountIdentityRejectsBlock(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})

	_, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Sources: []string{"account_identity"},
		Action:  "block",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "account_identity")
}

func TestCreateRiskPolicy_AccountIdentityInvalidDomain(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})

	_, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Sources:              []string{"account_identity"},
		Action:               "flag",
		ApprovedEmailDomains: []string{"not a domain"},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not a valid domain")
}

func TestUpdateRiskPolicy_ApprovedEmailDomains(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})

	created, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:                 new("Account Identity"),
		Sources:              []string{"account_identity"},
		Action:               "flag",
		ApprovedEmailDomains: []string{"acme.com"},
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), created.Version)

	// Omitting the field preserves the current list without a version bump.
	updated, err := ti.service.UpdateRiskPolicy(ctx, &gen.UpdateRiskPolicyPayload{
		ID:   created.ID,
		Name: "Account Identity Renamed",
	})
	require.NoError(t, err)
	require.Equal(t, []string{"acme.com"}, updated.ApprovedEmailDomains)
	require.Equal(t, int64(1), updated.Version)

	// Replacing the list is a detection-config change and bumps the version.
	updated, err = ti.service.UpdateRiskPolicy(ctx, &gen.UpdateRiskPolicyPayload{
		ID:                   created.ID,
		Name:                 updated.Name,
		ApprovedEmailDomains: []string{"acme.com", "beta.io"},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"acme.com", "beta.io"}, updated.ApprovedEmailDomains)
	require.Equal(t, int64(2), updated.Version)

	// Sending an empty array clears the list.
	updated, err = ti.service.UpdateRiskPolicy(ctx, &gen.UpdateRiskPolicyPayload{
		ID:                   created.ID,
		Name:                 updated.Name,
		ApprovedEmailDomains: []string{},
	})
	require.NoError(t, err)
	require.Empty(t, updated.ApprovedEmailDomains)
	require.Equal(t, int64(3), updated.Version)
}
