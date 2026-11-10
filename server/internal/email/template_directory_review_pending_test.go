package email

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDirectoryReviewPending_Contract(t *testing.T) {
	t.Parallel()

	tmpl := DirectoryReviewPending{
		OrganizationName:       "Example Organization",
		Summary:                "3 directory changes need a decision",
		NewGroupCount:          "1",
		NewAttributeValueCount: "0",
		SourceMissingCount:     "2",
		ReviewURL:              "https://app.example.com/example/identity",
		IsReminder:             "false",
	}

	require.Equal(t, TemplateKeyDirectoryReviewPending, tmpl.Key())
	require.False(t, tmpl.AddToAudience())
	require.Equal(t, map[string]string{
		"organization_name":         "Example Organization",
		"summary":                   "3 directory changes need a decision",
		"new_group_count":           "1",
		"new_attribute_value_count": "0",
		"source_missing_count":      "2",
		"review_url":                "https://app.example.com/example/identity",
		"is_reminder":               "false",
	}, tmpl.Variables())
}
