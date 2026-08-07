package urn_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestSkillShareLinkRoundTrip(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("88888888-8888-8888-8888-888888888888")
	original := urn.NewSkillShareLink(id)

	require.False(t, original.IsZero())
	require.Equal(t, "skill-share-link:88888888-8888-8888-8888-888888888888", original.String())

	parsed, err := urn.ParseSkillShareLink(original.String())
	require.NoError(t, err)
	require.Equal(t, original.ID, parsed.ID)

	data, err := json.Marshal(original)
	require.NoError(t, err)
	require.Equal(t, `"skill-share-link:88888888-8888-8888-8888-888888888888"`, string(data))

	var fromJSON urn.SkillShareLink
	err = json.Unmarshal(data, &fromJSON)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromJSON.ID)

	text, err := original.MarshalText()
	require.NoError(t, err)
	require.Equal(t, original.String(), string(text))

	var fromText urn.SkillShareLink
	err = fromText.UnmarshalText(text)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromText.ID)
}

func TestSkillShareLinkRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	_, err := urn.ParseSkillShareLink("")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseSkillShareLink("skill:88888888-8888-8888-8888-888888888888")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseSkillShareLink("skill-share-link:not-a-uuid")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseSkillShareLink("skill-share-link:88888888-8888-8888-8888-888888888888:extra")
	require.ErrorIs(t, err, urn.ErrInvalid)
}

func TestSkillShareLinkZeroValue(t *testing.T) {
	t.Parallel()

	var zero urn.SkillShareLink
	require.True(t, zero.IsZero())

	_, err := zero.MarshalJSON()
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = zero.MarshalText()
	require.ErrorIs(t, err, urn.ErrInvalid)

	value, err := zero.Value()
	require.ErrorIs(t, err, urn.ErrInvalid)
	require.Nil(t, value)

	err = zero.Scan(nil)
	require.NoError(t, err)
	require.True(t, zero.IsZero())
}

func TestSkillShareLinkDatabaseRoundTrip(t *testing.T) {
	t.Parallel()

	original := urn.NewSkillShareLink(uuid.MustParse("88888888-8888-8888-8888-888888888888"))
	value, err := original.Value()
	require.NoError(t, err)
	require.Equal(t, original.String(), value)

	var fromString urn.SkillShareLink
	err = fromString.Scan(value)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromString.ID)

	var fromBytes urn.SkillShareLink
	err = fromBytes.Scan([]byte(original.String()))
	require.NoError(t, err)
	require.Equal(t, original.ID, fromBytes.ID)

	var invalid urn.SkillShareLink
	err = invalid.Scan("skill-share-link:not-a-uuid")
	require.ErrorIs(t, err, urn.ErrInvalid)

	err = invalid.Scan(42)
	require.Error(t, err)
}
