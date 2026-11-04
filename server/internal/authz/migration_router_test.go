package authz

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	s "github.com/speakeasy-api/gram/server/internal/authz/schema"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

// switchableModes routes each unit to a mode the test can change at any
// time, and counts how often each unit is asked.
type switchableModes struct {
	mu    sync.Mutex
	modes map[string]MigrationMode
	asked map[string]int
}

func newSwitchableModes(modes map[string]MigrationMode) *switchableModes {
	return &switchableModes{mu: sync.Mutex{}, modes: modes, asked: make(map[string]int)}
}

func (m *switchableModes) MigrationMode(_ context.Context, unit, _ string) MigrationMode {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.asked[unit]++
	return m.modes[unit]
}

func (m *switchableModes) set(unit string, mode MigrationMode) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.modes[unit] = mode
}

func (m *switchableModes) timesAsked(unit string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.asked[unit]
}

// capturedComparisons records every shadow comparison.
type capturedComparisons struct {
	mu   sync.Mutex
	list []ShadowComparison
}

func (c *capturedComparisons) RecordShadowComparison(_ context.Context, comparison ShadowComparison) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.list = append(c.list, comparison)
}

func (c *capturedComparisons) all() []ShadowComparison {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]ShadowComparison(nil), c.list...)
}

func (c *capturedComparisons) outcomes() []ShadowOutcome {
	var out []ShadowOutcome
	for _, comparison := range c.all() {
		out = append(out, comparison.Outcome)
	}
	return out
}

func TestNewMigrationRouterValidatesManifests(t *testing.T) {
	t.Parallel()

	registry, err := DefaultRegistry()
	require.NoError(t, err)
	source := fixedModes(MigrationModeOld)

	_, err = NewMigrationRouter(registry, source, DefaultMigrationUnits()...)
	require.NoError(t, err, "the default units must match the default registry")

	cases := map[string][]MigrationUnit{
		"undeclared type":   {{Name: "undeclared", Types: []string{"undeclared"}, DependsOn: nil}},
		"no types":          {{Name: "empty", Types: nil, DependsOn: nil}},
		"invalid name":      {{Name: "Project", Types: []string{"project"}, DependsOn: nil}},
		"duplicate unit":    {{Name: "project", Types: []string{"project"}, DependsOn: nil}, {Name: "project", Types: []string{"organization"}, DependsOn: nil}},
		"type in two units": {{Name: "a", Types: []string{"project"}, DependsOn: nil}, {Name: "b", Types: []string{"project"}, DependsOn: nil}},
		"unknown dependency": {
			{Name: "project", Types: []string{"project"}, DependsOn: []string{"organization"}},
		},
		"dependency cycle": {
			{Name: "organization", Types: []string{"organization"}, DependsOn: []string{"project"}},
			{Name: "project", Types: []string{"project"}, DependsOn: []string{"organization"}},
		},
	}
	for name, units := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := NewMigrationRouter(registry, source, units...)
			require.Error(t, err)
		})
	}

	_, err = NewMigrationRouter(registry, nil, DefaultMigrationUnits()...)
	require.Error(t, err, "a router needs a mode source")
}

func TestRouteTakesTheMostConservativeMode(t *testing.T) {
	t.Parallel()

	registry, err := DefaultRegistry()
	require.NoError(t, err)
	project := Project(fixtureOrgID, "proj_1")

	cases := []struct {
		name  string
		modes map[string]MigrationMode
		ops   []Operation
		want  MigrationMode
	}{
		{
			name:  "a unit cannot run ahead of a unit it depends on",
			modes: map[string]MigrationMode{"organization": MigrationModeOld, "project": MigrationModeNew},
			ops:   []Operation{project.Read()},
			want:  MigrationModeOld,
		},
		{
			name:  "a dependency's shadow holds back its dependents",
			modes: map[string]MigrationMode{"organization": MigrationModeShadow, "project": MigrationModeNew, "mcp": MigrationModeNew},
			ops:   []Operation{newOperation("mcp_server", "read", "mcp_1", nil)},
			want:  MigrationModeShadow,
		},
		{
			name:  "a dependency ahead of its dependent does not move it",
			modes: map[string]MigrationMode{"organization": MigrationModeNew, "project": MigrationModeShadow},
			ops:   []Operation{project.Read()},
			want:  MigrationModeShadow,
		},
		{
			name:  "alternatives take the mode of every option",
			modes: map[string]MigrationMode{"organization": MigrationModeShadow, "project": MigrationModeNew},
			ops:   []Operation{project.PublishPlugin()},
			want:  MigrationModeShadow,
		},
		{
			name:  "one call runs every operation in one mode",
			modes: map[string]MigrationMode{"organization": MigrationModeNew, "project": MigrationModeNew, "mcp": MigrationModeShadow},
			ops:   []Operation{project.Read(), newOperation("mcp_server", "read", "mcp_1", nil)},
			want:  MigrationModeShadow,
		},
		{
			name:  "everything new decides new",
			modes: map[string]MigrationMode{"organization": MigrationModeNew, "project": MigrationModeNew},
			ops:   []Operation{project.Read(), project.PublishPlugin()},
			want:  MigrationModeNew,
		},
		{
			name:  "a type in no unit stays old",
			modes: map[string]MigrationMode{"organization": MigrationModeNew, "project": MigrationModeNew},
			ops:   []Operation{project.Read(), newOperation("chat", "read", "chat_1", nil)},
			want:  MigrationModeOld,
		},
		{
			name:  "the zero operation stays old",
			modes: map[string]MigrationMode{"organization": MigrationModeNew, "project": MigrationModeNew},
			ops:   []Operation{{}},
			want:  MigrationModeOld,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			router, err := NewMigrationRouter(registry, newSwitchableModes(tc.modes), DefaultMigrationUnits()...)
			require.NoError(t, err)
			ctx := humanClientContext(t)
			require.Equal(t, tc.want, router.routeFor(ctx, tc.ops).mode)
		})
	}
}

