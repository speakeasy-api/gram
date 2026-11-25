package oktaseed

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpregistry"
)

func TestVersion_TracksTheVendorTable(t *testing.T) {
	t.Parallel()

	before := Version()
	require.Len(t, before, versionHexLength)
	require.Equal(t, before, versionOf(slices.Clone(Vendors)), "the same table yields the same version")

	changed := slices.Clone(Vendors)
	changed[0].Mapping.OINNames = append([]string{"another-key"}, changed[0].Mapping.OINNames...)
	require.NotEqual(t, before, versionOf(changed), "a new OIN name must produce a new version, or deploys would not apply it")
}

func TestVersion_HashesAPinnedEncoding(t *testing.T) {
	t.Parallel()

	encoded, err := json.Marshal(versionInput{
		Revision: 7,
		Vendors: []Vendor{{
			Name:             "com.example/mcp",
			Title:            "Example",
			Description:      "An example.",
			WebsiteURL:       "https://example.com",
			DocumentationURL: "https://example.com/docs",
			IconURL:          "https://example.com/icon.png",
			SupportsDCR:      true,
			Remotes:          []Remote{{Type: "streamable-http", URL: "https://mcp.example.com/mcp"}},
			Mapping: mcpregistry.OktaMapping{
				OINNames:         []string{"example"},
				OINIntegrationID: "",
				XAASignOnModes:   nil,
				XAAIssuer:        "https://auth.example.com",
			},
		}},
	})
	require.NoError(t, err)
	require.JSONEq(t, `{"revision":7,"vendors":[{"name":"com.example/mcp","title":"Example","description":"An example.","websiteUrl":"https://example.com","documentationUrl":"https://example.com/docs","iconUrl":"https://example.com/icon.png","supportsDcr":true,"remotes":[{"type":"streamable-http","url":"https://mcp.example.com/mcp"}],"mapping":{"oinNames":["example"],"xaaIssuer":"https://auth.example.com"}}]}`, string(encoded))
}
