package commandsearch

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/command_search"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

// The other tests in this package use a fake authorizer, which accepts checks
// the real engine rejects as malformed. These run the real engine with grants
// enforced, as it is for a dashboard session.
func TestSearchWithEnforcedGrantsSucceeds(t *testing.T) {
	t.Parallel()

	project := uuid.New()
	session := "session-test"
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{ProjectID: &project, ActiveOrganizationID: "org_test", SessionID: &session})
	ctx = authztest.WithExactGrants(t, ctx,
		authz.NewGrant(authz.ScopeProjectRead, project.String()),
		authz.NewGrantWithSelector(authz.ScopeMCPRead, authz.Selector{
			authz.SelectorKeyResourceKind: authz.ResourceKindMCP,
			authz.SelectorKeyResourceID:   authz.WildcardResource,
			authz.SelectorKeyProjectID:    project.String(),
		}),
	)

	logger := testenv.NewLogger(t)
	engine := authz.NewEngine(logger, nil, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
	store := storeWithAllKinds()
	svc := service(t, store, fakeAuthz{})
	svc.authz = engine

	result, err := svc.Search(ctx, &gen.SearchPayload{Q: "ga"})
	require.NoError(t, err)

	kinds := map[string]bool{}
	for _, item := range result.Items {
		kinds[item.Kind] = true
	}
	require.True(t, kinds["gateway"], "a project-wide mcp:read grant must surface gateways")
	require.True(t, kinds["mcp"], "a project-wide mcp:read grant must surface MCP servers")
	require.True(t, kinds["environment"])
}