func TestRoutesStayOldWithoutPinsOrOrganization(t *testing.T) {
	t.Parallel()

	registry, err := DefaultRegistry()
	require.NoError(t, err)
	modes := newSwitchableModes(map[string]MigrationMode{"organization": MigrationModeNew, "project": MigrationModeNew})
	router, err := NewMigrationRouter(registry, modes, DefaultMigrationUnits()...)
	require.NoError(t, err)
	ops := []Operation{Project(fixtureOrgID, "proj_1").Read()}

	require.Equal(t, MigrationModeOld, router.routeFor(t.Context(), ops).mode, "a context no admission prepared is never routed new")
	var missing *MigrationRouter
	require.Equal(t, MigrationModeOld, missing.routeFor(humanClientContext(t), ops).mode, "no router keeps the engine deciding")

	orgless := withRoutePins(t.Context())
	require.Equal(t, MigrationModeOld, router.routeFor(orgless, ops).mode, "a request with no organization stays old")
	require.Zero(t, modes.timesAsked("project"), "no organization means no flag is read")
}

func TestRoutesArePinnedPerAuthorizationBoundary(t *testing.T) {
	t.Parallel()

	modes := newSwitchableModes(map[string]MigrationMode{"organization": MigrationModeShadow, "project": MigrationModeShadow})
	recorder := &capturedComparisons{}
	client, engine := newRoutedTestClient(t, nil, modes, recorder)
	project := Project(fixtureOrgID, "proj_1")
	role := "role:organization:role_1"

	ctx := humanClientContext(t, grantOf(role, string(ScopeProjectRead), ResourceKindProject, "proj_1"))
	require.NoError(t, client.Require(ctx, project.Read()))
	require.Len(t, recorder.all(), 1, "the first call is shadowed")

	// The flag flips mid-request. The request keeps its pinned routes, so
	// it neither starts deciding with the registry evaluator nor mixes the
	// two evaluators across its calls.
	modes.set("organization", MigrationModeNew)
	modes.set("project", MigrationModeNew)
	require.NoError(t, client.Require(ctx, project.Read()))
	require.Len(t, recorder.all(), 2, "the second call is still shadowed")
	require.Equal(t, 1, modes.timesAsked("project"), "each unit is read once per boundary")
	require.Equal(t, 1, modes.timesAsked("organization"), "dependencies are pinned with their dependents")

	// Re-preparing grants inside the same request keeps the pins.
	// TestRefreshContextKeepsPinnedRoutes covers the database-backed refresh.
	refreshed := GrantsToContext(ctx, []Grant{grantOf(role, string(ScopeProjectRead), ResourceKindProject, "proj_1")})
	require.Same(t, routePinsFromContext(ctx), routePinsFromContext(refreshed))

	// A job admitted under this request keeps its routes too.
	boundary, err := engine.CaptureAdmissionBoundary(ctx)
	require.NoError(t, err)
	job := boundary.Apply(humanClientContext(t, grantOf(role, string(ScopeProjectRead), ResourceKindProject, "proj_1")))
	require.NoError(t, client.Require(job, project.Read()))
	require.Len(t, recorder.all(), 3, "the job is shadowed like the request that admitted it")

	// A new request reads the flipped flag and decides with the registry
	// evaluator, so it records no comparison.
	next := humanClientContext(t, grantOf(role, string(ScopeProjectRead), ResourceKindProject, "proj_1"))
	require.NoError(t, client.Require(next, project.Read()))
	require.Len(t, recorder.all(), 3)
	require.Equal(t, 2, modes.timesAsked("project"))
}

