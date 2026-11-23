package transcriptcrypto

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/stretchr/testify/require"
)

type envelopeCryptor struct {
	key                    []byte
	id                     uuid.UUID
	encryptErr, decryptErr error
}

func newCryptor(t *testing.T) *envelopeCryptor {
	t.Helper()
	key := make([]byte, encryption.DataKeyBytes)
	_, err := rand.Read(key)
	require.NoError(t, err)
	return &envelopeCryptor{key: key, id: uuid.New()}
}

func (e *envelopeCryptor) Encrypt(_ context.Context, col string, plaintext []byte) (string, error) {
	if e.encryptErr != nil {
		return "partial-secret", e.encryptErr
	}
	sealed, err := encryption.SealEnvelope(e.key, e.id, plaintext, []byte(col))
	if err != nil {
		return "", fmt.Errorf("seal test envelope: %w", err)
	}
	return sealed.String(), nil
}
func (e *envelopeCryptor) Decrypt(_ context.Context, col, ciphertext string) ([]byte, error) {
	if e.decryptErr != nil {
		return []byte("partial-secret"), e.decryptErr
	}
	sealed, err := encryption.ParseEnvelope(ciphertext)
	if err != nil {
		return nil, fmt.Errorf("parse test envelope: %w", err)
	}
	plaintext, err := encryption.OpenEnvelope(e.key, sealed, []byte(col))
	if err != nil {
		return nil, fmt.Errorf("open test envelope: %w", err)
	}
	return plaintext, nil
}

var testProjectID = uuid.New()

func testIdentity(mid, cid uuid.UUID) Identity {
	return Identity{ProjectID: testProjectID, MessageID: mid, ChatID: cid}
}

func TestRoundTrip(t *testing.T) {
	t.Parallel()
	notes := "sensitive outcome"
	storageError := "sensitive provider error"
	empty := ""
	for _, message := range []Message{
		{StorageError: &storageError, Content: "sensitive content", ContentRaw: json.RawMessage(`[{"text":"private"}]`), ToolCalls: json.RawMessage(`[{"arguments":{"secret":"value"}}]`), ToolOutcomeNotes: &notes, ToolCallSummaries: json.RawMessage(`{"summary":"private"}`)},
		{},
		{StorageError: &empty, ContentRaw: json.RawMessage(`null`), ToolCalls: json.RawMessage(`[]`), ToolOutcomeNotes: &empty, ToolCallSummaries: json.RawMessage(`{}`)},
		{ContentRaw: json.RawMessage(`""`), ToolCalls: json.RawMessage(`null`), ToolCallSummaries: json.RawMessage(`[]`)},
	} {
		e := newCryptor(t)
		messageID, chatID := uuid.New(), uuid.New()
		ciphertext, err := encrypt(t.Context(), e, testIdentity(messageID, chatID), message)
		require.NoError(t, err)
		require.True(t, encryption.IsEnvelope(ciphertext))
		require.NotContains(t, ciphertext, "sensitive")
		actual, err := decrypt(t.Context(), e, testIdentity(messageID, chatID), ciphertext)
		require.NoError(t, err)
		require.Equal(t, message, actual, "preserve absent raw values separately from JSON null")
		expectedJSON, err := json.Marshal(message)
		require.NoError(t, err)
		actualJSON, err := json.Marshal(actual)
		require.NoError(t, err)
		require.JSONEq(t, string(expectedJSON), string(actualJSON))
		require.Equal(t, message.ToolOutcomeNotes, actual.ToolOutcomeNotes)
	}
}

