package assistants

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/assistants"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestEnsureManagedAssistantProvisionsAndIsIdempotent(t *testing.T) {
	t.Parallel()

	svc, ctx, projectID, conn := newRBACServiceWithConn(t, "assistants_ensure_managed")
	ctx = authztest.WithExactGrants(t, ctx, projectWriteGrant(projectID))

	// First call provisions the built-in assistant out of nothing.
	first, err := svc.EnsureManagedAssistant(ctx, &gen.EnsureManagedAssistantPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.NotEmpty(t, first.ID)
	require.Contains(t, first.Name, "Project Assistant")
	managedID, err := uuid.Parse(first.ID)
	require.NoError(t, err)
	skill, version := createSkillAttachmentFixture(t, conn, projectID, managedID, "managed-skill", "user-test")

	// Second call returns the same assistant — safe to call on every sidebar open.
	second, err := svc.EnsureManagedAssistant(ctx, &gen.EnsureManagedAssistantPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, first.ID, second.ID)
	require.Len(t, second.Skills, 1)
	require.Equal(t, skill.ID.String(), second.Skills[0].SkillID)
	require.Equal(t, version.ID.String(), second.Skills[0].ResolvedVersionID)

	managed, err := svc.GetManagedAssistant(ctx, &gen.GetManagedAssistantPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Len(t, managed.Skills, 1)
	require.Equal(t, skill.ID.String(), managed.Skills[0].SkillID)
	require.Equal(t, version.ID.String(), managed.Skills[0].ResolvedVersionID)

	// It is the project's single managed assistant.
	all, err := svc.core.ListAssistants(ctx, projectID)
	require.NoError(t, err)
	require.Len(t, all, 1)
}

func TestEnsureManagedAssistantRequiresProjectGrant(t *testing.T) {
	t.Parallel()

	svc, ctx, _ := newRBACService(t)
	ctx = authztest.WithExactGrants(t, ctx) // no grants

	_, err := svc.EnsureManagedAssistant(ctx, &gen.EnsureManagedAssistantPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}