func TestFeatureFlagModes(t *testing.T) {
	t.Parallel()

	flags := &feature.InMemory{}
	flags.SetFlagVariant(feature.RBACMigrationFlag("project"), "org_shadow", feature.RBACMigrationVariantShadow)
	flags.SetFlagVariant(feature.RBACMigrationFlag("project"), "org_new", feature.RBACMigrationVariantNew)
	source := FeatureFlagModes(flags)
	ctx := t.Context()

	require.Equal(t, MigrationModeShadow, source.MigrationMode(ctx, "project", "org_shadow"))
	require.Equal(t, MigrationModeNew, source.MigrationMode(ctx, "project", "org_new"))
	require.Equal(t, MigrationModeOld, source.MigrationMode(ctx, "project", "org_unset"))
	require.Equal(t, MigrationModeOld, source.MigrationMode(ctx, "organization", "org_new"), "each unit has its own flag")
	require.Equal(t, MigrationModeOld, FeatureFlagModes(nil).MigrationMode(ctx, "project", "org_new"), "no provider keeps the engine deciding")
}

// TestClientWithoutRouterKeepsEngineDecisions shows wiring the client changes
// nothing until a unit is routed: with a deliberately wrong registry, the
// client still answers exactly as the engine does.
func TestClientWithoutRouterKeepsEngineDecisions(t *testing.T) {
	t.Parallel()

	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	client, err := NewClient(engine, brokenProjectRegistry(t))
	require.NoError(t, err)
	ctx := blockedReaderContext(t)
	err = client.Require(ctx, Project(fixtureOrgID, "proj_1").Read())
	require.Error(t, err)
	require.NoError(t, ignoreForbidden(err), "the engine denies the blocked read")
}

// brokenProjectRegistry compiles every default type, but its project read ignores the read
// block the scope table applies, so it allows reads the engine denies.
func brokenProjectRegistry(t *testing.T) *s.Registry {
	t.Helper()
	access := string(GrantSurfaceAccess)
	var decls []s.TypeDecl
	for _, decl := range DefaultDeclarations() {
		if decl.Name() != "project" {
			decls = append(decls, decl)
		}
	}
	decls = append(decls, s.NewType("project", s.Parent("organization"), s.GrantSurface(access),
		s.Grant(ScopeProjectRead, s.GrantSpec{AnchoredAt: []string{"organization", s.Self}, Description: "Read the project"}),
		s.Grant(ScopeProjectWrite, s.GrantSpec{AnchoredAt: []string{"organization", s.Self}, Description: "Change the project"}),
		s.Grant(ScopePluginWrite, s.GrantSpec{AnchoredAt: []string{"organization", s.Self}, Description: "Publish plugins"}),
		s.Grant(ScopeLogRead, s.GrantSpec{AnchoredAt: []string{"organization", s.Self}, Description: "Read tool-call logs", Sensitive: true}),
		s.Block(ScopeProjectBlockedRead, s.BlockSpec{}),
		s.Block(ScopeProjectBlockedWrite, s.BlockSpec{ImpliedBy: []string{string(ScopeProjectBlockedRead)}}),
		s.Block(ScopePluginBlockedWrite, s.BlockSpec{}),
		s.Permission("read", s.Union(s.Rel(ScopeProjectRead), s.Rel(ScopeProjectWrite)), s.AnswersScope(string(ScopeProjectRead))),
		s.Permission("write", s.Rel(ScopeProjectWrite), s.AnswersScope(string(ScopeProjectWrite)), s.ExcludedBy(ScopeProjectBlockedWrite)),
		s.Permission("publish_plugin", s.Rel(ScopePluginWrite), s.AnswersScope(string(ScopePluginWrite)), s.ExcludedBy(ScopePluginBlockedWrite)),
		s.Permission("read_logs", s.Rel(ScopeLogRead), s.AnswersScope(string(ScopeLogRead))),
		s.Alternatives("plugin_publication", s.AlternativesSpec{Options: []s.PermissionRef{{Type: "project", Permission: "publish_plugin"}, {Type: "organization", Permission: "administer"}}, Blocks: s.AnyBlockVetoes}),
		s.Alternatives("read_or_organization_read", s.AlternativesSpec{Options: []s.PermissionRef{{Type: "organization", Permission: "read"}, {Type: "project", Permission: "read"}}, Blocks: s.BlocksApplyPerOption}),
		s.Alternatives("write_or_administer", s.AlternativesSpec{Options: []s.PermissionRef{{Type: "project", Permission: "write"}, {Type: "organization", Permission: "administer"}}, Blocks: s.BlocksApplyPerOption}),
	))
	registry, err := s.Compile(DefaultRegistryConfig(), decls...)
	require.NoError(t, err)
	return registry
}

// blockedReaderContext holds a project read grant and a read block on the
// same project: the engine denies the read.
func blockedReaderContext(t *testing.T) context.Context {
	t.Helper()
	role := "role:organization:role_1"
	return humanClientContext(t,
		grantOf(role, string(ScopeProjectRead), ResourceKindProject, "proj_1"),
		grantOf(role, string(ScopeProjectBlockedRead), ResourceKindProject, "proj_1"),
	)
}
