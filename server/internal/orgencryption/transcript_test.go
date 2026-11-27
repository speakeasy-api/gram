package orgencryption_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/chat/transcriptcrypto"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestChatTranscripts_TenantIsolationAndKeyLifecycle(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	ctx := t.Context()
	actor := orgencryption.SystemActor()
	codec, err := transcriptcrypto.New(ti.fixture.Service, ti.orgID)
	require.NoError(t, err)
	identity := transcriptcrypto.Identity{ProjectID: uuid.New(), ChatID: uuid.New(), MessageID: uuid.New()}
	notes := "EXAMPLE tool outcome"
	message := transcriptcrypto.ProtectedFields{
		Content:           "EXAMPLE transcript message",
		ContentRaw:        json.RawMessage(`{"type":"text","text":"EXAMPLE rich content"}`),
		ToolCalls:         json.RawMessage(`[{"id":"example-call","function":{"name":"example","arguments":"{}"}}]`),
		ToolOutcomeNotes:  &notes,
		ToolCallSummaries: json.RawMessage(`{"example-call":"EXAMPLE summary"}`),
	}

	// No platform-key or plaintext fallback exists before provisioning.
	ciphertext, err := codec.Encrypt(ctx, identity, message)
	require.ErrorIs(t, err, orgencryption.ErrNotProvisioned)
	require.Empty(t, ciphertext)

	require.NoError(t, ti.fixture.Provisioner.EnsureUseCase(ctx, ti.orgID, orgencryption.UseCaseChatTranscripts, actor))
	ciphertext, err = codec.Encrypt(ctx, identity, message)
	require.NoError(t, err)
	require.NotContains(t, ciphertext, message.Content)
	opened, err := codec.Decrypt(ctx, identity, ciphertext)
	require.NoError(t, err)
	require.Equal(t, message, opened)

	otherOrgID := createOrganization(t, ti.conn)
	require.NoError(t, ti.fixture.Provisioner.EnsureUseCase(ctx, otherOrgID, orgencryption.UseCaseChatTranscripts, actor))
	otherCodec, err := transcriptcrypto.New(ti.fixture.Service, otherOrgID)
	require.NoError(t, err)
	opened, err = otherCodec.Decrypt(ctx, identity, ciphertext)
	require.ErrorIs(t, err, orgencryption.ErrInvalidCiphertext)
	require.Equal(t, transcriptcrypto.ProtectedFields{}, opened)

	// A different use case and a different storage column cannot open it.
	ti.provision(t)
	_, err = ti.encryptor(t).Decrypt(ctx, "chat_message_transcripts.ciphertext", ciphertext)
	require.ErrorIs(t, err, orgencryption.ErrInvalidCiphertext)
	transcriptKey, err := ti.fixture.Service.For(ti.orgID, orgencryption.UseCaseChatTranscripts)
	require.NoError(t, err)
	_, err = transcriptKey.Decrypt(ctx, "example_table.value_encrypted", ciphertext)
	require.ErrorIs(t, err, orgencryption.ErrInvalidCiphertext)

	require.NoError(t, ti.fixture.Provisioner.RotateDataKey(ctx, ti.orgID, orgencryption.UseCaseChatTranscripts, actor))
	rotated, err := codec.Encrypt(ctx, identity, message)
	require.NoError(t, err)
	before, err := encryption.ParseEnvelope(ciphertext)
	require.NoError(t, err)
	after, err := encryption.ParseEnvelope(rotated)
	require.NoError(t, err)
	require.NotEqual(t, before.DataKeyID, after.DataKeyID)
	opened, err = codec.Decrypt(ctx, identity, ciphertext)
	require.NoError(t, err)
	require.Equal(t, message, opened, "rotation retains decrypt-only keys")

	require.NoError(t, ti.fixture.Provisioner.Suspend(ctx, ti.orgID, "EXAMPLE suspended key", actor))
	failed, err := codec.Encrypt(ctx, identity, message)
	require.ErrorIs(t, err, orgencryption.ErrSuspended)
	require.Empty(t, failed)
	opened, err = codec.Decrypt(ctx, identity, ciphertext)
	require.ErrorIs(t, err, orgencryption.ErrSuspended)
	require.Equal(t, transcriptcrypto.ProtectedFields{}, opened)
}

func TestChatTranscripts_StorageMigrationUnavailable(t *testing.T) {
	t.Parallel()

	parsed, err := orgencryption.ParseUseCase("chat-transcripts")
	require.NoError(t, err)
	require.Equal(t, orgencryption.UseCaseChatTranscripts, parsed)

	// A zero-value manager proves refusals happen before touching storage,
	// provisioning keys, or publishing audit events.
	var transitions orgencryption.Transitions
	_, err = transitions.Get(t.Context(), "org_example", parsed)
	require.ErrorIs(t, err, orgencryption.ErrTransitionNotAllowed)
	_, err = transitions.GetFresh(t.Context(), "org_example", parsed)
	require.ErrorIs(t, err, orgencryption.ErrTransitionNotAllowed)
	_, err = transitions.Enroll(t.Context(), "org_example", parsed, orgencryption.SystemActor())
	require.ErrorIs(t, err, orgencryption.ErrTransitionNotAllowed)
	_, err = transitions.StartInOrgKMS(t.Context(), "org_example", parsed, orgencryption.SystemActor())
	require.ErrorIs(t, err, orgencryption.ErrTransitionNotAllowed)

	ti := newTestInstance(t)
	manager := orgencryption.NewTransitions(testenv.NewLogger(t), ti.conn, nil, nil, orgencryption.TransitionsConfig{})
	listed, err := manager.List(t.Context(), ti.orgID)
	require.NoError(t, err)
	for _, transition := range listed {
		require.NotEqual(t, parsed, transition.UseCase, "plaintext transcripts must not be reported as platform-key encrypted")
	}
}
