package assistanttokens

import (
	"context"
	"crypto/sha256"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	assistantsrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	tokenrepo "github.com/speakeasy-api/gram/server/internal/auth/assistanttokens/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/stretchr/testify/require"
)

func TestInferenceCredentialNamespace(t *testing.T) {
	t.Parallel()

	for _, token := range []string{"sai_example", "Bearer sai_example", "bearer sai_example"} {
		require.True(t, IsInferenceCredential(token))
		require.False(t, IsRuntimeCredential(token))
	}
	for _, token := range []string{"", "Bearer eyJhbGciOiJIUzI1NiJ9.e30.signature", "other_example"} {
		require.False(t, IsInferenceCredential(token))
	}
	require.False(t, InferenceTranscriptOwned(context.Background()))
	ctx := context.WithValue(context.Background(), inferenceContextKey{}, assistantidentity.Identity{})
	require.True(t, InferenceTranscriptOwned(ctx))
}

func TestInferenceCredentialLifetimeBounds(t *testing.T) {
	t.Parallel()

	m := new(Manager)
	for _, lifetime := range []time.Duration{0, -time.Second, MaxInferenceLifetime + time.Second} {
		_, err := m.IssueInferenceCredential(t.Context(), InferenceIssueInput{RuntimeID: uuid.New(), Lifetime: lifetime})
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
			Backend: "local", BackendMetadataJson: []byte(`{}`), State: "running",
			WarmUntil: now, LastHeartbeatAt: now, UpdatedAt: now, EndedAt: pgtype.Timestamptz{}, DeletedAt: pgtype.Timestamptz{},
		}))
	}
	createRuntime(runtimeID)
	q := tokenrepo.New(f.conn)
	issue := func(seed string) (uuid.UUID, []byte) {
		hash := sha256.Sum256([]byte(seed))
		id, err := q.IssueInferenceCredential(ctx, tokenrepo.IssueInferenceCredentialParams{ProjectID: f.projectID, AssistantID: f.assistantID, RuntimeID: runtimeID, TokenHash: hash[:], IdentityJson: []byte(`{}`), PolicyJson: []byte(`{}`), PolicyVersion: 1, ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), InfinityModifier: pgtype.Finite, Valid: true}})
		require.NoError(t, err)
		return id, hash[:]
	}
	id, hash := issue("test-only-credential-one")
	_, err := q.ReadInferenceCredential(ctx, hash)
	require.NoError(t, err)
	require.NoError(t, q.RevokeInferenceCredential(ctx, tokenrepo.RevokeInferenceCredentialParams{ProjectID: uuid.New(), ID: id}))
	_, err = q.ReadInferenceCredential(ctx, hash)
	require.NoError(t, err, "another project cannot revoke")
	require.NoError(t, q.RevokeInferenceCredential(ctx, tokenrepo.RevokeInferenceCredentialParams{ProjectID: f.projectID, ID: id}))
	_, err = q.ReadInferenceCredential(ctx, hash)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, hash = issue("test-only-credential-two")
	require.NoError(t, runtimes.RetireAssistantRuntimesForSwitch(ctx, assistantsrepo.RetireAssistantRuntimesForSwitchParams{ProjectID: f.projectID, AssistantID: f.assistantID}))
	_, err = q.ReadInferenceCredential(ctx, hash)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	// A fresh row for the same assistant must not resurrect the old generation.
	createRuntime(uuid.New())
	_, err = q.ReadInferenceCredential(ctx, hash)
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestInferenceChatOwnership(t *testing.T) {
	t.Parallel()

	f := newFixture(t, "inference_chat_ownership")
	m := New("", f.conn, nil, nil, nil)
	ctx := context.WithValue(t.Context(), inferenceContextKey{}, assistantidentity.Identity{ProjectID: f.projectID, AssistantID: f.assistantID})
	bound, err := m.BindInferenceChat(ctx, f.chatID)
	require.NoError(t, err)
	id, ok := RuntimeChatID(bound)
	require.True(t, ok)
	require.Equal(t, f.chatID, id)
	_, err = m.BindInferenceChat(ctx, uuid.New())
	require.Error(t, err)
	_, err = m.BindInferenceChat(ctx, uuid.Nil)
	require.Error(t, err)
	other := context.WithValue(t.Context(), inferenceContextKey{}, assistantidentity.Identity{ProjectID: f.projectID, AssistantID: uuid.New()})
	_, err = m.BindInferenceChat(other, f.chatID)
	require.Error(t, err)
	_, err = m.BindInferenceChat(t.Context(), f.chatID)
	require.Error(t, err)
}

func TestInferenceCredentialAttributionKind(t *testing.T) {
	t.Parallel()
	ctx := context.WithValue(t.Context(), inferenceContextKey{}, assistantidentity.Identity{})
	require.True(t, InferenceTranscriptOwned(ctx))
	require.False(t, IsKitAPIKey(ctx), "startup inference uses canonical chat attribution")
	kitCtx := context.WithValue(ctx, kitAPIKeyContextKey{}, true)
	require.True(t, IsKitAPIKey(kitCtx))
	require.True(t, InferenceTranscriptOwned(kitCtx))
}

func TestBindInferenceChatCanonicalAttribution(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "inference_canonical_attribution")
	m := &Manager{tokens: tokenrepo.New(f.conn)}
	ctx := context.WithValue(t.Context(), inferenceContextKey{}, assistantidentity.Identity{ProjectID: f.projectID, AssistantID: f.assistantID})
	bound, err := m.BindInferenceChat(ctx, f.chatID)
	require.NoError(t, err)
	chatID, ok := RuntimeChatID(bound)
	require.True(t, ok)
	require.Equal(t, f.chatID, chatID)
	principal, ok := contextvalues.GetAssistantPrincipal(bound)
	require.True(t, ok)
	require.Equal(t, f.threadID, principal.ThreadID)
	_, err = m.BindInferenceChat(ctx, uuid.New())
	require.Error(t, err)
}
