package authz

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

func newBrokenShadowClient(t *testing.T, mode MigrationMode, recorder ShadowRecorder) *Client {
	t.Helper()
	registry := brokenProjectRegistry(t)
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	router, err := NewMigrationRouter(registry, fixedModes(mode),
		MigrationUnit{Name: "organization", Types: []string{"organization"}, DependsOn: nil},
		MigrationUnit{Name: "project", Types: []string{"project"}, DependsOn: []string{"organization"}},
	)
	require.NoError(t, err)
	client, err := NewClient(engine, registry, WithMigrationRouter(router), WithShadowRecorder(recorder), WithBuildVersion("test"))
	require.NoError(t, err)
	return client
}

// TestShadowDetectsBrokenProjection is the acceptance check for shadow
// comparisons: a registry that compiles but ignores a read block is caught
// as new_allow on every entry point, while the engine keeps answering.
func TestShadowDetectsBrokenProjection(t *testing.T) {
	t.Parallel()

	read := Project(fixtureOrgID, "proj_1").Read()

	t.Run("new mode would allow the blocked read", func(t *testing.T) {
		t.Parallel()
		client := newBrokenShadowClient(t, MigrationModeNew, &capturedComparisons{})
		require.NoError(t, client.Require(blockedReaderContext(t), read), "the broken projection allows what the engine denies")
	})

	t.Run("shadow mode keeps the engine's denial and records it", func(t *testing.T) {
		t.Parallel()
		recorder := &capturedComparisons{}
		client := newBrokenShadowClient(t, MigrationModeShadow, recorder)
		ctx := blockedReaderContext(t)

		err := client.Require(ctx, read)
		require.Error(t, err)
		require.NoError(t, ignoreForbidden(err), "the response is the engine's denial")

		allowed, err := client.Evaluate(ctx, read)
		require.NoError(t, err)
		require.False(t, allowed)

		matched, err := client.MatchMany(ctx, []Operation{read, Project(fixtureOrgID, "proj_2").Read()})
		require.NoError(t, err)
		require.Equal(t, []bool{false, false}, matched)

		comparisons := recorder.all()
		require.Len(t, comparisons, 3)
		for i, entry := range []string{"require", "evaluate", "match_many"} {
			c := comparisons[i]
			require.Equal(t, entry, c.Entry)
			require.Equal(t, ShadowNewAllow, c.Outcome)
			require.Equal(t, []string{"project"}, c.Units)
			require.Equal(t, "test", c.BuildVersion)
			require.Equal(t, client.registry.Fingerprint(), c.ModelVersion)
			require.Equal(t, fixtureOrgID, c.OrganizationID)
		}
		require.Equal(t, []string{"project.read"}, comparisons[0].Permissions, "records name permissions, never resource IDs")

		defaultRegistry, err := DefaultRegistry()
		require.NoError(t, err)
		require.NotEqual(t, defaultRegistry.Fingerprint(), comparisons[0].ModelVersion, "the model version tells the broken model apart")
	})
}

// TestShadowFoundationCorpus runs the frozen corpus with every unit in shadow
// mode. Responses must still meet the frozen expectations, because the
// engine decides, and the default registry must agree with the engine on
// every case it can compare.
func TestShadowFoundationCorpus(t *testing.T) {
	t.Parallel()

	var compared, matched atomic.Int64
	// Cleanup runs once every parallel case has finished.
	t.Cleanup(func() {
		t.Logf("shadow corpus: %d comparisons, %d matches", compared.Load(), matched.Load())
		require.Positive(t, matched.Load(), "the corpus must exercise shadow comparisons")
		require.Equal(t, compared.Load(), matched.Load(), "the corpus carries no facts and runs within budget, so every comparison must match")
	})
	for _, fixture := range clientCorpusFixtures(t) {
		t.Run(fixture.Name, func(t *testing.T) {
			t.Parallel()
			recorder := &capturedComparisons{}
			client, engine := newRoutedTestClient(t, nil, fixedModes(MigrationModeShadow), recorder)
			runClientCorpusCase(t, client, engine, fixture)
			for _, c := range recorder.all() {
				require.False(t, c.Outcome.Disagrees(), "the registry disagrees with the engine: %+v", c)
				compared.Add(1)
				if c.Outcome == ShadowMatch {
					matched.Add(1)
				}
			}
		})
	}
}

func TestNewAuthorityErrorsFailClosed(t *testing.T) {
	t.Parallel()

	client, _ := newTestClient(t)
	ctx := humanClientContext(t, grantOf("role:organization:role_1", string(ScopeProjectRead), ResourceKindProject, "*"))
	undecidable := newOperation("project", "read", "proj_1", map[string]string{"unknown_dimension": "x"})

	err := client.Require(ctx, undecidable)
	require.Error(t, err)
	require.Error(t, ignoreForbidden(err), "an undecidable request is an error, never an allow")

	allowed, err := client.Evaluate(ctx, undecidable)
	require.Error(t, err)
	require.False(t, allowed)

	matched, err := client.MatchMany(ctx, []Operation{undecidable})
	require.Error(t, err)
	require.Nil(t, matched)
}

