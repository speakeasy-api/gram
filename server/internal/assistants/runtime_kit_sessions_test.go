package assistants

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/stretchr/testify/require"
)

func kitSessionFixture(t *testing.T) (*ServiceCore, *pgxpool.Pool, kitRuntimeSessionKey, uuid.UUID) {
	t.Helper()
	conn, err := assistantsInfra.CloneTestDatabase(t, "kit_session_binding")
	require.NoError(t, err)
	projectID, assistantID, chatID, threadID := insertAssistantFixture(t, conn)
	queries := assistantrepo.New(conn)
	require.NoError(t, queries.SetAssistantDesiredRuntimeVersionForTest(t.Context(), assistantrepo.SetAssistantDesiredRuntimeVersionForTestParams{ProjectID: projectID, AssistantID: assistantID, RuntimeVersion: 3}))
	require.NoError(t, queries.ReserveAssistantRuntimeV2(t.Context(), assistantrepo.ReserveAssistantRuntimeV2Params{ProjectID: projectID, AssistantID: assistantID, AssistantThreadID: threadID, Backend: runtimeBackendLocal, State: runtimeStateActive}))
	runtime, err := queries.GetAssistantRuntimeV2(t.Context(), assistantrepo.GetAssistantRuntimeV2Params{ProjectID: projectID, AssistantID: assistantID})
	require.NoError(t, err)
	cipher, err := encryption.NewWithBytes(make([]byte, 32))
	require.NoError(t, err)
	return &ServiceCore{db: conn, encryptionClient: cipher}, conn, kitRuntimeSessionKey{ProjectID: projectID, AssistantID: assistantID, RuntimeID: runtime.ID, ThreadID: threadID}, chatID
}

func TestKitSessionPersistenceAndCanonicalChat(t *testing.T) {
	t.Parallel()
	core, conn, key, chatID := kitSessionFixture(t)
	missing, found, err := core.LoadKitRuntimeSession(t.Context(), key)
	require.NoError(t, err)
	require.False(t, found)
	require.Equal(t, chatID, missing.ChatID)
	require.False(t, missing.HasPriorTranscript)
	// Store only existence evidence; old content is never part of the returned binding.
	_, err = testrepo.New(conn).InsertChatMessage(t.Context(), testrepo.InsertChatMessageParams{ChatID: chatID, ProjectID: uuid.NullUUID{UUID: key.ProjectID, Valid: true}, Role: "assistant", Content: "Prior transcript must not be replayed"})
	require.NoError(t, err)
	stored, err := core.StoreKitRuntimeSession(t.Context(), key, "opaque/session:first")
	require.NoError(t, err)
	require.Equal(t, chatID, stored.ChatID)
	require.True(t, stored.HasPriorTranscript)
	require.Equal(t, "opaque/session:first", stored.SessionID)
	retry, err := core.StoreKitRuntimeSession(t.Context(), key, "empty-orphan-from-retry")
	require.NoError(t, err)
	require.Equal(t, stored, retry)
	restarted := &ServiceCore{db: conn, encryptionClient: core.encryptionClient}
	loaded, found, err := restarted.LoadKitRuntimeSession(t.Context(), key)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, stored, loaded)
}

func TestKitSessionConcurrentCreationConvergesBeforePrompt(t *testing.T) {
	t.Parallel()
	core, _, key, _ := kitSessionFixture(t)
	var wg sync.WaitGroup
	sessions := make([]kitRuntimeSession, 2)
	errs := make([]error, 2)
	for i, remoteSession := range []string{"remote-first", "remote-second"} {
		wg.Go(func() { sessions[i], errs[i] = core.StoreKitRuntimeSession(t.Context(), key, remoteSession) })
	}
	wg.Wait()
	require.NoError(t, errs[0])
	require.NoError(t, errs[1])
	require.Equal(t, sessions[0], sessions[1])
	require.NotEmpty(t, sessions[0].SessionID)
}