func TestRejectsCiphertextAndIdentityMismatch(t *testing.T) {
	t.Parallel()
	e := newCryptor(t)
	mid, cid := uuid.New(), uuid.New()
	ciphertext, err := encrypt(t.Context(), e, testIdentity(mid, cid), Message{Content: "sensitive"})
	require.NoError(t, err)
	envelope, err := encryption.ParseEnvelope(ciphertext)
	require.NoError(t, err)
	envelope.Sealed[len(envelope.Sealed)-1] ^= 1
	wrongColumn, err := e.Encrypt(t.Context(), "other.column", []byte(`{}`))
	require.NoError(t, err)
	for _, tc := range []struct {
		name     string
		mid, cid uuid.UUID
		value    string
	}{
		{"message swap", uuid.New(), cid, ciphertext},
		{"chat swap", mid, uuid.New(), ciphertext},
		{"missing message", uuid.Nil, cid, ciphertext},
		{"missing chat", mid, uuid.Nil, ciphertext},
		{"tampered", mid, cid, envelope.String()},
		{"wrong column", mid, cid, wrongColumn},
		{"plaintext", mid, cid, `{"content":"sensitive"}`},
		{"empty", mid, cid, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			actual, err := decrypt(t.Context(), e, testIdentity(tc.mid, tc.cid), tc.value)
			require.Error(t, err)
			require.Equal(t, Message{}, actual)
			require.NotContains(t, err.Error(), "sensitive")
		})
	}
}

func TestRejectsInvalidPayload(t *testing.T) {
	t.Parallel()
	e := newCryptor(t)
	mid, cid := uuid.New(), uuid.New()
	valid := payload{Version: version, Identity: testIdentity(mid, cid), Message: &Message{Content: "sensitive"}}
	for _, tc := range []struct {
		name string
		edit func(*payload)
	}{
		{"missing project", func(p *payload) { p.ProjectID = uuid.Nil }},
		{"missing message", func(p *payload) { p.Message = nil }},
		{"unknown version", func(p *payload) { p.Version = 99 }},
		{"missing version", func(p *payload) { p.Version = 0 }},
		{"missing message identity", func(p *payload) { p.MessageID = uuid.Nil }},
		{"missing chat identity", func(p *payload) { p.ChatID = uuid.Nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := valid
			tc.edit(&p)
			data, err := json.Marshal(p)
			require.NoError(t, err)
			ciphertext, err := e.Encrypt(t.Context(), column, data)
			require.NoError(t, err)
			actual, err := decrypt(t.Context(), e, testIdentity(mid, cid), ciphertext)
			require.Error(t, err)
			require.Equal(t, Message{}, actual)
			require.NotContains(t, err.Error(), "sensitive")
		})
	}
	for _, data := range []string{`{"content":"sensitive",`, `null`, `{}`, `{"version":"sensitive"}`} {
		ciphertext, err := e.Encrypt(t.Context(), column, []byte(data))
		require.NoError(t, err)
		actual, err := decrypt(t.Context(), e, testIdentity(mid, cid), ciphertext)
		require.Error(t, err)
		require.Equal(t, Message{}, actual)
		require.NotContains(t, err.Error(), "sensitive")
	}
}

func TestEncryptFailure(t *testing.T) {
	t.Parallel()
	e := newCryptor(t)
	mid, cid := uuid.New(), uuid.New()
	for _, message := range []Message{
		{ContentRaw: json.RawMessage(`sensitive`)},
		{ToolCalls: json.RawMessage(`{"sensitive":`)},
		{ToolCallSummaries: json.RawMessage{}},
	} {
		ciphertext, err := encrypt(t.Context(), e, testIdentity(mid, cid), message)
		require.Error(t, err)
		require.Empty(t, ciphertext)
		require.NotContains(t, err.Error(), "sensitive")
	}
	for _, ids := range [][2]uuid.UUID{{uuid.Nil, cid}, {mid, uuid.Nil}} {
		ciphertext, err := encrypt(t.Context(), e, testIdentity(ids[0], ids[1]), Message{})
		require.Error(t, err)
		require.Empty(t, ciphertext)
	}
	e.encryptErr = errors.New("sensitive backend error")
	ciphertext, err := encrypt(t.Context(), e, testIdentity(mid, cid), Message{Content: "sensitive"})
	require.Error(t, err)
	require.Empty(t, ciphertext)
	require.NotContains(t, err.Error(), "sensitive")
	e.decryptErr = errors.New("sensitive backend error")
	actual, err := decrypt(t.Context(), e, testIdentity(mid, cid), "ciphertext")
	require.Error(t, err)
	require.Equal(t, Message{}, actual)
	require.NotContains(t, err.Error(), "sensitive")
}