func TestMatchManyRejectsAlternativesInEveryMode(t *testing.T) {
	t.Parallel()

	for _, mode := range []MigrationMode{MigrationModeOld, MigrationModeShadow, MigrationModeNew} {
		t.Run(mode.String(), func(t *testing.T) {
			t.Parallel()
			client, _ := newRoutedTestClient(t, nil, fixedModes(mode), &capturedComparisons{})
			ctx := humanClientContext(t, grantOf("role:organization:role_1", string(ScopeOrgAdmin), ResourceKindOrg, fixtureOrgID))
			_, err := client.MatchMany(ctx, []Operation{Project(fixtureOrgID, "proj_1").PublishPlugin()})
			require.Error(t, err)
			require.Error(t, ignoreForbidden(err))
		})
	}
}

func TestOldModeMatchesTheEngine(t *testing.T) {
	t.Parallel()

	client, engine := newRoutedTestClient(t, nil, fixedModes(MigrationModeOld), &capturedComparisons{})
	role := "role:organization:role_1"
	project := Project(fixtureOrgID, "proj_1")
	ctx := humanClientContext(t,
		grantOf(role, string(ScopeOrgAdmin), ResourceKindOrg, fixtureOrgID),
		grantOf(role, string(ScopePluginBlockedWrite), ResourceKindProject, "proj_1"),
	)

	org := Organization(fixtureOrgID)
	require.NoError(t, client.Require(ctx, org.Administer()))
	err := client.Require(ctx, project.PublishPlugin())
	require.Error(t, err)
	require.NoError(t, ignoreForbidden(err), "the block vetoes publication through RequireAnyUnblocked")
	require.Equal(t,
		engine.RequireAnyUnblocked(ctx, Check{Scope: ScopePluginWrite, ResourceKind: ResourceKindProject, ResourceID: "proj_1", Dimensions: nil}, Check{Scope: ScopeOrgAdmin, ResourceKind: ResourceKindOrg, ResourceID: fixtureOrgID, Dimensions: nil}) == nil,
		err == nil)

	allowed, err := client.Evaluate(ctx, project.PublishPlugin())
	require.NoError(t, err)
	require.False(t, allowed, "a forbidden alternative evaluates to false, not an error")

	allowed, err = client.Evaluate(ctx, org.Read(), org.Administer())
	require.NoError(t, err)
	require.True(t, allowed)
}

func newTestComparer(t *testing.T, budget ShadowBudget) (*shadowComparer, *capturedComparisons) {
	t.Helper()
	recorder := &capturedComparisons{}
	comparer, err := newShadowComparer(budget, recorder, "test", "model")
	require.NoError(t, err)
	return comparer, recorder
}

var testRoute = route{mode: MigrationModeShadow, units: []string{"project"}}

func allow() shadowAnswer { return shadowAnswer{allowed: []bool{true}, err: nil} }
func deny() shadowAnswer  { return shadowAnswer{allowed: []bool{false}, err: nil} }

func TestShadowComparerOutcomes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		old    shadowAnswer
		got    shadowAnswer
		want   ShadowOutcome
		detail string
	}{
		{"match", allow(), allow(), ShadowMatch, ""},
		{"new allow", deny(), allow(), ShadowNewAllow, ""},
		{"new deny", allow(), deny(), ShadowNewDeny, ""},
		{"new error", allow(), shadowAnswer{allowed: nil, err: ErrMissingGrants}, ShadowNewError, "new_missing_grants"},
		{"same error category", shadowAnswer{allowed: nil, err: fmt.Errorf("mapped: %w", ErrInvalidCheck)}, shadowAnswer{allowed: nil, err: ErrInvalidCheck}, ShadowMatch, ""},
		{"changed error category", shadowAnswer{allowed: nil, err: ErrMissingGrants}, shadowAnswer{allowed: nil, err: ErrInvalidCheck}, ShadowNewError, "engine_missing_grants_new_invalid_operation"},
		{"allow where the engine errored", shadowAnswer{allowed: nil, err: ErrInvalidCheck}, allow(), ShadowNewAllow, "engine_invalid_operation"},
		{"deny where the engine errored", shadowAnswer{allowed: nil, err: ErrInvalidCheck}, deny(), ShadowNewError, "engine_invalid_operation_new_forbidden"},
		{"missing facts are incomparable", allow(), shadowAnswer{allowed: nil, err: fmt.Errorf("decide: %w", ErrFactUnresolved)}, ShadowIncomparable, "facts_unavailable"},
		{"unclassified errors", shadowAnswer{allowed: nil, err: errors.New("engine")}, shadowAnswer{allowed: nil, err: errors.New("new")}, ShadowMatch, ""},
		{"a batch with any new allow is new allow", shadowAnswer{allowed: []bool{true, false}, err: nil}, shadowAnswer{allowed: []bool{false, true}, err: nil}, ShadowNewAllow, ""},
		{"a batch with only new denies is new deny", shadowAnswer{allowed: []bool{true, true}, err: nil}, shadowAnswer{allowed: []bool{true, false}, err: nil}, ShadowNewDeny, ""},
		{"a batch of another length is new error", shadowAnswer{allowed: []bool{true, true}, err: nil}, allow(), ShadowNewError, "answer_shape"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			comparer, recorder := newTestComparer(t, DefaultShadowBudget)
			comparer.compare(t.Context(), "require", testRoute, nil, tc.old, func() shadowAnswer { return tc.got })
			got := recorder.all()
			require.Len(t, got, 1)
			require.Equal(t, tc.want, got[0].Outcome)
			require.Equal(t, tc.detail, got[0].Detail)
		})
	}
}

