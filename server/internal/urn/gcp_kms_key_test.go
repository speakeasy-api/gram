package urn_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestGcpKmsKeyRoundTrip(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	original := urn.NewGcpKmsKey(id)

	require.Equal(t, "gcp_kms_key:44444444-4444-4444-4444-444444444444", original.String())

	parsed, err := urn.ParseGcpKmsKey(original.String())
	require.NoError(t, err)
	require.Equal(t, original.ID, parsed.ID)

	data, err := json.Marshal(original)
	require.NoError(t, err)
	require.Equal(t, `"gcp_kms_key:44444444-4444-4444-4444-444444444444"`, string(data))

	var fromJSON urn.GcpKmsKey
	err = json.Unmarshal(data, &fromJSON)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromJSON.ID)

	text, err := original.MarshalText()
	require.NoError(t, err)

	var fromText urn.GcpKmsKey
	err = fromText.UnmarshalText(text)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromText.ID)

	value, err := original.Value()
	require.NoError(t, err)

	var fromDB urn.GcpKmsKey
	err = fromDB.Scan(value)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromDB.ID)
}

func TestGcpKmsKeyRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	_, err := urn.ParseGcpKmsKey("")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseGcpKmsKey("aws_kms_key:44444444-4444-4444-4444-444444444444")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseGcpKmsKey("gcp_kms_key:not-a-uuid")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.NewGcpKmsKey(uuid.Nil).MarshalJSON()
	require.ErrorIs(t, err, urn.ErrInvalid)
}
