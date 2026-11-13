package assistants

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/assistants"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestValidateReasoningEffort(t *testing.T) {
	t.Parallel()
	for _, effort := range []string{"none", "low", "medium", "high"} {
		require.NoError(t, validateReasoningEffort(effort))
	}
	for _, effort := range []string{"", "HIGH", " high", "minimal", "disabled"} {
		require.ErrorIs(t, validateReasoningEffort(effort), errAssistantValidation)
	}
	require.Equal(t, "medium", normalizeReasoningEffort(""))
}

func TestAssistantReasoningEffortPersistenceAndAudit(t *testing.T) {
	t.Parallel()
	svc, ctx, projectID, conn := newRBACServiceWithConn(t, "assistant_reasoning_effort")
	ctx = authztest.WithExactGrants(t, ctx, assistantWriteGrant(projectID))
	assistant := createTestAssistant(t, svc, ctx, "Reasoning")
	require.Equal(t, "medium", assistant.ReasoningEffort)
	for _, effort := range []string{"none", "low", "medium", "high"} {
		updated, err := svc.UpdateAssistant(ctx, &gen.UpdateAssistantPayload{ID: assistant.ID, ReasoningEffort: &effort})
		require.NoError(t, err)
		require.Equal(t, effort, updated.ReasoningEffort)
		require.Equal(t, assistant.Model, updated.Model)
	}
	log, err := audittest.LatestAuditLogByActionAndSubject(ctx, conn, audit.ActionAssistantReasoningUpdate, assistant.ID)
	require.NoError(t, err)
	require.JSONEq(t, `{"reasoning_effort":"medium"}`, string(log.BeforeSnapshot))
	require.JSONEq(t, `{"reasoning_effort":"high"}`, string(log.AfterSnapshot))
	require.NotContains(t, string(log.AfterSnapshot), "instructions")
	count, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionAssistantReasoningUpdate)
	require.NoError(t, err)
	require.EqualValues(t, 5, count)
	renamed, err := svc.UpdateAssistant(ctx, &gen.UpdateAssistantPayload{ID: assistant.ID, Name: new("Renamed")})
	require.NoError(t, err)
	require.Equal(t, "high", renamed.ReasoningEffort)
	_, err = svc.UpdateAssistant(ctx, &gen.UpdateAssistantPayload{ID: assistant.ID, ReasoningEffort: new("high")})
	require.NoError(t, err)
	after, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionAssistantReasoningUpdate)
	require.NoError(t, err)
	require.Equal(t, count, after, "omitted and no-op updates do not emit reasoning changes")
	for _, bad := range []string{"", "invalid"} {
		_, err = svc.UpdateAssistant(ctx, &gen.UpdateAssistantPayload{ID: assistant.ID, ReasoningEffort: &bad})
		requireOopsCode(t, err, oops.CodeBadRequest)
	}
	fetched, err := svc.GetAssistant(ctx, &gen.GetAssistantPayload{ID: assistant.ID})
	require.NoError(t, err)
	require.Equal(t, "high", fetched.ReasoningEffort)
	listed, err := svc.ListAssistants(ctx, &gen.ListAssistantsPayload{})
	require.NoError(t, err)
	require.Len(t, listed.Assistants, 1)
	require.Equal(t, "high", listed.Assistants[0].ReasoningEffort)
	explicit, err := svc.CreateAssistant(ctx, &gen.CreateAssistantPayload{Name: "No reasoning", Model: "test-model", ReasoningEffort: "none"})
	require.NoError(t, err)
	require.Equal(t, "none", explicit.ReasoningEffort)
	_, err = svc.CreateAssistant(ctx, &gen.CreateAssistantPayload{Name: "Invalid", Model: "test-model", ReasoningEffort: "invalid"})
	requireOopsCode(t, err, oops.CodeBadRequest)

	chatID := uuid.New()
	q := assistantrepo.New(conn)
	require.NoError(t, q.UpsertAssistantChat(ctx, assistantrepo.UpsertAssistantChatParams{ChatID: chatID, ProjectID: projectID, OrganizationID: "org-test", Title: pgtype.Text{}}))
	threadID, err := q.UpsertAssistantThread(ctx, assistantrepo.UpsertAssistantThreadParams{AssistantID: uuid.MustParse(assistant.ID), ProjectID: projectID, CorrelationID: "reasoning-bootstrap", ChatID: chatID, SourceKind: sourceKindSlack, SourceRefJson: []byte(`{}`)})
	require.NoError(t, err)
	bootstrap, err := svc.core.BuildThreadBootstrap(ctx, projectID, threadID, uuid.MustParse(assistant.ID))
	require.NoError(t, err)
	require.Equal(t, "high", bootstrap.ReasoningEffort)
}
