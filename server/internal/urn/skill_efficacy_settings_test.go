package urn_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestSkillEfficacySettingsURN(t *testing.T) {
	t.Parallel()

	original := urn.NewSkillEfficacySettings("org_test")
	require.Equal(t, "skill_efficacy_settings:org_test", original.String())

	parsed, err := urn.ParseSkillEfficacySettings(original.String())
	require.NoError(t, err)
	require.Equal(t, original, parsed)

	data, err := json.Marshal(original)
	require.NoError(t, err)
	var decoded urn.SkillEfficacySettings
	require.NoError(t, json.Unmarshal(data, &decoded))
	require.Equal(t, original, decoded)
}