func TestShadowComparerNeverCountsUnfinishedWorkAsParity(t *testing.T) {
	t.Parallel()

	t.Run("timeout", func(t *testing.T) {
		t.Parallel()
		comparer, recorder := newTestComparer(t, ShadowBudget{MaxConcurrent: 1, Timeout: 10 * time.Millisecond})
		release := make(chan struct{})
		defer close(release)
		comparer.compare(t.Context(), "require", testRoute, nil, allow(), func() shadowAnswer {
			<-release
			return allow()
		})
		require.Equal(t, []ShadowOutcome{ShadowFailed}, recorder.outcomes())
		require.Equal(t, "timeout", recorder.all()[0].Detail)
	})

	t.Run("cancelled while comparing", func(t *testing.T) {
		t.Parallel()
		comparer, recorder := newTestComparer(t, ShadowBudget{MaxConcurrent: 1, Timeout: time.Minute})
		ctx, cancel := context.WithCancel(t.Context())
		release := make(chan struct{})
		defer close(release)
		comparer.compare(ctx, "require", testRoute, nil, allow(), func() shadowAnswer {
			cancel()
			<-release
			return allow()
		})
		require.Equal(t, []ShadowOutcome{ShadowFailed}, recorder.outcomes())
		require.Equal(t, "canceled", recorder.all()[0].Detail)
	})

	t.Run("cancelled before comparing", func(t *testing.T) {
		t.Parallel()
		comparer, recorder := newTestComparer(t, DefaultShadowBudget)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		ran := false
		comparer.compare(ctx, "require", testRoute, nil, allow(), func() shadowAnswer {
			ran = true
			return allow()
		})
		require.False(t, ran)
		require.Equal(t, []ShadowOutcome{ShadowFailed}, recorder.outcomes())
	})

	t.Run("budget exhausted", func(t *testing.T) {
		t.Parallel()
		comparer, recorder := newTestComparer(t, ShadowBudget{MaxConcurrent: 1, Timeout: 5 * time.Millisecond})
		release := make(chan struct{})
		// The first comparison times out but its evaluation keeps the only
		// slot until it finishes.
		comparer.compare(t.Context(), "require", testRoute, nil, allow(), func() shadowAnswer {
			<-release
			return allow()
		})
		comparer.compare(t.Context(), "require", testRoute, nil, allow(), allow)
		require.Equal(t, []ShadowOutcome{ShadowFailed, ShadowSkipped}, recorder.outcomes())
		require.Equal(t, "concurrency_budget", recorder.all()[1].Detail)

		close(release)
		require.Eventually(t, func() bool { return len(comparer.slots) == 0 }, time.Second, time.Millisecond)
		comparer.compare(t.Context(), "require", testRoute, nil, allow(), allow)
		require.Equal(t, ShadowMatch, recorder.outcomes()[2], "a released slot is reused")
	})

	t.Run("panic", func(t *testing.T) {
		t.Parallel()
		comparer, recorder := newTestComparer(t, DefaultShadowBudget)
		comparer.compare(t.Context(), "require", testRoute, nil, allow(), func() shadowAnswer { panic("boom") })
		require.Equal(t, []ShadowOutcome{ShadowNewError}, recorder.outcomes())
		require.Equal(t, "new_panic", recorder.all()[0].Detail)
	})
}

func TestNewShadowComparerRejectsInvalidBudgets(t *testing.T) {
	t.Parallel()

	_, err := newShadowComparer(ShadowBudget{MaxConcurrent: 0, Timeout: time.Second}, &capturedComparisons{}, "", "")
	require.Error(t, err)
	_, err = newShadowComparer(ShadowBudget{MaxConcurrent: 1, Timeout: 0}, &capturedComparisons{}, "", "")
	require.Error(t, err)
	_, err = newShadowComparer(DefaultShadowBudget, nil, "", "")
	require.Error(t, err)
}
