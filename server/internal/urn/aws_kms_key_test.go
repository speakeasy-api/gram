package urn_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestAwsKmsKeyRoundTrip(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	original := urn.NewAwsKmsKey(id)

	require.Equal(t, "aws_kms_key:44444444-4444-4444-4444-444444444444", original.String())

	parsed, err := urn.ParseAwsKmsKey(original.String())
	require.NoError(t, err)
	require.Equal(t, original.ID, parsed.ID)

	data, err := json.Marshal(original)
	require.NoError(t, err)
	require.Equal(t, `"aws_kms_key:44444444-4444-4444-4444-444444444444"`, string(data))

	var fromJSON urn.AwsKmsKey
	err = json.Unmarshal(data, &fromJSON)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromJSON.ID)

	text, err := original.MarshalText()
	require.NoError(t, err)

	var fromText urn.AwsKmsKey
	err = fromText.UnmarshalText(text)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromText.ID)

	value, err := original.Value()
	require.NoError(t, err)

	var fromDB urn.AwsKmsKey
	err = fromDB.Scan(value)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromDB.ID)
}

func TestAwsKmsKeyRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	_, err := urn.ParseAwsKmsKey("")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseAwsKmsKey("gcp_kms_key:44444444-4444-4444-4444-444444444444")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseAwsKmsKey("aws_kms_key:not-a-uuid")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.NewAwsKmsKey(uuid.Nil).MarshalJSON()
	require.ErrorIs(t, err, urn.ErrInvalid)
}