func TestKitSessionRejectsMismatchedAndRetiredAuthority(t *testing.T) {
	t.Parallel()
	core, conn, key, _ := kitSessionFixture(t)
	_, err := core.StoreKitRuntimeSession(t.Context(), key, "session-bound")
	require.NoError(t, err)
	for _, bad := range []kitRuntimeSessionKey{
		{ProjectID: uuid.New(), AssistantID: key.AssistantID, RuntimeID: key.RuntimeID, ThreadID: key.ThreadID},
		{ProjectID: key.ProjectID, AssistantID: uuid.New(), RuntimeID: key.RuntimeID, ThreadID: key.ThreadID},
		{ProjectID: key.ProjectID, AssistantID: key.AssistantID, RuntimeID: uuid.New(), ThreadID: key.ThreadID},
		{ProjectID: key.ProjectID, AssistantID: key.AssistantID, RuntimeID: key.RuntimeID, ThreadID: uuid.New()},
	} {
		_, _, err := core.LoadKitRuntimeSession(t.Context(), bad)
		require.ErrorIs(t, err, ErrKitRuntimeBinding)
		_, err = core.StoreKitRuntimeSession(t.Context(), bad, "must-not-bind")
		require.ErrorIs(t, err, ErrKitRuntimeBinding)
	}
	queries := assistantrepo.New(conn)
	require.NoError(t, queries.SetAssistantDesiredRuntimeVersionForTest(t.Context(), assistantrepo.SetAssistantDesiredRuntimeVersionForTestParams{ProjectID: key.ProjectID, AssistantID: key.AssistantID, RuntimeVersion: 2}))
	_, _, err = core.LoadKitRuntimeSession(t.Context(), key)
	require.ErrorIs(t, err, ErrKitRuntimeBinding)
	require.NoError(t, queries.SetAssistantDesiredRuntimeVersionForTest(t.Context(), assistantrepo.SetAssistantDesiredRuntimeVersionForTestParams{ProjectID: key.ProjectID, AssistantID: key.AssistantID, RuntimeVersion: 3}))
	require.NoError(t, queries.RetireAssistantRuntimesForSwitch(t.Context(), assistantrepo.RetireAssistantRuntimesForSwitchParams{ProjectID: key.ProjectID, AssistantID: key.AssistantID}))
	_, _, err = core.LoadKitRuntimeSession(t.Context(), key)
	require.ErrorIs(t, err, ErrKitRuntimeBinding)
	_, err = core.StoreKitRuntimeSession(t.Context(), key, "late-response")
	require.ErrorIs(t, err, ErrKitRuntimeBinding)
	require.NoError(t, queries.ReserveAssistantRuntimeV2(t.Context(), assistantrepo.ReserveAssistantRuntimeV2Params{ProjectID: key.ProjectID, AssistantID: key.AssistantID, AssistantThreadID: key.ThreadID, Backend: runtimeBackendLocal, State: runtimeStateActive}))
	replacement, err := queries.GetAssistantRuntimeV2(t.Context(), assistantrepo.GetAssistantRuntimeV2Params{ProjectID: key.ProjectID, AssistantID: key.AssistantID})
	require.NoError(t, err)
	require.NotEqual(t, key.RuntimeID, replacement.ID)
	nextKey := key
	nextKey.RuntimeID = replacement.ID
	next, found, err := core.LoadKitRuntimeSession(t.Context(), nextKey)
	require.NoError(t, err)
	require.False(t, found)
	require.Empty(t, next.SessionID)
}

func TestKitSessionCallbackRequiresExactSessionAndHoldsAuthorityFence(t *testing.T) {
	t.Parallel()
	core, conn, key, chatID := kitSessionFixture(t)
	_, err := core.StoreKitRuntimeSession(t.Context(), key, "exact-session")
	require.NoError(t, err)
	tx, err := conn.Begin(t.Context())
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(t.Context()) }()
	_, err = core.ValidateKitRuntimeSessionInTx(t.Context(), tx, key, "another-session")
	require.ErrorIs(t, err, ErrKitRuntimeBinding)
	binding, err := core.ValidateKitRuntimeSessionInTx(t.Context(), tx, key, "exact-session")
	require.NoError(t, err)
	require.Equal(t, chatID, binding.ChatID)
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	contender, err := conn.Begin(t.Context())
	require.NoError(t, err)
	defer func() { _ = contender.Rollback(t.Context()) }()
	err = assistantrepo.New(contender).AcquireAssistantAdvisoryLock(ctx, key.AssistantID.String())
	require.Error(t, err)
	require.ErrorIs(t, ctx.Err(), context.DeadlineExceeded)
}

func TestKitSessionCannotAliasAnotherThread(t *testing.T) {
	t.Parallel()
	core, conn, key, chatID := kitSessionFixture(t)
	_, err := core.StoreKitRuntimeSession(t.Context(), key, "one-session")
	require.NoError(t, err)
	threadID, err := assistantrepo.New(conn).UpsertAssistantThread(t.Context(), assistantrepo.UpsertAssistantThreadParams{ProjectID: key.ProjectID, AssistantID: key.AssistantID, ChatID: chatID, CorrelationID: "different-thread", SourceKind: sourceKindSlack, SourceRefJson: []byte(`{}`)})
	require.NoError(t, err)
	other := key
	other.ThreadID = threadID
	_, err = core.StoreKitRuntimeSession(t.Context(), other, "one-session")
	require.ErrorIs(t, err, ErrKitRuntimeSessionConflict)
	require.NoError(t, assistantrepo.New(conn).SoftDeleteAssistantThread(t.Context(), assistantrepo.SoftDeleteAssistantThreadParams{ID: key.ThreadID, ProjectID: key.ProjectID}))
	_, _, err = core.LoadKitRuntimeSession(t.Context(), key)
	require.ErrorIs(t, err, ErrKitRuntimeBinding)
}

