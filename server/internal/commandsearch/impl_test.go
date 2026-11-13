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
	blockedStarted    chan struct{}
	blockedStartOnce  sync.Once
	blockedErr        error
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
		if f.blockedStarted != nil {
			f.blockedStartOnce.Do(func() { close(f.blockedStarted) })
		}
		<-ctx.Done()
		if f.blockedErr != nil {
			return f.blockedErr
		}
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

type fakeAuthz struct {
	denied      authz.Scope
	evaluate    func(authz.Check) bool
	evaluateCtx func(context.Context, authz.Check) (bool, error)
}

func (f fakeAuthz) Evaluate(ctx context.Context, checks ...authz.Check) (bool, error) {
	if f.evaluateCtx != nil {
		return f.evaluateCtx(ctx, checks[0])
	}
	if f.evaluate != nil {
		return f.evaluate(checks[0]), nil
	}
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
	store.blockedStarted = make(chan struct{})
	svc := service(t, store, fakeAuthz{})
	svc.budget = 100 * time.Millisecond
	type searchResult struct {
		result *gen.Commandresourceindex
		err    error
	}
	done := make(chan searchResult, 1)
	go func() {
		result, err := svc.Search(ctxFor(uuid.New()), &gen.SearchPayload{Q: "ga"})
		done <- searchResult{result: result, err: err}
	}()
	<-store.blockedStarted
	got := <-done
	require.NoError(t, got.err)
	require.True(t, got.result.Partial)
	require.True(t, got.result.TimedOut)
	require.False(t, got.result.Truncated)
	require.Empty(t, got.result.TruncatedKinds)
	require.Equal(t, []string{"gateway"}, got.result.FailedKinds)
	require.Len(t, got.result.Items, 3)
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
	require.Equal(t, int32(perKindQueryLimit), store.environmentParams.ResultLimit)
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

func TestSearchGatewayCollectionAuthorizationUsesProjectAwareWildcard(t *testing.T) {
	t.Parallel()

	currentProject := uuid.New()
	tests := []struct {
		name        string
		wantGateway bool
	}{
		{name: "current project collection grant", wantGateway: true},
		{name: "other project collection grant", wantGateway: false},
		{name: "individual gateway grant", wantGateway: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store := storeWithAllKinds()
			seenGatewayCheck := false
			authorizer := fakeAuthz{evaluate: func(check authz.Check) bool {
				if check.Scope != authz.ScopeMCPRead {
					return true
				}
				seenGatewayCheck = true
				require.Equal(t, authz.WildcardResource, check.ResourceID)
				require.Empty(t, check.ResourceKind)
				require.Equal(t, map[string]string{authz.SelectorKeyProjectID: currentProject.String()}, check.Dimensions)
				return tt.wantGateway
			}}
			result, err := service(t, store, authorizer).Search(ctxFor(currentProject), &gen.SearchPayload{Q: "ga"})
			require.NoError(t, err)
			require.True(t, seenGatewayCheck)
			if tt.wantGateway {
				require.Equal(t, 1, store.calls["gateway"])
				require.Contains(t, itemKinds(result.Items), "gateway")
			} else {
				require.Zero(t, store.calls["gateway"])
				require.NotContains(t, itemKinds(result.Items), "gateway")
			}
		})
	}
}

func itemKinds(items []*gen.Commandresourceindexitem) []string {
	kinds := make([]string, 0, len(items))
	for _, item := range items {
		kinds = append(kinds, item.Kind)
	}
	return kinds
}

func TestDeadlineAdapterErrorClassificationPreservesOrdinaryErrors(t *testing.T) {
	t.Parallel()

	require.True(t, deadlineInterruptedAdapter(context.DeadlineExceeded, context.Canceled))
	require.True(t, deadlineInterruptedAdapter(context.DeadlineExceeded, context.DeadlineExceeded))
	require.False(t, deadlineInterruptedAdapter(context.DeadlineExceeded, fmt.Errorf("database unavailable")))
}

func TestSearchReportsPerKindTruncationWithoutFailure(t *testing.T) {
	t.Parallel()

	store := storeWithAllKinds()
	store.environments = make([]repo.ListCommandIndexEnvironmentsRow, perKindQueryLimit)
	for i := range store.environments {
		store.environments[i] = repo.ListCommandIndexEnvironmentsRow{ID: uuid.New(), Name: fmt.Sprintf("Environment %02d", i), Slug: fmt.Sprintf("environment-%02d", i)}
	}
	result, err := service(t, store, fakeAuthz{}).Search(ctxFor(uuid.New()), &gen.SearchPayload{Q: "en"})
	require.NoError(t, err)
	require.True(t, result.Truncated)
	require.Equal(t, []string{"environment"}, result.TruncatedKinds)
	require.False(t, result.Partial)
	require.Empty(t, result.FailedKinds)
	require.Len(t, result.Items, perKindLimit+3)
}

func TestSearchReportsGlobalTruncatedKinds(t *testing.T) {
	t.Parallel()

	store := storeWithAllKinds()
	store.environments = make([]repo.ListCommandIndexEnvironmentsRow, perKindLimit)
	store.gateways = make([]repo.ListCommandIndexGatewaysRow, perKindLimit)
	store.plugins = make([]repo.ListCommandIndexPluginsRow, perKindLimit)
	store.rules = make([]repo.ListCommandIndexCustomRulesRow, perKindLimit)
	for i := range perKindLimit {
		name := fmt.Sprintf("Match %02d", i)
		store.environments[i] = repo.ListCommandIndexEnvironmentsRow{ID: uuid.New(), Name: name, Slug: fmt.Sprintf("environment-%02d", i)}
		store.gateways[i] = repo.ListCommandIndexGatewaysRow{ID: uuid.New(), Name: name}
		store.plugins[i] = repo.ListCommandIndexPluginsRow{ID: uuid.New(), Name: name, Slug: fmt.Sprintf("plugin-%02d", i)}
		store.rules[i] = repo.ListCommandIndexCustomRulesRow{ID: uuid.New(), Title: name, RuleID: fmt.Sprintf("rule.%02d", i), Severity: "medium"}
	}
	result, err := service(t, store, fakeAuthz{}).Search(ctxFor(uuid.New()), &gen.SearchPayload{Q: "ma"})
	require.NoError(t, err)
	require.True(t, result.Truncated)
	require.Equal(t, []string{"plugin", "rule"}, result.TruncatedKinds)
	require.False(t, result.Partial)
	require.Empty(t, result.FailedKinds)
	require.Len(t, result.Items, globalLimit)
}

func TestSearchPreflightDeadlineIsRequestError(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	var once sync.Once
	authorizer := fakeAuthz{evaluateCtx: func(ctx context.Context, _ authz.Check) (bool, error) {
		once.Do(func() { close(started) })
		<-ctx.Done()
		return false, fmt.Errorf("wait for preflight deadline: %w", ctx.Err())
	}}
	svc := service(t, storeWithAllKinds(), authorizer)
	svc.budget = 100 * time.Millisecond
	done := make(chan error, 1)
	go func() {
		_, err := svc.Search(ctxFor(uuid.New()), &gen.SearchPayload{Q: "ga"})
		done <- err
	}()
	<-started
	require.Error(t, <-done)
}
