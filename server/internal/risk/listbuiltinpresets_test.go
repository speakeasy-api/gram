package risk_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestListBuiltinPresets_ReturnsCatalog(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	result, err := ti.service.ListBuiltinPresets(ctx, &gen.ListBuiltinPresetsPayload{})
	require.NoError(t, err)
	require.NotEmpty(t, result.Version)
	require.NotEmpty(t, result.Categories)

	// Every category is non-empty and every entry carries an id, reason and match
	// type. Collect ids so we can assert a known rule is surfaced.
	ids := map[string]bool{}
	for _, category := range result.Categories {
		require.NotEmpty(t, category.Label)
		require.NotEmpty(t, category.Entries)
		for _, entry := range category.Entries {
			require.NotEmpty(t, entry.ID)
			require.NotEmpty(t, entry.Reason)
			ids[entry.ID] = true
		}
	}
	require.True(t, ids["test-credit-cards"], "expected the test-credit-cards rule to be surfaced")
}

func TestListBuiltinPresets_RequiresOrgAdmin(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	// Zero grants — RBAC should deny.
	ctx = withExactAccessGrants(t, ctx, ti.conn)

	_, err := ti.service.ListBuiltinPresets(ctx, &gen.ListBuiltinPresetsPayload{})
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}
