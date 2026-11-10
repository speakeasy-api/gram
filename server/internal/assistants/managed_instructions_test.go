package assistants

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
)

// previousShippedInstructions is a frozen copy of an earlier shipped version of
// managed_assistant_instructions.txt, as an unedited older assistant stores it.
//
//go:embed testdata/managed_assistant_instructions_05c5372.txt
var previousShippedInstructions string

func TestShippedManagedAssistantInstructionsIncludesCurrent(t *testing.T) {
	t.Parallel()

	sum := sha256.Sum256([]byte(managedAssistantInstructions))
	require.Contains(t, shippedManagedAssistantInstructions, hex.EncodeToString(sum[:]),
		"managed_assistant_instructions.txt changed: add its new hash to shippedManagedAssistantInstructions")
}

func TestCurrentAssistantInstructionsReplacesShippedVersion(t *testing.T) {
	t.Parallel()

	require.Equal(t, managedAssistantInstructions, currentAssistantInstructions(previousShippedInstructions))
	require.Equal(t, managedAssistantInstructions, currentAssistantInstructions(managedAssistantInstructions))
}

func TestCurrentAssistantInstructionsKeepsEditedPrompt(t *testing.T) {
	t.Parallel()

	for _, stored := range []string{"", "You are a custom assistant.", previousShippedInstructions + "\nAlways answer in French."} {
		require.Equal(t, stored, currentAssistantInstructions(stored))
	}
}

func TestBuildThreadBootstrapRefreshesOnlyManagedAssistantPrompt(t *testing.T) {
	t.Parallel()

	svc, ctx, projectID, conn := newRBACServiceWithConn(t, "assistant_bootstrap_managed_prompt")
	old := previousShippedInstructions

	managed, err := svc.core.EnableManagedAssistant(ctx, "org-test", projectID, "user-test", false)
	require.NoError(t, err)
	_, err = svc.core.UpdateAssistant(ctx, projectID, managed.ID, nil, nil, &old, nil, nil, nil, nil, nil, nil, nil)
	require.NoError(t, err)
	custom, err := svc.core.CreateAssistant(ctx, "org-test", projectID, "user-test", "Custom assistant", "test-model", old, nil, nil, 60, 1, StatusActive, false, nil, nil)
	require.NoError(t, err)

	bootstrap := func(assistantID uuid.UUID, correlationID string) string {
		chatID := uuid.New()
		require.NoError(t, assistantrepo.New(conn).UpsertAssistantChat(ctx, assistantrepo.UpsertAssistantChatParams{
			ChatID: chatID, ProjectID: projectID, OrganizationID: "org-test", Title: pgtype.Text{},
		}))
		threadID, err := assistantrepo.New(conn).UpsertAssistantThread(ctx, assistantrepo.UpsertAssistantThreadParams{
			AssistantID: assistantID, ProjectID: projectID, CorrelationID: correlationID, ChatID: chatID,
			SourceKind: sourceKindSlack, SourceRefJson: []byte(`{}`),
		})
		require.NoError(t, err)
		got, err := svc.core.BuildThreadBootstrap(ctx, projectID, threadID, assistantID)
		require.NoError(t, err)
		return got.Instructions
	}

	require.Contains(t, bootstrap(managed.ID, "managed-prompt"), managedAssistantInstructions)
	require.Contains(t, bootstrap(custom.ID, "custom-prompt"), old)
}
