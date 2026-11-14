package risk_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
)

// "confirm" is the action formerly called "warn". The API answers "confirm"
// for either name, and the row keeps the legacy value "warn" so a server not
// yet on this version, or a rollback, keeps enforcing the policy.
func TestRiskPolicyConfirmAction_AcceptsLegacyWarnAndStoresIt(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})

	storedAction := func(id string) string {
		t.Helper()
		row, err := riskrepo.New(ti.conn).GetRiskPolicy(ctx, riskrepo.GetRiskPolicyParams{ID: uuid.MustParse(id), ProjectID: *authCtx.ProjectID})
		require.NoError(t, err)
		return row.Action
	}

	for _, action := range []string{"confirm", "warn"} {
		created, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
			Name:    new("Secrets " + action),
			Sources: []string{"gitleaks"},
			Action:  action,
		})
		require.NoError(t, err)
		require.Equal(t, "confirm", created.Action, "the API answers with the current name for %q", action)
		require.Equal(t, "warn", storedAction(created.ID), "the row keeps the legacy value")

		got, err := ti.service.GetRiskPolicy(ctx, &gen.GetRiskPolicyPayload{ID: created.ID})
		require.NoError(t, err)
		require.Equal(t, "confirm", got.Action)
	}

	// Changing a policy to and from confirm round-trips through the legacy value.
	created, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:    new("Secrets block"),
		Sources: []string{"gitleaks"},
		Action:  "block",
	})
	require.NoError(t, err)
	updated, err := ti.service.UpdateRiskPolicy(ctx, &gen.UpdateRiskPolicyPayload{
		ID:     created.ID,
		Name:   created.Name,
		Action: new("confirm"),
	})
	require.NoError(t, err)
	require.Equal(t, "confirm", updated.Action)
	require.Equal(t, "warn", storedAction(created.ID))

	// An edit that leaves the action alone keeps the policy a confirm policy.
	renamed, err := ti.service.UpdateRiskPolicy(ctx, &gen.UpdateRiskPolicyPayload{
		ID:   created.ID,
		Name: "Secrets confirm renamed",
	})
	require.NoError(t, err)
	require.Equal(t, "confirm", renamed.Action)
	require.Equal(t, "warn", storedAction(created.ID))
}
