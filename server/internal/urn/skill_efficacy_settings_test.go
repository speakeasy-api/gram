package urn_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestSkillEfficacySettingsRoundTrip(t *testing.T) {
	t.Parallel()

	original := urn.NewSkillEfficacySettings("org_test")
	require.Equal(t, "skill_efficacy_settings:org_test", original.String())

	parsed, err := urn.ParseSkillEfficacySettings(original.String())
	require.NoError(t, err)
	require.Equal(t, original, parsed)

	data, err := json.Marshal(original)
	require.NoError(t, err)
	require.Equal(t, `"skill_efficacy_settings:org_test"`, string(data))
	var decoded urn.SkillEfficacySettings
	require.NoError(t, json.Unmarshal(data, &decoded))
	require.Equal(t, original, decoded)

	text, err := original.MarshalText()
	require.NoError(t, err)
	var fromText urn.SkillEfficacySettings
	require.NoError(t, fromText.UnmarshalText(text))
	require.Equal(t, original, fromText)

	value, err := original.Value()
	require.NoError(t, err)
	var fromDB urn.SkillEfficacySettings
	require.NoError(t, fromDB.Scan(value))
	require.Equal(t, original, fromDB)
}

func TestSkillEfficacySettingsRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	for _, value := range []string{
		"",
		"skill_efficacy_settings",
		"skill_efficacy_settings:",
		"wrong:org_test",
		"skill_efficacy_settings:org:extra",
		"skill_efficacy_settings:" + strings.Repeat("a", 129),
	} {
		_, err := urn.ParseSkillEfficacySettings(value)
		require.ErrorIs(t, err, urn.ErrInvalid)
	}

	_, err := urn.NewSkillEfficacySettings("").MarshalJSON()
	require.ErrorIs(t, err, urn.ErrInvalid)
	_, err = urn.NewSkillEfficacySettings(strings.Repeat("a", 129)).MarshalText()
	require.ErrorIs(t, err, urn.ErrInvalid)

	mutated := urn.NewSkillEfficacySettings("org_test")
	mutated.ID = ""
	_, err = mutated.Value()
	require.ErrorIs(t, err, urn.ErrInvalid)
}