func TestConstructorAndZeroValue(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		service *orgencryption.Service
		org     string
	}{
		{nil, "organization"}, {nil, ""}, {&orgencryption.Service{}, ""}, {&orgencryption.Service{}, " "}, {&orgencryption.Service{}, "bad\x00org"},
	} {
		codec, err := New(tc.service, tc.org)
		require.Error(t, err)
		require.Nil(t, codec)
	}
	codec, err := New(&orgencryption.Service{}, "organization")
	require.NoError(t, err) // Construction scopes only; it does not access storage.
	require.NotNil(t, codec.encryptor)
	for _, codec := range []*Codec{nil, {}} {
		ciphertext, err := codec.Encrypt(t.Context(), testIdentity(uuid.New(), uuid.New()), Message{})
		require.Error(t, err)
		require.Empty(t, ciphertext)
		message, err := codec.Decrypt(t.Context(), testIdentity(uuid.New(), uuid.New()), "plaintext")
		require.Error(t, err)
		require.Equal(t, Message{}, message)
	}
}

func TestPreservesErrorClassificationWithoutDetails(t *testing.T) {
	t.Parallel()
	for _, cause := range []error{orgencryption.ErrKeyUnavailable, orgencryption.ErrSuspended, orgencryption.ErrNotProvisioned, context.Canceled, context.DeadlineExceeded} {
		e := newCryptor(t)
		e.encryptErr = errors.Join(cause, errors.New("sensitive provider detail"))
		e.decryptErr = e.encryptErr
		mid, cid := uuid.New(), uuid.New()
		ciphertext, err := encrypt(t.Context(), e, testIdentity(mid, cid), Message{Content: "sensitive"})
		require.ErrorIs(t, err, cause)
		require.Empty(t, ciphertext)
		require.NotContains(t, err.Error(), "sensitive")
		message, err := decrypt(t.Context(), e, testIdentity(mid, cid), "ciphertext")
		require.ErrorIs(t, err, cause)
		require.Equal(t, Message{}, message)
		require.NotContains(t, err.Error(), "sensitive")
	}
}

func TestRejectsMissingOrNullMessage(t *testing.T) {
	t.Parallel()
	e := newCryptor(t)
	mid, cid := uuid.New(), uuid.New()
	for _, field := range []string{"", `,"message":null`} {
		data := `{"version":1,"project_id":"` + testProjectID.String() + `","message_id":"` + mid.String() + `","chat_id":"` + cid.String() + `"` + field + `}`
		ciphertext, err := e.Encrypt(t.Context(), column, []byte(data))
		require.NoError(t, err)
		message, err := decrypt(t.Context(), e, testIdentity(mid, cid), ciphertext)
		require.Error(t, err)
		require.Equal(t, Message{}, message)
	}
}

func TestProjectBinding(t *testing.T) {
	t.Parallel()
	e := newCryptor(t)
	identity := testIdentity(uuid.New(), uuid.New())
	ciphertext, err := encrypt(t.Context(), e, identity, Message{Content: "sensitive"})
	require.NoError(t, err)
	for _, projectID := range []uuid.UUID{uuid.Nil, uuid.New()} {
		other := identity
		other.ProjectID = projectID
		message, err := decrypt(t.Context(), e, other, ciphertext)
		require.Error(t, err)
		require.Equal(t, Message{}, message)
	}
	identity.ProjectID = uuid.Nil
	ciphertext, err = encrypt(t.Context(), e, identity, Message{Content: "sensitive"})
	require.Error(t, err)
	require.Empty(t, ciphertext)
}
