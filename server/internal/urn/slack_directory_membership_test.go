package urn_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestSlackDirectoryMembershipRoundTrip(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	original := urn.NewSlackDirectoryMembership(id)

	require.Equal(t, "slack_directory_membership:33333333-3333-3333-3333-333333333333", original.String())

	parsed, err := urn.ParseSlackDirectoryMembership(original.String())
	require.NoError(t, err)
	require.Equal(t, original.ID, parsed.ID)

	data, err := json.Marshal(original)
	require.NoError(t, err)
	require.Equal(t, `"slack_directory_membership:33333333-3333-3333-3333-333333333333"`, string(data))

	var fromJSON urn.SlackDirectoryMembership
	err = json.Unmarshal(data, &fromJSON)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromJSON.ID)

	text, err := original.MarshalText()
	require.NoError(t, err)

	var fromText urn.SlackDirectoryMembership
	err = fromText.UnmarshalText(text)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromText.ID)

	value, err := original.Value()
	require.NoError(t, err)

	var fromDB urn.SlackDirectoryMembership
	err = fromDB.Scan(value)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromDB.ID)
}

func TestSlackDirectoryMembershipRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	_, err := urn.ParseSlackDirectoryMembership("")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseSlackDirectoryMembership("toolset:33333333-3333-3333-3333-333333333333")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseSlackDirectoryMembership("slack_directory_membership:not-a-uuid")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.NewSlackDirectoryMembership(uuid.Nil).MarshalJSON()
	require.ErrorIs(t, err, urn.ErrInvalid)
}
