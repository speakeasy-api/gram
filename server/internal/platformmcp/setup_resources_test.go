package platformmcp

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSetupResourceValidation(t *testing.T) {
	t.Parallel()

	valid := SetupResource{
		URI:         setupResourceURI("fixture", "provider_setup"),
		Name:        "fixture-provider-setup",
		Title:       "Fixture provider setup",
		Description: "Static reviewed fixture guide.",
		Text:        "# Setup\n",
	}
	require.True(t, validSetupResource(valid))

	for _, resource := range []SetupResource{
		{URI: "gram://platform-mcp/setup/%zz/provider_setup", Name: valid.Name, Title: valid.Title, Description: valid.Description, Text: valid.Text},
		{URI: "https://example.test/setup", Name: valid.Name, Title: valid.Title, Description: valid.Description, Text: valid.Text},
		{URI: setupResourceURI("fixture", "provider_setup/extra"), Name: valid.Name, Title: valid.Title, Description: valid.Description, Text: valid.Text},
		{URI: valid.URI, Name: valid.Name, Title: valid.Title, Description: valid.Description, Text: strings.Repeat("a", maxSetupResourceBytes+1)},
	} {
		require.False(t, validSetupResource(resource))
	}
}
