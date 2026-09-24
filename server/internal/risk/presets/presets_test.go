package presets_test

import (
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/risk/policycatalog"
	"github.com/speakeasy-api/gram/server/internal/risk/policycore"
	"github.com/speakeasy-api/gram/server/internal/risk/presets"
)

func TestPresetsResolveToValidCreatePayloads(t *testing.T) {
	t.Parallel()

	catalog, err := policycatalog.Build()
	require.NoError(t, err)

	ids := presets.IDs()
	require.Len(t, uniqueStrings(ids), len(ids), "preset ids must be unique")
	require.Equal(t, ids, keys(presets.All()))

	for _, preset := range presets.All() {
		t.Run(preset.ID, func(t *testing.T) {
			t.Parallel()
			require.NotEmpty(t, preset.Label)
			require.NotEmpty(t, preset.Description)
			require.NotEmpty(t, preset.Keywords)
			require.NoError(t, policycore.ValidateName(preset.Label))
			require.Contains(t, catalog.PolicyTypes, preset.PolicyType)
			require.Contains(t, catalog.Actions, preset.Action)
			require.GreaterOrEqual(t, preset.Score, 0.1)
			require.LessOrEqual(t, preset.Score, 10.0)
			require.LessOrEqual(t, utf8.RuneCountInString(preset.UserMessage), 500)

			draft := preset.Draft()
			require.Equal(t, preset.ID, draft.PresetID)
			require.Equal(t, preset.Label, draft.Name)

			switch preset.PolicyType {
			case presets.PolicyTypeStandard:
				require.NotEmpty(t, preset.Sources)
				for _, source := range preset.Sources {
					require.Contains(t, catalog.Sources, source)
				}
				for _, entity := range preset.PresidioEntities {
					require.Contains(t, catalog.PresidioEntities, entity)
				}
				require.Equal(t, slices.Contains(preset.Sources, policycatalog.PresidioSource), len(preset.PresidioEntities) > 0, "presidio entities travel with the presidio source")
				require.NoError(t, policycore.ValidateSourceAction(preset.Sources, preset.Action))
				require.Empty(t, preset.Prompt)
			case presets.PolicyTypePromptBased:
				require.Empty(t, preset.Sources)
				require.Empty(t, preset.PresidioEntities)
				require.NotEmpty(t, preset.Prompt)
				require.LessOrEqual(t, utf8.RuneCountInString(preset.Prompt), 4000)
			default:
				t.Fatalf("unknown policy type %q", preset.PolicyType)
			}
		})
	}
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func TestByIDReturnsCopies(t *testing.T) {
	t.Parallel()

	preset, ok := presets.ByID("secrets_and_credentials")
	require.True(t, ok)
	preset.Sources[0] = "mutated"
	again, ok := presets.ByID("secrets_and_credentials")
	require.True(t, ok)
	require.Equal(t, "gitleaks", again.Sources[0])

	_, ok = presets.ByID("missing")
	require.False(t, ok)
}

func TestSuggestMapsDescriptionsToPresets(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		description string
		preset      string
		policyType  string
	}{
		{"stop agents deleting anything in production", "destructive_production_actions", presets.PolicyTypePromptBased},
		{"Block API keys and passwords from being pasted into prompts", "secrets_and_credentials", presets.PolicyTypeStandard},
		{"flag customer personal data like emails and credit card numbers", "customer_pii_egress", presets.PolicyTypeStandard},
		{"catch prompt injection coming back from tool outputs", "prompt_injection", presets.PolicyTypeStandard},
		{"tell me when people connect unapproved MCP servers", "unapproved_mcp_servers", presets.PolicyTypeStandard},
		{"warn when someone uses a personal gmail account", "non_corporate_accounts", presets.PolicyTypeStandard},
	} {
		t.Run(test.description, func(t *testing.T) {
			t.Parallel()
			suggestion := presets.Suggest(test.description)
			require.NotNil(t, suggestion.Preset)
			require.Equal(t, test.preset, suggestion.Preset.ID)
			require.Equal(t, test.preset, suggestion.Draft.PresetID)
			require.Equal(t, test.policyType, suggestion.Draft.PolicyType)
			require.Greater(t, suggestion.Confidence, 0.0)
			require.Contains(t, suggestion.Rationale, suggestion.Preset.Label)
			for _, alternative := range suggestion.Alternatives {
				require.NotEqual(t, test.preset, alternative.Preset.ID)
				require.LessOrEqual(t, alternative.Confidence, suggestion.Confidence)
			}
		})
	}
}

func TestSuggestFallsBackToBespokePromptPolicy(t *testing.T) {
	t.Parallel()

	description := "Agents must never recommend a competitor's product when answering support tickets, even if asked directly by the customer."
	suggestion := presets.Suggest(description)
	require.Nil(t, suggestion.Preset)
	require.Empty(t, suggestion.Alternatives)
	require.InDelta(t, 0.0, suggestion.Confidence, 0)
	require.Equal(t, presets.PolicyTypePromptBased, suggestion.Draft.PolicyType)
	require.Equal(t, description, suggestion.Draft.Prompt)
	require.Equal(t, "flag", suggestion.Draft.Action)
	require.NoError(t, policycore.ValidateName(suggestion.Draft.Name))
	require.LessOrEqual(t, utf8.RuneCountInString(suggestion.Draft.Name), 60)
	require.True(t, strings.HasPrefix(description, suggestion.Draft.Name), "name is a prefix of the description cut at a word boundary")
}

func keys(all []presets.Preset) []string {
	out := make([]string, 0, len(all))
	for _, preset := range all {
		out = append(out, preset.ID)
	}
	return out
}
