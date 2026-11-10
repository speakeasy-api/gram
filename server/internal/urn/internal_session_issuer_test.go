package urn_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestInternalSessionIssuerRoundTrip(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	original := urn.NewInternalSessionIssuer(id)

	require.Equal(t, "internal_session_issuer:44444444-4444-4444-4444-444444444444", original.String())

	parsed, err := urn.ParseInternalSessionIssuer(original.String())
	require.NoError(t, err)
	require.Equal(t, original.ID, parsed.ID)

	data, err := json.Marshal(original)
	require.NoError(t, err)
	require.Equal(t, `"internal_session_issuer:44444444-4444-4444-4444-444444444444"`, string(data))

	var fromJSON urn.InternalSessionIssuer
	err = json.Unmarshal(data, &fromJSON)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromJSON.ID)

	text, err := original.MarshalText()
	require.NoError(t, err)

	var fromText urn.InternalSessionIssuer
	err = fromText.UnmarshalText(text)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromText.ID)

	value, err := original.Value()
	require.NoError(t, err)

	var fromDB urn.InternalSessionIssuer
	err = fromDB.Scan(value)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromDB.ID)
}

func TestInternalSessionIssuerRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	_, err := urn.ParseInternalSessionIssuer("")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseInternalSessionIssuer("toolset:44444444-4444-4444-4444-444444444444")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseInternalSessionIssuer("internal_session_issuer:not-a-uuid")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.NewInternalSessionIssuer(uuid.Nil).MarshalJSON()
	require.ErrorIs(t, err, urn.ErrInvalid)
}
