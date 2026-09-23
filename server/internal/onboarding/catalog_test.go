package onboarding_test

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/onboarding"
	"github.com/speakeasy-api/gram/server/internal/onboarding/repo"
)

// adapterSourceIDs are the hook_source values written by pipelines other than
// the canonical chat sources: the unified-ingest adapters (the plugin
// generator's platform switch), LiteLLM's adapter slug, Claude Tag's service
// name and the OpenAI compliance import's Work tag.
var adapterSourceIDs = []string{"opencode", "copilot", "openclaw", "pi", "litellm", "claude-tag", "chatgpt-work"}

func TestDefaultCatalogValidates(t *testing.T) {
	t.Parallel()
	require.NoError(t, onboarding.Default.Validate())
}

func TestDefaultCatalogSourceIDsMatchIngest(t *testing.T) {
	t.Parallel()
	known := append(chat.KnownSources(), adapterSourceIDs...)
	for _, product := range onboarding.Default.Products {
		for _, id := range product.SourceIDs {
			require.Truef(t, slices.Contains(known, id), "product %q names source %q, which no ingest path writes", product.Slug, id)
		}
	}
}

func TestDefaultCatalogEveryUseCaseHasACapability(t *testing.T) {
	t.Parallel()
	for _, useCase := range onboarding.UseCases {
		require.Truef(t, slices.ContainsFunc(onboarding.Default.Capabilities, func(c onboarding.CapabilitySpec) bool { return c.UseCase == useCase }), "use case %q has no capability", useCase)
	}
}

func TestDefaultCatalogEveryProductHasAProvider(t *testing.T) {
	t.Parallel()
	for _, product := range onboarding.Default.Products {
		_, ok := onboarding.Default.Provider(product.Provider)
		require.Truef(t, ok, "product %q names unknown provider %q", product.Slug, product.Provider)
	}
	require.NotEmpty(t, onboarding.Default.PlansFor(onboarding.ProviderAnthropic))
	require.Empty(t, onboarding.Default.PlansFor(onboarding.ProviderOpenCode))
}

func TestCatalogValidateRejectsGateOnAnotherProvidersPlan(t *testing.T) {
	t.Parallel()
	bad := onboarding.Catalog{
		Providers: []onboarding.ProviderSpec{{Slug: "cursor", Name: "Cursor"}, {Slug: "openai", Name: "OpenAI"}},
		Plans:     []onboarding.PlanSpec{{Slug: "cursor-pro", Provider: "cursor", Name: "Pro"}},
		Products: []onboarding.ProductSpec{{Slug: "codex", Name: "Codex", Provider: "openai", SourceIDs: []string{"codex"}, Techniques: []onboarding.TechniqueSupport{
			{Technique: "codex-hooks", Plans: []string{"cursor-pro"}},
		}}},
		Techniques:   []onboarding.TechniqueSpec{{Slug: "codex-hooks", Name: "Codex hooks", Description: "", Capabilities: nil}},
		Capabilities: nil,
	}
	require.ErrorContains(t, bad.Validate(), "sold by cursor")
}

func TestSyncCatalogIsRepeatable(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	logger := testLogger(t)

	require.NoError(t, onboarding.SyncCatalog(ctx, logger, ti.conn, onboarding.Default))
	require.NoError(t, onboarding.SyncCatalog(ctx, logger, ti.conn, onboarding.Default))

	queries := repo.New(ti.conn)
	providers, err := queries.ListOnboardingProviders(ctx)
	require.NoError(t, err)
	require.Len(t, providers, len(onboarding.Default.Providers))
	products, err := queries.ListOnboardingProducts(ctx)
	require.NoError(t, err)
	require.Len(t, products, len(onboarding.Default.Products))
	plans, err := queries.ListOnboardingPlans(ctx)
	require.NoError(t, err)
	require.Len(t, plans, len(onboarding.Default.Plans))
	techniques, err := queries.ListOnboardingTechniques(ctx)
	require.NoError(t, err)
	require.Len(t, techniques, len(onboarding.Default.Techniques))
	capabilities, err := queries.ListOnboardingCapabilities(ctx)
	require.NoError(t, err)
	require.Len(t, capabilities, len(onboarding.Default.Capabilities))

	for _, plan := range plans {
		spec, ok := onboarding.Default.Plan(plan.Slug)
		require.True(t, ok, plan.Slug)
		require.Equal(t, spec.Provider, plan.ProviderSlug)
	}
}

func TestSyncCatalogMovesAPlanToItsNewProvider(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	logger := testLogger(t)

	moved := onboarding.Default
	moved.Plans = slices.Clone(onboarding.Default.Plans)
	for i := range moved.Plans {
		if moved.Plans[i].Slug == onboarding.PlanCursorPro {
			moved.Plans[i].Provider = onboarding.ProviderGitHub
		}
	}
	// The cursor product gates a technique on cursor plans; drop the gate so
	// the moved catalog still validates.
	moved.Products = slices.Clone(onboarding.Default.Products)
	for i := range moved.Products {
		if moved.Products[i].Slug == onboarding.ProductCursor {
			moved.Products[i].Techniques = []onboarding.TechniqueSupport{{Technique: onboarding.TechniqueCursorHooks, Plans: nil}}
		}
	}
	require.NoError(t, onboarding.SyncCatalog(ctx, logger, ti.conn, moved))

	plans, err := repo.New(ti.conn).ListOnboardingPlans(ctx)
	require.NoError(t, err)
	index := slices.IndexFunc(plans, func(p repo.ListOnboardingPlansRow) bool { return p.Slug == onboarding.PlanCursorPro })
	require.GreaterOrEqual(t, index, 0)
	require.Equal(t, onboarding.ProviderGitHub, plans[index].ProviderSlug)
}
