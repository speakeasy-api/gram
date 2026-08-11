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

func TestAssistantPlatformSlugsManagedWithFlagOnGetsPlatformToolset(t *testing.T) {
	t.Parallel()

	svc, managedRecord, _ := platformSlugsFixture(t)

	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagAssistantPlatformMCP, "org-test", true)
	svc.core.SetFeatureProvider(flags)

	slugs, err := svc.core.assistantPlatformSlugs(t.Context(), managedRecord)
	require.NoError(t, err)
	require.Equal(t, []string{
		platformtools.AssistantsPlatformToolsetSlug,
		platformtools.ManagedAssistantPlatformToolsetSlug,
		platformtools.PlatformMCPReadToolsetSlug,
	}, slugs)
}

func TestAssistantPlatformSlugsManagedWithFlagOffUnchanged(t *testing.T) {
	t.Parallel()

	svc, managedRecord, _ := platformSlugsFixture(t)
	svc.core.SetFeatureProvider(&feature.InMemory{})

	slugs, err := svc.core.assistantPlatformSlugs(t.Context(), managedRecord)
	require.NoError(t, err)
	require.Equal(t, []string{
		platformtools.AssistantsPlatformToolsetSlug,
		platformtools.ManagedAssistantPlatformToolsetSlug,
	}, slugs)
}

func TestAssistantPlatformSlugsNilProviderFailsClosed(t *testing.T) {
	t.Parallel()

	svc, managedRecord, _ := platformSlugsFixture(t)

	slugs, err := svc.core.assistantPlatformSlugs(t.Context(), managedRecord)
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
	flags.SetFlag(feature.FlagAssistantPlatformMCP, "org-test", true)
	svc.core.SetFeatureProvider(flags)

	slugs, err := svc.core.assistantPlatformSlugs(t.Context(), otherRecord)
	require.NoError(t, err)
	require.Equal(t, []string{platformtools.AssistantsPlatformToolsetSlug}, slugs)
}
