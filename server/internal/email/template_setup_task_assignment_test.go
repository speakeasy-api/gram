package email

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSetupTaskAssignment_MetadataAndVariables(t *testing.T) {
	t.Parallel()

	tmpl := SetupTaskAssignment{
		AssignerName:     "Alex Example",
		OrganizationName: "Example Organization",
		TaskTitle:        "Configure identity provider",
		TaskDescription:  "Connect single sign-on for your organization.",
		SetupLink:        "https://app.example.com/example/setup",
	}

	require.Equal(t, TemplateKeySetupTaskAssignment, tmpl.Key())
	require.Equal(t, map[string]string{
		"assigner_name":     "Alex Example",
		"organization_name": "Example Organization",
		"task_title":        "Configure identity provider",
		"task_description":  "Connect single sign-on for your organization.",
		"setup_link":        "https://app.example.com/example/setup",
	}, tmpl.Variables())
	require.False(t, tmpl.AddToAudience(),
		"task assignments should not add recipients to the Loops audience")
}
