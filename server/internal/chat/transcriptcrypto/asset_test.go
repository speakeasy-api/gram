package transcriptcrypto

import (
	"encoding/binary"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func testAssetIdentity(kind AssetKind, chatID uuid.UUID, body []byte) AssetIdentity {
	return AssetIdentity{Kind: kind, ProjectID: testProjectID, ChatID: chatID, SHA256: ContentHash(body)}
}

func TestAssetRoundTrip(t *testing.T) {
	t.Parallel()
	c := newCryptor(t)
	chatID := uuid.New()
	for _, tc := range []struct {
		name string
		kind AssetKind
		chat uuid.UUID
		body []byte
	}{
		{name: "message content", kind: AssetMessageContent, chat: chatID, body: []byte(`{"content":"a large invented body"}`)},
		{name: "content part", kind: AssetContentPart, chat: chatID, body: []byte("invented attachment text")},
		{name: "binary chat attachment", kind: AssetChatAttachment, chat: uuid.Nil, body: []byte{0x00, 0xff, 0x10, 0x80, 0x00}},
		{name: "empty body", kind: AssetContentPart, chat: chatID, body: []byte{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			id := testAssetIdentity(tc.kind, tc.chat, tc.body)
			sealed, err := encryptAsset(t.Context(), c, id, tc.body)
			require.NoError(t, err)
			if len(tc.body) > 4 {
				require.NotContains(t, string(sealed), string(tc.body))
			}
			opened, err := decryptAsset(t.Context(), c, id, sealed)
			require.NoError(t, err)
			require.Equal(t, tc.body, opened)
		})
	}
}

func TestAssetRejectsIdentityMismatch(t *testing.T) {
	t.Parallel()
	c := newCryptor(t)
	body := []byte("invented attachment text")
	id := testAssetIdentity(AssetContentPart, uuid.New(), body)
	sealed, err := encryptAsset(t.Context(), c, id, body)
	require.NoError(t, err)

	otherChat := id
	otherChat.ChatID = uuid.New()
	otherProject := id
	otherProject.ProjectID = uuid.New()
	otherKind := id
	otherKind.Kind = AssetMessageContent
	otherHash := id
	otherHash.SHA256 = ContentHash([]byte("other bytes"))
	for name, want := range map[string]AssetIdentity{"chat": otherChat, "project": otherProject, "kind": otherKind, "hash": otherHash} {
		opened, err := decryptAsset(t.Context(), c, want, sealed)
		require.ErrorIs(t, err, ErrAssetMismatch, name)
		require.Nil(t, opened, name)
	}
}

func TestAssetRejectsTamperingAndMessageCiphertext(t *testing.T) {
	t.Parallel()
	c := newCryptor(t)
	body := []byte("invented attachment text")
	id := testAssetIdentity(AssetContentPart, uuid.New(), body)
	sealed, err := encryptAsset(t.Context(), c, id, body)
	require.NoError(t, err)

	tampered := append([]byte(nil), sealed...)
	tampered[len(tampered)-10] ^= 0x01
	opened, err := decryptAsset(t.Context(), c, id, tampered)
	require.Error(t, err)
	require.Nil(t, opened)

	// A message-body ciphertext is bound to another storage location and
	// never opens as an asset.
	messageCiphertext, err := encrypt(t.Context(), c, testIdentity(uuid.New(), id.ChatID), Message{Content: string(body)})
	require.NoError(t, err)
	opened, err = decryptAsset(t.Context(), c, id, []byte(messageCiphertext))
	require.Error(t, err)
	require.Nil(t, opened)
}

func TestAssetRejectsBodyThatDoesNotMatchItsHash(t *testing.T) {
	t.Parallel()
	c := newCryptor(t)
	id := testAssetIdentity(AssetContentPart, uuid.New(), []byte("named bytes"))
	_, err := encryptAsset(t.Context(), c, id, []byte("other bytes"))
	require.ErrorIs(t, err, ErrAssetMismatch)

	// A sealed object whose header names the hash but whose body differs is
	// rejected on open too.
	header := `{"version":1,"kind":"content_part","project_id":"` + id.ProjectID.String() + `","chat_id":"` + id.ChatID.String() + `","sha256":"` + id.SHA256 + `"}`
	plaintext := binary.BigEndian.AppendUint32(nil, uint32(len(header)))
	plaintext = append(plaintext, header...)
	plaintext = append(plaintext, "other bytes"...)
	forged, err := c.Encrypt(t.Context(), assetColumn, plaintext)
	require.NoError(t, err)
	opened, err := decryptAsset(t.Context(), c, id, []byte(forged))
	require.ErrorIs(t, err, ErrAssetMismatch)
	require.Nil(t, opened)
}

func TestAssetIdentityValidation(t *testing.T) {
	t.Parallel()
	c := newCryptor(t)
	body := []byte("x")
	valid := testAssetIdentity(AssetContentPart, uuid.New(), body)
	for name, id := range map[string]AssetIdentity{
		"unknown kind":            {Kind: "other", ProjectID: valid.ProjectID, ChatID: valid.ChatID, SHA256: valid.SHA256},
		"missing project":         {Kind: valid.Kind, ProjectID: uuid.Nil, ChatID: valid.ChatID, SHA256: valid.SHA256},
		"missing chat":            {Kind: valid.Kind, ProjectID: valid.ProjectID, ChatID: uuid.Nil, SHA256: valid.SHA256},
		"attachment with a chat":  {Kind: AssetChatAttachment, ProjectID: valid.ProjectID, ChatID: valid.ChatID, SHA256: valid.SHA256},
		"uppercase hash":          {Kind: valid.Kind, ProjectID: valid.ProjectID, ChatID: valid.ChatID, SHA256: strings.ToUpper(valid.SHA256)},
		"short hash":              {Kind: valid.Kind, ProjectID: valid.ProjectID, ChatID: valid.ChatID, SHA256: valid.SHA256[:10]},
		"non hex hash characters": {Kind: valid.Kind, ProjectID: valid.ProjectID, ChatID: valid.ChatID, SHA256: strings.Repeat("g", 64)},
	} {
		_, err := encryptAsset(t.Context(), c, id, body)
		require.Error(t, err, name)
		_, err = decryptAsset(t.Context(), c, id, []byte("sealed"))
		require.Error(t, err, name)
	}
}

func TestAssetKeyFailureKeepsClassification(t *testing.T) {
	t.Parallel()
	c := newCryptor(t)
	body := []byte("invented attachment text")
	id := testAssetIdentity(AssetContentPart, uuid.New(), body)
	sealed, err := encryptAsset(t.Context(), c, id, body)
	require.NoError(t, err)

	keyErr := errors.New("provider detail")
	c.decryptErr = keyErr
	opened, err := decryptAsset(t.Context(), c, id, sealed)
	require.ErrorIs(t, err, keyErr)
	require.NotContains(t, err.Error(), "provider detail")
	require.Nil(t, opened)
}
