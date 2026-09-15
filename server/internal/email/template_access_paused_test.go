package email

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAccessPaused_Contract(t *testing.T) {
	t.Parallel()

	tmpl := AccessPaused{
		OrganizationName: "Example Organization",
		ActionURL:        "https://app.getgram.ai/example/billing",
	}

	require.Equal(t, TemplateKeyAccessPaused, tmpl.Key())
	require.Equal(t, map[string]string{
		"organization_name": "Example Organization",
		"action_url":        "https://app.getgram.ai/example/billing",
	}, tmpl.Variables())
	require.False(t, tmpl.AddToAudience())
}
