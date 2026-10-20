package policycatalog_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/risk/policycatalog"
)

func TestEveryCatalogValueHasADescription(t *testing.T) {
	t.Parallel()

	catalog, err := policycatalog.Build()
	require.NoError(t, err)

	for _, source := range catalog.Sources {
		require.NotEmpty(t, policycatalog.SourceDescriptions[source], "source %q", source)
	}
	for _, action := range catalog.Actions {
		require.NotEmpty(t, policycatalog.ActionDescriptions[action], "action %q", action)
	}
	for _, policyType := range catalog.PolicyTypes {
		require.NotEmpty(t, policycatalog.PolicyTypeDescriptions[policyType], "policy type %q", policyType)
	}
	for _, entity := range catalog.PresidioEntities {
		require.NotEmpty(t, policycatalog.PresidioEntityDescription(entity), "presidio entity %q", entity)
	}
	categories := policycatalog.CategoryDescriptions()
	for _, category := range catalog.DetectionScopeCategories {
		require.NotEmpty(t, categories[category], "category %q", category)
	}
}

func TestDescribeValuesListsBareValuesWithoutMeaning(t *testing.T) {
	t.Parallel()

	text := policycatalog.DescribeValues([]string{"b", "a"}, func(value string) string {
		if value == "a" {
			return "first"
		}
		return ""
	})
	require.Equal(t, "a: first\nb", text)
}
