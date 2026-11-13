package projectresourceindex

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/project_resource_index"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/projectresourceindex/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type fakeStore struct {
	assistants   []repo.ListProjectResourceIndexAssistantsRow
	mcp          []repo.ListProjectResourceIndexMCPServersRow
	skills       []repo.ListProjectResourceIndexSkillsRow
	environments []repo.ListProjectResourceIndexEnvironmentsRow
	gateways     []repo.ListProjectResourceIndexGatewaysRow
	plugins      []repo.ListProjectResourceIndexPluginsRow
	rules        []repo.ListProjectResourceIndexCustomRulesRow
	errKind      string
	calls        map[string]int
	mu           sync.Mutex
}

func (f *fakeStore) result(kind string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls[kind]++
	if f.errKind == kind {
		return errors.New("query failed")
	}
	return nil
}
func (f *fakeStore) ListProjectResourceIndexAssistants(_ context.Context, _ uuid.UUID) ([]repo.ListProjectResourceIndexAssistantsRow, error) {
	return f.assistants, f.result("assistant")
}
func (f *fakeStore) ListProjectResourceIndexMCPServers(_ context.Context, _ uuid.UUID) ([]repo.ListProjectResourceIndexMCPServersRow, error) {
	return f.mcp, f.result("mcp")
}
func (f *fakeStore) ListProjectResourceIndexSkills(_ context.Context, _ uuid.UUID) ([]repo.ListProjectResourceIndexSkillsRow, error) {
	return f.skills, f.result("skill")
}
func (f *fakeStore) ListProjectResourceIndexEnvironments(_ context.Context, _ uuid.UUID) ([]repo.ListProjectResourceIndexEnvironmentsRow, error) {
	return f.environments, f.result("environment")
}
func (f *fakeStore) ListProjectResourceIndexGateways(_ context.Context, _ repo.ListProjectResourceIndexGatewaysParams) ([]repo.ListProjectResourceIndexGatewaysRow, error) {
	return f.gateways, f.result("gateway")
}
func (f *fakeStore) ListProjectResourceIndexPlugins(_ context.Context, _ repo.ListProjectResourceIndexPluginsParams) ([]repo.ListProjectResourceIndexPluginsRow, error) {
	return f.plugins, f.result("plugin")
}
func (f *fakeStore) ListProjectResourceIndexCustomRules(_ context.Context, _ repo.ListProjectResourceIndexCustomRulesParams) ([]repo.ListProjectResourceIndexCustomRulesRow, error) {
	return f.rules, f.result("rule")
}

type fakeAuthz struct{ denied authz.Scope }

func (f fakeAuthz) Evaluate(_ context.Context, checks ...authz.Check) (bool, error) {
	return checks[0].Scope != f.denied, nil
}
func (fakeAuthz) RequirePluginWrite(context.Context, string, string) error { return nil }

type fakeFeatures bool

func (f fakeFeatures) IsFeatureEnabled(context.Context, string, productfeatures.Feature) (bool, error) {
	return bool(f), nil
}
func testContext(project uuid.UUID) context.Context {
	return contextvalues.SetAuthContext(context.Background(), &contextvalues.AuthContext{ProjectID: &project, ActiveOrganizationID: "org_test"})
}
func testService(t *testing.T, store *fakeStore, a fakeAuthz) *Service {
	t.Helper()
	return &Service{logger: testenv.NewLogger(t), repo: store, authz: a, features: fakeFeatures(true)}
}
func fullStore() *fakeStore {
	ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()}
	return &fakeStore{calls: map[string]int{}, assistants: []repo.ListProjectResourceIndexAssistantsRow{{ID: ids[0], Name: "Assistant"}}, mcp: []repo.ListProjectResourceIndexMCPServersRow{{ID: ids[1], Name: pgtype.Text{String: "MCP", Valid: true}, Slug: pgtype.Text{String: "mcp", Valid: true}, RouteID: ids[1]}}, skills: []repo.ListProjectResourceIndexSkillsRow{{ID: ids[2], DisplayName: "Skill", Name: "skill"}}, environments: []repo.ListProjectResourceIndexEnvironmentsRow{{ID: ids[3], Name: "Environment", Slug: "environment"}}, gateways: []repo.ListProjectResourceIndexGatewaysRow{{ID: ids[4], Name: "Gateway"}}, plugins: []repo.ListProjectResourceIndexPluginsRow{{ID: ids[5], Name: "Plugin", Slug: "plugin"}}, rules: []repo.ListProjectResourceIndexCustomRulesRow{{ID: ids[6], Title: "Rule", RuleID: "custom.rule", Severity: "medium"}}}
}

func TestGetReturnsCompletePerKindMetadata(t *testing.T) {
	t.Parallel()
	store := fullStore()
	got, err := testService(t, store, fakeAuthz{}).Get(testContext(uuid.New()), &gen.GetPayload{})
	require.NoError(t, err)
	require.Len(t, got.Items, 7)
	require.Equal(t, "1", got.SchemaVersion)
	require.Len(t, got.Kinds, 7)
	for _, kind := range got.Kinds {
		require.True(t, kind.Complete)
		require.False(t, kind.Overflow)
		require.Equal(t, 1, kind.ItemCount)
		require.Equal(t, 1, store.calls[kind.Kind])
	}
}
func TestGetDoesNotCapLargeKind(t *testing.T) {
	t.Parallel()
	store := fullStore()
	store.assistants = make([]repo.ListProjectResourceIndexAssistantsRow, 250)
	for i := range store.assistants {
		store.assistants[i] = repo.ListProjectResourceIndexAssistantsRow{ID: uuid.New(), Name: "Assistant"}
	}
	got, err := testService(t, store, fakeAuthz{}).Get(testContext(uuid.New()), &gen.GetPayload{})
	require.NoError(t, err)
	require.Len(t, got.Items, 256)
	require.Equal(t, 250, got.Kinds[0].ItemCount)
	require.False(t, got.Kinds[0].Overflow)
}
func TestGetSkipsUnauthorizedKindButReportsCompleteZero(t *testing.T) {
	t.Parallel()
	store := fullStore()
	got, err := testService(t, store, fakeAuthz{denied: authz.ScopeOrgAdmin}).Get(testContext(uuid.New()), &gen.GetPayload{})
	require.NoError(t, err)
	require.Zero(t, store.calls["rule"])
	require.Equal(t, 0, got.Kinds[5].ItemCount)
	require.True(t, got.Kinds[5].Complete)
}
func TestGetFailsWholeResponseOnQueryError(t *testing.T) {
	t.Parallel()
	store := fullStore()
	store.errKind = "gateway"
	got, err := testService(t, store, fakeAuthz{}).Get(testContext(uuid.New()), &gen.GetPayload{})
	require.Error(t, err)
	require.Nil(t, got)
}
func TestReservedMCPRoutesUseUUID(t *testing.T) {
	t.Parallel()
	store := fullStore()
	store.mcp[0].Slug = pgtype.Text{String: "gateway", Valid: true}
	got, err := testService(t, store, fakeAuthz{}).Get(testContext(uuid.New()), &gen.GetPayload{})
	require.NoError(t, err)
	for _, item := range got.Items {
		if item.Kind == "mcp" {
			require.Equal(t, store.mcp[0].RouteID.String(), item.RouteSelector)
		}
	}
}
