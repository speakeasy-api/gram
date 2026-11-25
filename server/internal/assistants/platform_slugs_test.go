package assistants

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/assistants"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
)

// platformSlugsFixture provisions the managed assistant plus a sibling and
// returns records shaped for assistantPlatformSlugs.
func platformSlugsFixture(t *testing.T) (*Service, assistantRecord, assistantRecord) {
	t.Helper()

	svc, ctx, projectID, conn := newRBACServiceWithConn(t, "assistants_platform_slugs")
	ensureAssistantTestOrganization(t, conn)
	ctx = authztest.WithExactGrants(t, ctx, assistantWriteGrant(projectID))

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

// The managed assistant gets the Platform MCP read toolset on top of the base
// assistants toolset.
func TestAssistantPlatformSlugsManagedGetsPlatformMCP(t *testing.T) {
	t.Parallel()

	svc, managedRecord, _ := platformSlugsFixture(t)

	slugs, err := svc.core.assistantPlatformSlugs(t.Context(), managedRecord)
	require.NoError(t, err)
	require.Equal(t, []string{
		platformtools.AssistantsPlatformToolsetSlug,
		platformtools.PlatformMCPReadToolsetSlug,
	}, slugs)
}

func TestAssistantPlatformSlugsNonManagedNeverGetsPlatformToolset(t *testing.T) {
	t.Parallel()

	svc, _, otherRecord := platformSlugsFixture(t)

	slugs, err := svc.core.assistantPlatformSlugs(t.Context(), otherRecord)
	require.NoError(t, err)
	require.Equal(t, []string{platformtools.AssistantsPlatformToolsetSlug}, slugs)
}
