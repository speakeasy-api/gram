package assistants

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/assistants"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestEnsureManagedAssistantProvisionsAndIsIdempotent(t *testing.T) {
	t.Parallel()

	svc, ctx, projectID, _ := newRBACServiceWithConn(t, "assistants_ensure_managed")
	ctx = authztest.WithExactGrants(t, ctx, projectReadGrant(projectID))

	// First call provisions the built-in assistant out of nothing.
	first, err := svc.EnsureManagedAssistant(ctx, &gen.EnsureManagedAssistantPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.NotEmpty(t, first.ID)
	require.Contains(t, first.Name, "Project Assistant")

	// Second call returns the same assistant — safe to call on every sidebar open.
	second, err := svc.EnsureManagedAssistant(ctx, &gen.EnsureManagedAssistantPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, first.ID, second.ID)

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