func TestKitGatewayTokenEncryptedRestartRotationAndRetirement(t *testing.T) {
	t.Parallel()
	core, conn, key, _ := kitSessionFixture(t)
	const token = "synthetic-gateway-test-token"
	_, err := core.LoadKitGatewayToken(t.Context(), key.ProjectID, key.AssistantID, key.RuntimeID)
	require.ErrorIs(t, err, ErrKitGatewayTokenNotFound)
	require.NoError(t, core.StoreKitGatewayToken(t.Context(), key.ProjectID, key.AssistantID, key.RuntimeID, token))
	queries := assistantrepo.New(conn)
	ciphertext, err := queries.LoadKitGatewayCredential(t.Context(), assistantrepo.LoadKitGatewayCredentialParams{ProjectID: key.ProjectID, AssistantID: key.AssistantID, RuntimeID: key.RuntimeID})
	require.NoError(t, err)
	require.NotContains(t, ciphertext, token)
	require.NoError(t, queries.SetAssistantRuntimeStateForTest(t.Context(), assistantrepo.SetAssistantRuntimeStateForTestParams{ProjectID: key.ProjectID, RuntimeID: key.RuntimeID, State: runtimeStateStopped, UpdatedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true}}))
	restarted := &ServiceCore{db: conn, encryptionClient: core.encryptionClient}
	loaded, err := restarted.LoadKitGatewayToken(t.Context(), key.ProjectID, key.AssistantID, key.RuntimeID)
	require.NoError(t, err)
	require.Equal(t, token, loaded)
	require.NoError(t, restarted.StoreKitGatewayToken(t.Context(), key.ProjectID, key.AssistantID, key.RuntimeID, "rotated-test-token"))
	loaded, err = core.LoadKitGatewayToken(t.Context(), key.ProjectID, key.AssistantID, key.RuntimeID)
	require.NoError(t, err)
	require.Equal(t, "rotated-test-token", loaded)
	_, err = core.LoadKitGatewayToken(t.Context(), uuid.New(), key.AssistantID, key.RuntimeID)
	require.ErrorIs(t, err, ErrKitRuntimeBinding)
	require.NoError(t, queries.RetireAssistantRuntimesForSwitch(t.Context(), assistantrepo.RetireAssistantRuntimesForSwitchParams{ProjectID: key.ProjectID, AssistantID: key.AssistantID}))
	loaded, err = core.LoadKitGatewayToken(t.Context(), key.ProjectID, key.AssistantID, key.RuntimeID)
	require.ErrorIs(t, err, ErrKitRuntimeBinding)
	require.Empty(t, loaded)
	require.ErrorIs(t, core.StoreKitGatewayToken(t.Context(), key.ProjectID, key.AssistantID, key.RuntimeID, "late-token"), ErrKitRuntimeBinding)
}

func TestKitGatewayCredentialRejectsCiphertextFromAnotherAuthority(t *testing.T) {
	t.Parallel()
	core, conn, key, _ := kitSessionFixture(t)
	require.NoError(t, core.StoreKitGatewayToken(t.Context(), key.ProjectID, key.AssistantID, key.RuntimeID, "synthetic-token"))
	queries := assistantrepo.New(conn)
	encrypted, err := queries.LoadKitGatewayCredential(t.Context(), assistantrepo.LoadKitGatewayCredentialParams{ProjectID: key.ProjectID, AssistantID: key.AssistantID, RuntimeID: key.RuntimeID})
	require.NoError(t, err)
	require.NoError(t, queries.RetireAssistantRuntimesForSwitch(t.Context(), assistantrepo.RetireAssistantRuntimesForSwitchParams{ProjectID: key.ProjectID, AssistantID: key.AssistantID}))
	require.NoError(t, queries.ReserveAssistantRuntimeV2(t.Context(), assistantrepo.ReserveAssistantRuntimeV2Params{ProjectID: key.ProjectID, AssistantID: key.AssistantID, AssistantThreadID: key.ThreadID, Backend: runtimeBackendLocal, State: runtimeStateActive}))
	next, err := queries.GetAssistantRuntimeV2(t.Context(), assistantrepo.GetAssistantRuntimeV2Params{ProjectID: key.ProjectID, AssistantID: key.AssistantID})
	require.NoError(t, err)
	_, err = queries.StoreKitGatewayCredential(t.Context(), assistantrepo.StoreKitGatewayCredentialParams{ProjectID: key.ProjectID, AssistantID: key.AssistantID, RuntimeID: next.ID, TokenCiphertext: encrypted})
	require.NoError(t, err)
	token, err := core.LoadKitGatewayToken(t.Context(), key.ProjectID, key.AssistantID, next.ID)
	require.ErrorIs(t, err, errKitGatewayCredential)
	require.Empty(t, token)
}

