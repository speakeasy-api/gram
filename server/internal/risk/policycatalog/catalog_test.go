package policycatalog

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/risk/categories"
	"github.com/speakeasy-api/gram/server/internal/scanners"
	"github.com/speakeasy-api/gram/server/internal/scanners/accountidentity"
	"github.com/speakeasy-api/gram/server/internal/scanners/clidestructive"
	"github.com/speakeasy-api/gram/server/internal/scanners/destructivetool"
	"github.com/speakeasy-api/gram/server/internal/scanners/gitleaks"
	"github.com/speakeasy-api/gram/server/internal/scanners/promptinjection"
	"github.com/speakeasy-api/gram/server/internal/scanners/shadowmcpscan"
)

func TestCatalogV1IsDeterministicAndClosed(t *testing.T) {
	t.Parallel()

	catalog, err := Build()
	require.NoError(t, err)
	require.Equal(t, SchemaV1, catalog.Schema)
	require.Equal(t, []string{"block", "flag", "warn"}, catalog.Actions)
	require.Equal(t, []string{"assistant_message", "tool_request", "tool_response", "user_message"}, catalog.PolicyMessageTypes)
	require.NotContains(t, catalog.PolicyMessageTypes, "prompt_attachment")
	require.NotContains(t, catalog.DetectionScopeCategories, "account_identity")
	require.NotContains(t, catalog.DetectionScopeCategories, string(categories.CategoryPromptPolicy))
	require.Empty(t, catalog.PromptInjectionRules)
	require.NotNil(t, catalog.PromptInjectionRules)
	require.NotContains(t, catalog.PresidioEntities, "HARMFUL_CONTENT_REQUEST")
	require.NotContains(t, catalog.PresidioEntities, "PERSON")
	require.NotContains(t, catalog.PresidioEntities, "POLICY_VIOLATION")
	require.NotContains(t, catalog.PresidioEntities, "TOPIC_BOUNDARY_VIOLATION")
	require.NotContains(t, catalog.PresidioEntities, "UNAUTHORIZED_ACTION")
	require.NotContains(t, catalog.PresidioEntities, "US_DRIVER_LICENSE")
	require.NotContains(t, catalog.DisabledRules, PresidioDeadLetterRule)
	require.Contains(t, catalog.DisabledRules, accountidentity.RulePersonalAccount)
	require.Contains(t, catalog.DisabledRules, accountidentity.RuleUnapprovedDomain)
	require.Contains(t, catalog.DisabledRules, destructivetool.Rule)
	require.Contains(t, catalog.DisabledRules, promptinjection.Rule)
	require.Contains(t, catalog.DisabledRules, shadowmcpscan.Rule)
	for _, ruleID := range clidestructive.ReportableRuleIDs() {
		require.Contains(t, catalog.DisabledRules, ruleID)
	}
	require.Contains(t, catalog.DisabledRules, gitleaks.SecretAccessKeyRuleID)
	require.NotContains(t, catalog.DisabledRules, gitleaks.AccessKeyIDRuleID)
	for _, ruleID := range catalog.DisabledRules {
		require.NoError(t, scanners.ValidateRuleID(ruleID), ruleID)
	}

	for _, values := range [][]string{
		catalog.PolicyTypes,
		catalog.Actions,
		catalog.Sources,
		catalog.FlagOnlySources,
		catalog.PresidioEntities,
		catalog.PromptInjectionRules,
		catalog.DisabledRules,
		catalog.DetectionScopeCategories,
		catalog.PolicyMessageTypes,
	} {
		require.NotNil(t, values)
		require.True(t, slices.IsSorted(values))
		require.Equal(t, values, slices.Compact(slices.Clone(values)))
	}

	encoded, err := CanonicalJSON(catalog)
	require.NoError(t, err)
	var roundTrip Catalog
	require.NoError(t, json.Unmarshal(encoded, &roundTrip))
	require.Equal(t, catalog, roundTrip)

	fingerprint, err := Fingerprint(catalog)
	require.NoError(t, err)
	require.Equal(t, "sha256:812dd2f822a6964e1195b7562fbba38a90664b8b7dcb12c78a1580c749988ae5", fingerprint)
	require.True(t, strings.HasPrefix(fingerprint, "sha256:"))
	require.Len(t, fingerprint, len("sha256:")+64)

	again, err := Build()
	require.NoError(t, err)
	againFingerprint, err := Fingerprint(again)
	require.NoError(t, err)
	require.Equal(t, fingerprint, againFingerprint)
}

func TestPresidioEntityRuleRoundTrips(t *testing.T) {
	t.Parallel()

	catalog, err := Build()
	require.NoError(t, err)
	for _, entity := range catalog.PresidioEntities {
		ruleID := CanonicalPresidioRuleID(entity)
		got, ok := PresidioEntityForRuleID(ruleID)
		require.True(t, ok, ruleID)
		require.Equal(t, entity, got)
		require.Contains(t, catalog.DisabledRules, ruleID)
	}

	_, ok := PresidioEntityForRuleID("pii.person")
	require.False(t, ok)
	_, ok = PresidioEntityForRuleID("pii.CREDIT_CARD")
	require.False(t, ok)
	_, ok = PresidioEntityForRuleID(PresidioDeadLetterRule)
	require.False(t, ok)
	_, ok = PresidioEntityForRuleID("secret.example")
	require.False(t, ok)
}
