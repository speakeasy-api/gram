package customdomains_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/domains"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestCreateDomain_RequiresPaidAccountType(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		accountType string
		allowed     bool
	}{
		{accountType: "free", allowed: false},
		{accountType: "pro", allowed: true},
		{accountType: "payg", allowed: true},
		{accountType: "enterprise", allowed: true},
		{accountType: "unknown", allowed: false},
	} {
		t.Run(tt.accountType, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestCustomDomainsService(t)
			authCtx := testAuthContext(t, ctx)
			authCtx.AccountType = tt.accountType
			ctx = authztest.WithExactGrants(t, ctx, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})

			_, err := ti.service.CreateDomain(ctx, &gen.CreateDomainPayload{Domain: "tier.example.com"})
			if !tt.allowed {
				requireOopsCode(t, err, oops.CodeUnauthorized)
				require.Zero(t, ti.temporal.registrationCalls)
				return
			}
			require.NoError(t, err)
			require.Equal(t, 1, ti.temporal.registrationCalls)
		})
	}
}