func TestKitSessionRejectsLegacyRuntimeEvenWithDesiredKit(t *testing.T) {
	t.Parallel()
	core, conn, key, _ := kitSessionFixture(t)
	queries := assistantrepo.New(conn)
	require.NoError(t, queries.RetireAssistantRuntimesForSwitch(t.Context(), assistantrepo.RetireAssistantRuntimesForSwitchParams{ProjectID: key.ProjectID, AssistantID: key.AssistantID}))
	require.NoError(t, queries.SetAssistantDesiredRuntimeVersionForTest(t.Context(), assistantrepo.SetAssistantDesiredRuntimeVersionForTestParams{ProjectID: key.ProjectID, AssistantID: key.AssistantID, RuntimeVersion: 2}))
	require.NoError(t, queries.ReserveAssistantRuntimeV2(t.Context(), assistantrepo.ReserveAssistantRuntimeV2Params{ProjectID: key.ProjectID, AssistantID: key.AssistantID, AssistantThreadID: key.ThreadID, Backend: runtimeBackendLocal, State: runtimeStateActive}))
	legacy, err := queries.GetAssistantRuntimeV2(t.Context(), assistantrepo.GetAssistantRuntimeV2Params{ProjectID: key.ProjectID, AssistantID: key.AssistantID})
	require.NoError(t, err)
	require.Equal(t, int16(2), legacy.RuntimeVersion)
	require.NoError(t, queries.SetAssistantDesiredRuntimeVersionForTest(t.Context(), assistantrepo.SetAssistantDesiredRuntimeVersionForTestParams{ProjectID: key.ProjectID, AssistantID: key.AssistantID, RuntimeVersion: 3}))
	key.RuntimeID = legacy.ID
	_, _, err = core.LoadKitRuntimeSession(t.Context(), key)
	require.ErrorIs(t, err, ErrKitRuntimeBinding)
	_, err = core.StoreKitRuntimeSession(t.Context(), key, "must-not-bind-legacy")
	require.ErrorIs(t, err, ErrKitRuntimeBinding)
	require.ErrorIs(t, core.StoreKitGatewayToken(t.Context(), key.ProjectID, key.AssistantID, key.RuntimeID, "test-token"), ErrKitRuntimeBinding)
}

func TestKitSessionRejectsDeletedRuntime(t *testing.T) {
	t.Parallel()
	core, conn, key, _ := kitSessionFixture(t)
	_, err := core.StoreKitRuntimeSession(t.Context(), key, "session-to-retire")
	require.NoError(t, err)
	require.NoError(t, core.StoreKitGatewayToken(t.Context(), key.ProjectID, key.AssistantID, key.RuntimeID, "test-token"))
	require.NoError(t, assistantrepo.New(conn).MarkAssistantRuntimeReaped(t.Context(), assistantrepo.MarkAssistantRuntimeReapedParams{ProjectID: key.ProjectID, RuntimeID: key.RuntimeID, ReapedState: runtimeStateReaped}))
	_, _, err = core.LoadKitRuntimeSession(t.Context(), key)
	require.ErrorIs(t, err, ErrKitRuntimeBinding)
	token, err := core.LoadKitGatewayToken(t.Context(), key.ProjectID, key.AssistantID, key.RuntimeID)
	require.ErrorIs(t, err, ErrKitRuntimeBinding)
	require.Empty(t, token)
}

func TestKitGatewayConcurrentProvisioningUsesPersistedWinner(t *testing.T) {
	t.Parallel()
	core, _, key, _ := kitSessionFixture(t)
	tokens := make([]string, 2)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i, candidate := range []string{"candidate-one", "candidate-two"} {
		wg.Go(func() {
			tokens[i], errs[i] = core.EnsureKitGatewayToken(t.Context(), key.ProjectID, key.AssistantID, key.RuntimeID, candidate)
		})
	}
	wg.Wait()
	require.NoError(t, errs[0])
	require.NoError(t, errs[1])
	require.NotEmpty(t, tokens[0])
	require.Equal(t, tokens[0], tokens[1])
	loaded, err := core.LoadKitGatewayToken(t.Context(), key.ProjectID, key.AssistantID, key.RuntimeID)
	require.NoError(t, err)
	require.Equal(t, tokens[0], loaded)
}
