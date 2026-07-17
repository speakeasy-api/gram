package urn_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestSkillRoundTrip(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("77777777-7777-7777-7777-777777777777")
	original := urn.NewSkill(id)

	require.False(t, original.IsZero())
	require.Equal(t, "skill:77777777-7777-7777-7777-777777777777", original.String())

	parsed, err := urn.ParseSkill(original.String())
	require.NoError(t, err)
	require.Equal(t, original.ID, parsed.ID)

	data, err := json.Marshal(original)
	require.NoError(t, err)
	require.Equal(t, `"skill:77777777-7777-7777-7777-777777777777"`, string(data))

	var fromJSON urn.Skill
	err = json.Unmarshal(data, &fromJSON)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromJSON.ID)

	text, err := original.MarshalText()
	require.NoError(t, err)
	require.Equal(t, original.String(), string(text))

	var fromText urn.Skill
	err = fromText.UnmarshalText(text)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromText.ID)
}

func TestSkillRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	_, err := urn.ParseSkill("")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseSkill("toolset:77777777-7777-7777-7777-777777777777")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseSkill("skill:not-a-uuid")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseSkill("skill:77777777-7777-7777-7777-777777777777:extra")
	require.ErrorIs(t, err, urn.ErrInvalid)
}

func TestSkillZeroValue(t *testing.T) {
	t.Parallel()

	var zero urn.Skill
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

func TestSkillDatabaseRoundTrip(t *testing.T) {
	t.Parallel()

	original := urn.NewSkill(uuid.MustParse("77777777-7777-7777-7777-777777777777"))
	value, err := original.Value()
	require.NoError(t, err)
	require.Equal(t, original.String(), value)

	var fromString urn.Skill
	err = fromString.Scan(value)
	require.NoError(t, err)
	require.Equal(t, original.ID, fromString.ID)

	var fromBytes urn.Skill
	err = fromBytes.Scan([]byte(original.String()))
	require.NoError(t, err)
	require.Equal(t, original.ID, fromBytes.ID)

	var invalid urn.Skill
	err = invalid.Scan("skill:not-a-uuid")
	require.ErrorIs(t, err, urn.ErrInvalid)

	err = invalid.Scan(42)
	require.Error(t, err)
}
