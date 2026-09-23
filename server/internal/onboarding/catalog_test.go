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

func TestCatalogValidateRejectsCrossVendorPlan(t *testing.T) {
	t.Parallel()
	bad := onboarding.Catalog{
		Plans:        []onboarding.PlanSpec{{Slug: "cursor-pro", Vendor: "cursor", Name: "Pro"}},
		Products:     []onboarding.ProductSpec{{Slug: "codex", Name: "Codex", Vendor: "openai", SourceIDs: []string{"codex"}, Plans: []string{"cursor-pro"}, Techniques: nil}},
		Techniques:   nil,
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

	productPlans, err := queries.ListOnboardingProductPlans(ctx)
	require.NoError(t, err)
	var expectedProductPlans int
	for _, p := range onboarding.Default.Products {
		expectedProductPlans += len(p.Plans)
	}
	require.Len(t, productPlans, expectedProductPlans)
}

func TestSyncCatalogDropsRemovedRelations(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	logger := testLogger(t)

	trimmed := onboarding.Default
	trimmed.Products = slices.Clone(onboarding.Default.Products)
	for i := range trimmed.Products {
		if trimmed.Products[i].Slug == onboarding.ProductCursor {
			trimmed.Products[i].Plans = []string{onboarding.PlanCursorPro}
			// The admin API gate names the dropped plans, so it goes too.
			trimmed.Products[i].Techniques = []onboarding.TechniqueSupport{{Technique: onboarding.TechniqueCursorHooks, Plans: nil}}
		}
	}
	require.NoError(t, onboarding.SyncCatalog(ctx, logger, ti.conn, trimmed))

	productPlans, err := repo.New(ti.conn).ListOnboardingProductPlans(ctx)
	require.NoError(t, err)
	products, err := repo.New(ti.conn).ListOnboardingProducts(ctx)
	require.NoError(t, err)
	cursorIndex := slices.IndexFunc(products, func(p repo.OnboardingProduct) bool { return p.Slug == onboarding.ProductCursor })
	require.GreaterOrEqual(t, cursorIndex, 0)
	var cursorPlans []string
	for _, pp := range productPlans {
		if pp.ProductID == products[cursorIndex].ID {
			cursorPlans = append(cursorPlans, pp.PlanSlug)
		}
	}
	require.Equal(t, []string{onboarding.PlanCursorPro}, cursorPlans)
}
