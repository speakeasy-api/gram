package assistants

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/assistants"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
)

// platformSlugsFixture provisions the managed assistant plus a sibling and
// returns records shaped for assistantPlatformSlugs.
func platformSlugsFixture(t *testing.T) (*Service, assistantRecord, assistantRecord) {
	t.Helper()

	svc, ctx, projectID, conn := newRBACServiceWithConn(t, "assistants_platform_slugs")
	ensureAssistantTestOrganization(t, conn)
	ctx = authztest.WithExactGrants(t, ctx, projectWriteGrant(projectID))

	managed, err := svc.EnsureManagedAssistant(ctx, &gen.EnsureManagedAssistantPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	managedID, err := uuid.Parse(managed.ID)
	require.NoError(t, err)

	other, err := svc.CreateAssistant(ctx, &gen.CreateAssistantPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Name:             "Sibling",
		Model:            "openai/gpt-4o-mini",
		Instructions:     "",
		Toolsets:         nil,
		McpServers:       nil,
		WarmTTLSeconds:   nil,
		MaxConcurrency:   nil,
		Status:           nil,
	})
	require.NoError(t, err)
	otherID, err := uuid.Parse(other.ID)
	require.NoError(t, err)

	managedRecord := assistantRecord{ID: managedID, ProjectID: projectID}
	otherRecord := assistantRecord{ID: otherID, ProjectID: projectID}
	return svc, managedRecord, otherRecord
}

// The platformmcp variant REPLACES the whole legacy platformtools surface
// rather than adding to it — the base assistants toolset included. An org on
// one variant must never see the other's tools.
func TestAssistantPlatformSlugsManagedPlatformMCPVariantReplacesLegacy(t *testing.T) {
	t.Parallel()

	svc, managedRecord, _ := platformSlugsFixture(t)

	flags := &feature.InMemory{}
	flags.SetFlagVariant(feature.FlagAssistantPlatformMCP, "org-test", feature.VariantAssistantToolsPlatformMCP)
	svc.core.SetFeatureProvider(flags)

	slugs, err := svc.core.assistantPlatformSlugs(t.Context(), managedRecord, sourceKindDashboard)
	require.NoError(t, err)
	require.Equal(t, []string{platformtools.PlatformMCPReadToolsetSlug}, slugs)
}

// The rollout is scoped to the dashboard: the same managed assistant answering
// Slack keeps the legacy toolsets, so flipping an organization never strips a
// Slack thread of memory, triggers and the insights tools.
func TestAssistantPlatformSlugsNonDashboardThreadStaysLegacy(t *testing.T) {
	t.Parallel()

	svc, managedRecord, _ := platformSlugsFixture(t)

	flags := &feature.InMemory{}
	flags.SetFlagVariant(feature.FlagAssistantPlatformMCP, "org-test", feature.VariantAssistantToolsPlatformMCP)
	svc.core.SetFeatureProvider(flags)

	for _, sourceKind := range []string{
		sourceKindSlack,
		sourceKindMSTeams,
		sourceKindLinear,
		sourceKindGithub,
		sourceKindCron,
		sourceKindWake,
		sourceKindWarmup,
		sourceKindSetup,
	} {
		slugs, err := svc.core.assistantPlatformSlugs(t.Context(), managedRecord, sourceKind)
		require.NoError(t, err)
		require.Equal(t, []string{
			platformtools.AssistantsPlatformToolsetSlug,
			platformtools.ManagedAssistantPlatformToolsetSlug,
		}, slugs, "source kind %q", sourceKind)
	}
}

func TestAssistantPlatformSlugsManagedLegacyVariantUnchanged(t *testing.T) {
	t.Parallel()

	svc, managedRecord, _ := platformSlugsFixture(t)

	flags := &feature.InMemory{}
	flags.SetFlagVariant(feature.FlagAssistantPlatformMCP, "org-test", feature.VariantAssistantToolsLegacy)
	svc.core.SetFeatureProvider(flags)

	slugs, err := svc.core.assistantPlatformSlugs(t.Context(), managedRecord, sourceKindDashboard)
	require.NoError(t, err)
	require.Equal(t, []string{
		platformtools.AssistantsPlatformToolsetSlug,
		platformtools.ManagedAssistantPlatformToolsetSlug,
	}, slugs)
}

// An unrecognized variant key (e.g. a PostHog variant renamed out from under
// the server) must land on legacy, not on no toolset at all.
func TestAssistantPlatformSlugsUnknownVariantFallsBackToLegacy(t *testing.T) {
	t.Parallel()

	svc, managedRecord, _ := platformSlugsFixture(t)

	flags := &feature.InMemory{}
	flags.SetFlagVariant(feature.FlagAssistantPlatformMCP, "org-test", feature.Variant("control"))
	svc.core.SetFeatureProvider(flags)

	slugs, err := svc.core.assistantPlatformSlugs(t.Context(), managedRecord, sourceKindDashboard)
	require.NoError(t, err)
	require.Equal(t, []string{
		platformtools.AssistantsPlatformToolsetSlug,
		platformtools.ManagedAssistantPlatformToolsetSlug,
	}, slugs)
}

func TestAssistantPlatformSlugsNoVariantUnchanged(t *testing.T) {
	t.Parallel()

	svc, managedRecord, _ := platformSlugsFixture(t)
	svc.core.SetFeatureProvider(&feature.InMemory{})

	slugs, err := svc.core.assistantPlatformSlugs(t.Context(), managedRecord, sourceKindDashboard)
	require.NoError(t, err)
	require.Equal(t, []string{
		platformtools.AssistantsPlatformToolsetSlug,
		platformtools.ManagedAssistantPlatformToolsetSlug,
	}, slugs)
}

func TestAssistantPlatformSlugsNilProviderFallsBackToLegacy(t *testing.T) {
	t.Parallel()

	svc, managedRecord, _ := platformSlugsFixture(t)

	slugs, err := svc.core.assistantPlatformSlugs(t.Context(), managedRecord, sourceKindDashboard)
	require.NoError(t, err)
	require.Equal(t, []string{
		platformtools.AssistantsPlatformToolsetSlug,
		platformtools.ManagedAssistantPlatformToolsetSlug,
	}, slugs)
}

func TestAssistantPlatformSlugsNonManagedNeverGetsPlatformToolset(t *testing.T) {
	t.Parallel()

	svc, _, otherRecord := platformSlugsFixture(t)

	flags := &feature.InMemory{}
	flags.SetFlagVariant(feature.FlagAssistantPlatformMCP, "org-test", feature.VariantAssistantToolsPlatformMCP)
	svc.core.SetFeatureProvider(flags)

	slugs, err := svc.core.assistantPlatformSlugs(t.Context(), otherRecord, sourceKindDashboard)
	require.NoError(t, err)
	require.Equal(t, []string{platformtools.AssistantsPlatformToolsetSlug}, slugs)
}
