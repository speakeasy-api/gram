package urn_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestChatAnalysisSettingsRoundTrip(t *testing.T) {
	t.Parallel()

	original := urn.NewChatAnalysisSettings("org_test")
	require.Equal(t, "chat_analysis_settings:org_test", original.String())

	parsed, err := urn.ParseChatAnalysisSettings(original.String())
	require.NoError(t, err)
	require.Equal(t, original, parsed)

	data, err := json.Marshal(original)
	require.NoError(t, err)
	require.Equal(t, `"chat_analysis_settings:org_test"`, string(data))
	var decoded urn.ChatAnalysisSettings
	require.NoError(t, json.Unmarshal(data, &decoded))
	require.Equal(t, original, decoded)

	text, err := original.MarshalText()
	require.NoError(t, err)
	var fromText urn.ChatAnalysisSettings
	require.NoError(t, fromText.UnmarshalText(text))
	require.Equal(t, original, fromText)

	value, err := original.Value()
	require.NoError(t, err)
	var fromDB urn.ChatAnalysisSettings
	require.NoError(t, fromDB.Scan(value))
	require.Equal(t, original, fromDB)
}

func TestChatAnalysisSettingsRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	for _, value := range []string{
		"",
		"chat_analysis_settings",
		"chat_analysis_settings:",
		"wrong:org_test",
		"chat_analysis_settings:org:extra",
		"chat_analysis_settings:" + strings.Repeat("a", 129),
	} {
		_, err := urn.ParseChatAnalysisSettings(value)
		require.ErrorIs(t, err, urn.ErrInvalid)
	}

	_, err := urn.NewChatAnalysisSettings("").MarshalJSON()
	require.ErrorIs(t, err, urn.ErrInvalid)
	_, err = urn.NewChatAnalysisSettings(strings.Repeat("a", 129)).MarshalText()
	require.ErrorIs(t, err, urn.ErrInvalid)

	mutated := urn.NewChatAnalysisSettings("org_test")
	mutated.ID = ""
	_, err = mutated.Value()
	require.ErrorIs(t, err, urn.ErrInvalid)
}
