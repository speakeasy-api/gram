package urn_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestDataEncryptionKeyRoundTrip(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	original := urn.NewDataEncryptionKey(id)

	require.Equal(t, "data_encryption_key:44444444-4444-4444-4444-444444444444", original.String())

	parsed, err := urn.ParseDataEncryptionKey(original.String())
	require.NoError(t, err)
	require.Equal(t, original.ID, parsed.ID)

	data, err := json.Marshal(original)
	require.NoError(t, err)
	require.Equal(t, `"data_encryption_key:44444444-4444-4444-4444-444444444444"`, string(data))

	var fromJSON urn.DataEncryptionKey
	err = json.Unmarshal(data, &fromJSON)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromJSON.ID)

	text, err := original.MarshalText()
	require.NoError(t, err)

	var fromText urn.DataEncryptionKey
	err = fromText.UnmarshalText(text)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromText.ID)

	value, err := original.Value()
	require.NoError(t, err)

	var fromDB urn.DataEncryptionKey
	err = fromDB.Scan(value)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromDB.ID)
}

func TestDataEncryptionKeyRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	_, err := urn.ParseDataEncryptionKey("")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseDataEncryptionKey("aws_kms_key:44444444-4444-4444-4444-444444444444")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseDataEncryptionKey("data_encryption_key:not-a-uuid")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.NewDataEncryptionKey(uuid.Nil).MarshalJSON()
	require.ErrorIs(t, err, urn.ErrInvalid)
}

func TestParseDataEncryptionKey_RejectsNilID(t *testing.T) {
	t.Parallel()

	_, err := urn.ParseDataEncryptionKey("data_encryption_key:00000000-0000-0000-0000-000000000000")
	require.ErrorIs(t, err, urn.ErrInvalid)
}
