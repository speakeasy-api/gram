package urn_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestGcpIamCredentialRoundTrip(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("55555555-5555-5555-5555-555555555555")
	original := urn.NewGcpIamCredential(id)

	require.Equal(t, "gcp_iam_credential:55555555-5555-5555-5555-555555555555", original.String())

	parsed, err := urn.ParseGcpIamCredential(original.String())
	require.NoError(t, err)
	require.Equal(t, original.ID, parsed.ID)

	data, err := json.Marshal(original)
	require.NoError(t, err)
	require.Equal(t, `"gcp_iam_credential:55555555-5555-5555-5555-555555555555"`, string(data))

	var fromJSON urn.GcpIamCredential
	err = json.Unmarshal(data, &fromJSON)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromJSON.ID)

	text, err := original.MarshalText()
	require.NoError(t, err)

	var fromText urn.GcpIamCredential
	err = fromText.UnmarshalText(text)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromText.ID)

	value, err := original.Value()
	require.NoError(t, err)

	var fromDB urn.GcpIamCredential
	err = fromDB.Scan(value)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromDB.ID)
}

func TestGcpIamCredentialRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	_, err := urn.ParseGcpIamCredential("")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseGcpIamCredential("aws_iam_credential:55555555-5555-5555-5555-555555555555")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseGcpIamCredential("gcp_iam_credential:not-a-uuid")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.NewGcpIamCredential(uuid.Nil).MarshalJSON()
	require.ErrorIs(t, err, urn.ErrInvalid)
}
