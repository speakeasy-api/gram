package commandsearch

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/command_search"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/commandsearch/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type fakeStore struct {
	mu                sync.Mutex
	calls             map[string]int
	blocked           string
	environmentParams repo.ListCommandIndexEnvironmentsParams
	environments      []repo.ListCommandIndexEnvironmentsRow
	gateways          []repo.ListCommandIndexGatewaysRow
	plugins           []repo.ListCommandIndexPluginsRow
	rules             []repo.ListCommandIndexCustomRulesRow
}

func (f *fakeStore) call(ctx context.Context, kind string) error {
	f.mu.Lock()
	f.calls[kind]++
	blocked := f.blocked == kind
	f.mu.Unlock()
	if blocked {
		<-ctx.Done()
		return fmt.Errorf("wait for blocked fake query: %w", ctx.Err())
	}
	return nil
}

func (f *fakeStore) ListCommandIndexEnvironments(ctx context.Context, params repo.ListCommandIndexEnvironmentsParams) ([]repo.ListCommandIndexEnvironmentsRow, error) {
	f.mu.Lock()
	f.environmentParams = params
	f.mu.Unlock()
	return f.environments, f.call(ctx, "environment")
}
func (f *fakeStore) ListCommandIndexGateways(ctx context.Context, _ repo.ListCommandIndexGatewaysParams) ([]repo.ListCommandIndexGatewaysRow, error) {
	return f.gateways, f.call(ctx, "gateway")
}
func (f *fakeStore) ListCommandIndexPlugins(ctx context.Context, _ repo.ListCommandIndexPluginsParams) ([]repo.ListCommandIndexPluginsRow, error) {
	return f.plugins, f.call(ctx, "plugin")
}
func (f *fakeStore) ListCommandIndexCustomRules(ctx context.Context, _ repo.ListCommandIndexCustomRulesParams) ([]repo.ListCommandIndexCustomRulesRow, error) {
	return f.rules, f.call(ctx, "rule")
}

type fakeAuthz struct{ denied authz.Scope }

func (f fakeAuthz) Evaluate(_ context.Context, checks ...authz.Check) (bool, error) {
	return checks[0].Scope != f.denied, nil
}
func (fakeAuthz) RequirePluginWrite(context.Context, string, string) error { return nil }

func ctxFor(projectID uuid.UUID) context.Context {
	return contextvalues.SetAuthContext(context.Background(), &contextvalues.AuthContext{ProjectID: &projectID, ActiveOrganizationID: "org_test"})
}
func service(t *testing.T, store *fakeStore, authorizer fakeAuthz) *Service {
	t.Helper()
	return &Service{logger: testenv.NewLogger(t), repo: store, authz: authorizer, budget: time.Second}
}
func storeWithAllKinds() *fakeStore {
	ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New(), uuid.New()}
	return &fakeStore{
		calls:        map[string]int{},
		environments: []repo.ListCommandIndexEnvironmentsRow{{ID: ids[0], Name: "Environment", Slug: "environment"}},
		gateways:     []repo.ListCommandIndexGatewaysRow{{ID: ids[1], Name: "Gateway"}},
		plugins:      []repo.ListCommandIndexPluginsRow{{ID: ids[2], Name: "Plugin", Slug: "plugin"}},
		rules:        []repo.ListCommandIndexCustomRulesRow{{ID: ids[3], Title: "Rule", RuleID: "custom.rule", Severity: "medium"}},
	}
}

func TestSearchCompleteStableAndOneQueryPerKind(t *testing.T) {
	t.Parallel()

	store := storeWithAllKinds()
	svc := service(t, store, fakeAuthz{})
	a, err := svc.Search(ctxFor(uuid.New()), &gen.SearchPayload{Q: "ru"})
	require.NoError(t, err)
	b, err := svc.Search(ctxFor(uuid.New()), &gen.SearchPayload{Q: "ru"})
	require.NoError(t, err)
	require.Len(t, a.Items, 4)
	a.GeneratedAt, b.GeneratedAt = "", ""
	require.Equal(t, a, b)
	for _, kind := range allKinds {
		require.Equal(t, 2, store.calls[kind])
	}
}

func TestSearchSkipsUnauthorizedKind(t *testing.T) {
	t.Parallel()

	store := storeWithAllKinds()
	result, err := service(t, store, fakeAuthz{denied: authz.ScopeOrgAdmin}).Search(ctxFor(uuid.New()), &gen.SearchPayload{Q: "ru"})
	require.NoError(t, err)
	require.Zero(t, store.calls["rule"])
	for _, item := range result.Items {
		require.NotEqual(t, "rule", item.Kind)
	}
}

func TestSearchDeadlineReturnsExactPartialMetadata(t *testing.T) {
	t.Parallel()

	store := storeWithAllKinds()
	store.blocked = "gateway"
	svc := service(t, store, fakeAuthz{})
	svc.budget = 10 * time.Millisecond
	result, err := svc.Search(ctxFor(uuid.New()), &gen.SearchPayload{Q: "ga"})
	require.NoError(t, err)
	require.True(t, result.Partial)
	require.True(t, result.TimedOut)
	require.Equal(t, []string{"gateway"}, result.FailedKinds)
	require.Len(t, result.Items, 3)
}

func TestSearchPluginUsesIDSelectorAndSlugKeyword(t *testing.T) {
	t.Parallel()

	store := storeWithAllKinds()
	result, err := service(t, store, fakeAuthz{}).Search(ctxFor(uuid.New()), &gen.SearchPayload{Q: "pl"})
	require.NoError(t, err)
	for _, item := range result.Items {
		if item.Kind == "plugin" {
			require.Equal(t, store.plugins[0].ID.String(), item.RouteSelector)
			require.Equal(t, []string{"plugin"}, item.Keywords)
			return
		}
	}
	t.Fatal("plugin not found")
}

func TestMakePrefixBoundsUsesSingleNormalizedLowerRange(t *testing.T) {
	t.Parallel()

	require.Equal(t, prefixBounds{lower: "api", lowerCeiling: "apj"}, makePrefixBounds("  API  "))
}

func TestSearchPassesNormalizedLowerPrefixRange(t *testing.T) {
	t.Parallel()

	store := storeWithAllKinds()
	_, err := service(t, store, fakeAuthz{}).Search(ctxFor(uuid.New()), &gen.SearchPayload{Q: "API"})
	require.NoError(t, err)
	require.Equal(t, "api", store.environmentParams.LowerBound)
	require.Equal(t, "apj", store.environmentParams.LowerCeiling)
	require.Equal(t, int32(perKindLimit), store.environmentParams.ResultLimit)
}

func TestSearchRejectsQueryOutsideNormalizedLengthBounds(t *testing.T) {
	t.Parallel()

	for _, query := range []string{"  ", strings.Repeat("a", 129)} {
		store := storeWithAllKinds()
		_, err := service(t, store, fakeAuthz{}).Search(ctxFor(uuid.New()), &gen.SearchPayload{Q: query})
		require.Error(t, err)
		for _, kind := range allKinds {
			require.Zero(t, store.calls[kind])
		}
	}
}

func TestMatchRankExactThenAliasPrefixThenNamePrefix(t *testing.T) {
	t.Parallel()

	query := "cat"
	require.Equal(t, 0, matchRank(&gen.Commandresourceindexitem{Name: "Other", Keywords: []string{"CAT"}}, query))
	require.Equal(t, 1, matchRank(&gen.Commandresourceindexitem{Name: "Other", Keywords: []string{"catalog"}}, query))
	require.Equal(t, 2, matchRank(&gen.Commandresourceindexitem{Name: "Catalog"}, query))
}
