package urn_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestAwsIamCredentialRoundTrip(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	original := urn.NewAwsIamCredential(id)

	require.Equal(t, "aws_iam_credential:44444444-4444-4444-4444-444444444444", original.String())

	parsed, err := urn.ParseAwsIamCredential(original.String())
	require.NoError(t, err)
	require.Equal(t, original.ID, parsed.ID)

	data, err := json.Marshal(original)
	require.NoError(t, err)
	require.Equal(t, `"aws_iam_credential:44444444-4444-4444-4444-444444444444"`, string(data))

	var fromJSON urn.AwsIamCredential
	err = json.Unmarshal(data, &fromJSON)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromJSON.ID)

	text, err := original.MarshalText()
	require.NoError(t, err)

	var fromText urn.AwsIamCredential
	err = fromText.UnmarshalText(text)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromText.ID)

	value, err := original.Value()
	require.NoError(t, err)

	var fromDB urn.AwsIamCredential
	err = fromDB.Scan(value)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromDB.ID)
}

func TestAwsIamCredentialRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	_, err := urn.ParseAwsIamCredential("")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseAwsIamCredential("gcp_iam_credential:44444444-4444-4444-4444-444444444444")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseAwsIamCredential("aws_iam_credential:not-a-uuid")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.NewAwsIamCredential(uuid.Nil).MarshalJSON()
	require.ErrorIs(t, err, urn.ErrInvalid)
}
