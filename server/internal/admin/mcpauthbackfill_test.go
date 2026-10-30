package admin

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestBackfillMcpAuthGrants(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	ctx = contextvalues.SetAdminAuthContext(ctx, &contextvalues.AdminAuthContext{OIDCSubject: "staff-test"})
	orgID := "org_" + uuid.NewString()
	seedOrg(t, ctx, db, orgFixture{id: orgID, name: "mcp-auth-backfill", slug: "mcp-auth-backfill", accountType: "enterprise"})

	q := accessrepo.New(db)
	user := urn.NewPrincipal(urn.PrincipalTypeUser, "user_"+uuid.NewString())
	role := urn.NewPrincipal(urn.PrincipalTypeRole, "organization:"+uuid.NewString())
	apiKey := urn.NewPrincipal(urn.PrincipalTypeAPIKey, uuid.NewString())
	reader := urn.NewPrincipal(urn.PrincipalTypeUser, "user_"+uuid.NewString())
	grant := func(principal urn.Principal, scope authz.Scope, resourceID string) {
		t.Helper()
		raw, err := authz.NewSelector(scope, resourceID).MarshalJSON()
		require.NoError(t, err)
		_, err = q.UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{OrganizationID: orgID, PrincipalUrn: principal, Scope: string(scope), Selectors: raw})
		require.NoError(t, err)
	}
	grant(user, authz.ScopeMCPConnect, uuid.NewString())
	grant(role, authz.ScopeMCPConnect, authz.WildcardResource)
	grant(apiKey, authz.ScopeMCPConnect, authz.WildcardResource)
	grant(reader, authz.ScopeMCPRead, authz.WildcardResource)

	result, err := svc.BackfillMcpAuthGrants(ctx, &gen.BackfillMcpAuthGrantsPayload{})
	require.NoError(t, err)
	require.GreaterOrEqual(t, result.Granted, int64(3))

	wildcard := authz.MCPAuthClientCheck(uuid.NewString(), authz.MCPAuthClientDimensions{ClientType: authz.AuthClientTypeStaticCredential, ProjectID: uuid.NewString()})
	for principal, want := range map[urn.Principal]bool{user: true, role: true, apiKey: true, reader: false} {
		grants, err := authz.LoadGrants(ctx, db, orgID, []urn.Principal{principal})
		require.NoError(t, err)
		allowed, err := authz.GrantsAuthorize(grants, wildcard)
		require.NoError(t, err)
		require.Equal(t, want, allowed, principal.String())
	}

	_, err = svc.BackfillMcpAuthGrants(ctx, &gen.BackfillMcpAuthGrantsPayload{})
	require.NoError(t, err, "safe to run again")
}
