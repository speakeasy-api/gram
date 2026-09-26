package urn_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestSlackDirectoryConnectionRoundTrip(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	original := urn.NewSlackDirectoryConnection(id)

	require.Equal(t, "slack_directory_connection:33333333-3333-3333-3333-333333333333", original.String())

	parsed, err := urn.ParseSlackDirectoryConnection(original.String())
	require.NoError(t, err)
	require.Equal(t, original.ID, parsed.ID)

	data, err := json.Marshal(original)
	require.NoError(t, err)
	require.Equal(t, `"slack_directory_connection:33333333-3333-3333-3333-333333333333"`, string(data))

	var fromJSON urn.SlackDirectoryConnection
	err = json.Unmarshal(data, &fromJSON)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromJSON.ID)

	text, err := original.MarshalText()
	require.NoError(t, err)

	var fromText urn.SlackDirectoryConnection
	err = fromText.UnmarshalText(text)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromText.ID)

	value, err := original.Value()
	require.NoError(t, err)

	var fromDB urn.SlackDirectoryConnection
	err = fromDB.Scan(value)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromDB.ID)
}

func TestSlackDirectoryConnectionRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	_, err := urn.ParseSlackDirectoryConnection("")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseSlackDirectoryConnection("toolset:33333333-3333-3333-3333-333333333333")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseSlackDirectoryConnection("slack_directory_connection:not-a-uuid")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseSlackDirectoryConnection("slack_directory_connection:" + uuid.Nil.String())
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.NewSlackDirectoryConnection(uuid.Nil).MarshalJSON()
	require.ErrorIs(t, err, urn.ErrInvalid)
}
