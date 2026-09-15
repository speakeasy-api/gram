package email

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTrialEndingSoon_Contract(t *testing.T) {
	t.Parallel()

	tmpl := TrialEndingSoon{
		OrganizationName: "Example Organization",
		TrialEndDate:     "August 18, 2026",
		ActionURL:        "https://app.getgram.ai/example/billing",
	}

	require.Equal(t, TemplateKeyTrialEndingSoon, tmpl.Key())
	require.Equal(t, map[string]string{
		"organization_name": "Example Organization",
		"trial_end_date":    "August 18, 2026",
		"action_url":        "https://app.getgram.ai/example/billing",
	}, tmpl.Variables())
	require.False(t, tmpl.AddToAudience())
}
