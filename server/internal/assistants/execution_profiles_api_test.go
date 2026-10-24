package assistants

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/assistants"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestExecutionProfileChoiceRequiresWriteAndOneMode(t *testing.T) {
	t.Parallel()

	svc, ctx, projectID := newRBACService(t)
	writerCtx := authztest.WithExactGrants(t, ctx, assistantWriteGrant(projectID))
	assistant := createTestAssistant(t, svc, writerCtx, "Assistant")

	readerCtx := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeAssistantRead, assistant.ID))
	listed, err := svc.ListExecutionProfiles(readerCtx, &gen.ListExecutionProfilesPayload{AssistantID: assistant.ID})
	require.NoError(t, err)
	require.NotNil(t, listed.Profiles)

	choice := func(mutate func(*gen.SetExecutionProfileChoicePayload)) *gen.SetExecutionProfileChoicePayload {
		p := &gen.SetExecutionProfileChoicePayload{
			AssistantID: assistant.ID, TriggerInstanceID: uuid.NewString(), ExecutionContext: "userless",
			McpServerID: uuid.NewString(), Automatic: new(true),
		}
		mutate(p)
		return p
	}
	_, err = svc.SetExecutionProfileChoice(readerCtx, choice(func(*gen.SetExecutionProfileChoicePayload) {}))
	requireOopsCode(t, err, oops.CodeForbidden)

	_, err = svc.SetExecutionProfileChoice(writerCtx, choice(func(p *gen.SetExecutionProfileChoicePayload) { p.Automatic = nil }))
	requireOopsCode(t, err, oops.CodeBadRequest)
	_, err = svc.SetExecutionProfileChoice(writerCtx, choice(func(p *gen.SetExecutionProfileChoicePayload) { p.NoAuth = new(true) }))
	requireOopsCode(t, err, oops.CodeBadRequest)

	_, err = svc.SetExecutionProfileChoice(writerCtx, choice(func(*gen.SetExecutionProfileChoicePayload) {}))
	requireOopsCode(t, err, oops.CodeNotFound)
}
