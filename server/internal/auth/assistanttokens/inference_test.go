package assistanttokens

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	assistantsrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	tokenrepo "github.com/speakeasy-api/gram/server/internal/auth/assistanttokens/repo"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	"github.com/stretchr/testify/require"
)

func TestInferenceTranscriptOwnership(t *testing.T) {
	t.Parallel()

	require.False(t, InferenceTranscriptOwned(context.Background()))
	ctx := context.WithValue(context.Background(), inferenceContextKey{}, assistantidentity.Identity{})
	require.True(t, InferenceTranscriptOwned(ctx))
}

func TestInferenceCredentialLifetimeBounds(t *testing.T) {
	t.Parallel()

	m := new(Manager)
	for _, lifetime := range []time.Duration{0, -time.Second, MaxInferenceLifetime + time.Second} {
		_, err := m.IssueKitAPIKey(t.Context(), InferenceIssueInput{RuntimeID: uuid.New(), Lifetime: lifetime}, "test")
		require.Error(t, err)
	}
}

func TestInferenceCredentialStorageFencing(t *testing.T) {
	t.Parallel()

	f := newFixture(t, "inference_credential_fencing")
	ctx := t.Context()
	runtimeID := uuid.New()
	runtimes := assistantsrepo.New(f.conn)
	createRuntime := func(id uuid.UUID) {
		t.Helper()
		now := pgtype.Timestamptz{Time: time.Now(), InfinityModifier: pgtype.Finite, Valid: true}
		require.NoError(t, runtimes.CreateAssistantRuntime(ctx, assistantsrepo.CreateAssistantRuntimeParams{
			ID: id, AssistantThreadID: uuid.Nil, AssistantID: f.assistantID, ProjectID: f.projectID,
			Backend: "local", BackendMetadataJson: []byte(`{}`), State: "active",
			WarmUntil: now, LastHeartbeatAt: now, UpdatedAt: now, EndedAt: pgtype.Timestamptz{}, DeletedAt: pgtype.Timestamptz{},
		}))
		require.NoError(t, chatrepo.New(f.conn).SetKitCompletionRuntimeFixture(ctx, chatrepo.SetKitCompletionRuntimeFixtureParams{RuntimeID: id, ProjectID: f.projectID, RuntimeVersion: 3}))
	}
	require.NoError(t, runtimes.SetAssistantDesiredRuntimeVersionForTest(ctx, assistantsrepo.SetAssistantDesiredRuntimeVersionForTestParams{ProjectID: f.projectID, AssistantID: f.assistantID, RuntimeVersion: 3}))
	createRuntime(runtimeID)
	q := tokenrepo.New(f.conn)
	issue := func(seed string) uuid.UUID {
		t.Helper()
		key, err := keysrepo.New(f.conn).CreateAPIKey(ctx, keysrepo.CreateAPIKeyParams{OrganizationID: "org-test", ProjectID: uuid.NullUUID{UUID: f.projectID, Valid: true}, CreatedByUserID: "user-1", Name: seed, KeyPrefix: "test", KeyHash: seed, Scopes: []string{"consumer"}})
		require.NoError(t, err)
		keyID := key.ID
		_, err = q.IssueKitAPIKeyBinding(ctx, tokenrepo.IssueKitAPIKeyBindingParams{ApiKeyID: keyID, ProjectID: f.projectID, AssistantID: f.assistantID, RuntimeID: runtimeID, IdentityJson: []byte(`{}`)})
		require.NoError(t, err)
		return keyID
	}
	keyID := issue("test-only-credential-one")
	_, err := q.ReadKitAPIKeyBinding(ctx, keyID)
	require.NoError(t, err)
	require.NoError(t, q.RevokeInferenceCredential(ctx, tokenrepo.RevokeInferenceCredentialParams{ProjectID: uuid.New(), ApiKeyID: keyID}))
	_, err = q.ReadKitAPIKeyBinding(ctx, keyID)
	require.NoError(t, err, "another project cannot revoke")
	require.NoError(t, q.RevokeInferenceCredential(ctx, tokenrepo.RevokeInferenceCredentialParams{ProjectID: f.projectID, ApiKeyID: keyID}))
	_, err = q.ReadKitAPIKeyBinding(ctx, keyID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = keysrepo.New(f.conn).GetAPIKeyByKeyHash(ctx, "test-only-credential-one")
	require.ErrorIs(t, err, pgx.ErrNoRows, "binding revocation also revokes its native key")
	keyID = issue("test-only-credential-two")
	require.NoError(t, runtimes.RetireAssistantRuntimesForSwitch(ctx, assistantsrepo.RetireAssistantRuntimesForSwitchParams{ProjectID: f.projectID, AssistantID: f.assistantID}))
	_, err = q.ReadKitAPIKeyBinding(ctx, keyID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	// A fresh row for the same assistant must not resurrect the old generation.
	createRuntime(uuid.New())
	_, err = q.ReadKitAPIKeyBinding(ctx, keyID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
}
