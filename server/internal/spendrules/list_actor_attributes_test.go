package spendrules_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/spend_rules"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/spendrules/celenv"
)

func TestListActorAttributes_Success(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestSpendRulesService(t)
	ctx = withOrgAdmin(t, ctx, ti.conn)

	result, err := ti.service.ListActorAttributes(ctx, &gen.ListActorAttributesPayload{})
	require.NoError(t, err)
	require.NotNil(t, result)

	// The API mirrors the celenv catalog exactly — same order, names, and kinds
	// — so the editor can never offer an attribute the server would reject.
	require.Len(t, result.Attributes, len(celenv.ActorAttributes))
	for i, want := range celenv.ActorAttributes {
		require.Equal(t, want.Name, result.Attributes[i].Name)
		require.Equal(t, string(want.Kind), result.Attributes[i].Type)
		require.NotEmpty(t, result.Attributes[i].Description)
	}

	// Every returned attribute must be one the server validates as a real
	// target attribute.
	for _, attr := range result.Attributes {
		_, ok := celenv.TargetAttributeKind(attr.Name)
		require.Truef(t, ok, "attribute %q is not a known target attribute", attr.Name)
		require.Contains(t, []string{"string", "list"}, attr.Type)
	}
}

func TestListActorAttributes_Unauthorized(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestSpendRulesService(t)

	// Enterprise account with zero grants — RBAC should deny.
	ctx = withExactAccessGrants(t, ctx, ti.conn)

	_, err := ti.service.ListActorAttributes(ctx, &gen.ListActorAttributesPayload{})
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}
