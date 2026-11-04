package urn_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestAccessGrantRoundTrip(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	original := urn.NewAccessGrant(id)

	require.Equal(t, "access_grant:33333333-3333-3333-3333-333333333333", original.String())

	parsed, err := urn.ParseAccessGrant(original.String())
	require.NoError(t, err)
	require.Equal(t, original.ID, parsed.ID)

	data, err := json.Marshal(original)
	require.NoError(t, err)
	require.Equal(t, `"access_grant:33333333-3333-3333-3333-333333333333"`, string(data))

	var fromJSON urn.AccessGrant
	err = json.Unmarshal(data, &fromJSON)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromJSON.ID)

	text, err := original.MarshalText()
	require.NoError(t, err)

	var fromText urn.AccessGrant
	err = fromText.UnmarshalText(text)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromText.ID)

	value, err := original.Value()
	require.NoError(t, err)

	var fromDB urn.AccessGrant
	err = fromDB.Scan(value)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromDB.ID)
}

func TestAccessGrantRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	_, err := urn.ParseAccessGrant("")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseAccessGrant("toolset:33333333-3333-3333-3333-333333333333")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseAccessGrant("access_grant:not-a-uuid")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseAccessGrant("access_grant:" + uuid.Nil.String())
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.NewAccessGrant(uuid.Nil).MarshalJSON()
	require.ErrorIs(t, err, urn.ErrInvalid)
}
