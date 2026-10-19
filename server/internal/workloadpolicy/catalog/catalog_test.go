package catalog_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/workloadpolicy/catalog"
)

const validPlatform = `
key: example
display_name: Example
description: An example platform.
icon: /access-hub/example.svg
enabled: true
issuer:
  value: https://issuer.example.com
  visibility: hidden
jwks_uri:
  value: https://issuer.example.com/jwks.json
  visibility: read-only
variables:
  - key: org_id
    tier: rule
    label: Organization ID
    pattern: "[a-z0-9-]+"
subject:
  template: "wimse://issuer.example.com/org/{org_id}/agent/"
  wildcard: true
setup:
  steps:
    - id: org
      title: Organization
      phase: collect
      blocks:
        - type: field
          variable: org_id
        - type: subject_rule
    - id: create
      title: Create
      phase: create
      blocks:
        - type: agent_picker
    - id: console
      title: Console
      phase: connect
      blocks:
        - type: computed
          value: token_endpoint
          label: Token endpoint
`

func TestEmbeddedPlatformsAreValid(t *testing.T) {
	t.Parallel()

	platforms, err := catalog.Embedded().Platforms(t.Context())
	require.NoError(t, err)
	require.NotEmpty(t, platforms)
}

func TestEmbeddedClaudeTagPinsOneOrganization(t *testing.T) {
	t.Parallel()

	platforms, err := catalog.Embedded().Platforms(t.Context())
	require.NoError(t, err)

	var claudeTag *catalog.Platform
	for i := range platforms {
		if platforms[i].Key == "claude-tag" {
			claudeTag = &platforms[i]
		}
	}
	require.NotNil(t, claudeTag)
	require.Equal(t, "https://identity.anthropic.com/agents", claudeTag.Issuer.Value)
	require.Equal(t, catalog.VisibilityHidden, claudeTag.Issuer.Visibility)
	require.Equal(t, "wimse://identity.anthropic.com/org/{org_id}/agent/", claudeTag.Subject.Template)
	require.True(t, claudeTag.Subject.Wildcard)
	require.NotNil(t, claudeTag.Setup)
}

func TestParseAcceptsAValidPlatform(t *testing.T) {
	t.Parallel()

	platform, err := catalog.Parse([]byte(validPlatform))
	require.NoError(t, err)
	require.Equal(t, "example", platform.Key)
	require.Len(t, platform.Setup.Steps, 3)
}

func TestParseRefusesInvalidPlatforms(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		from    string
		to      string
		wantErr string
	}{
		{name: "unknown field", from: "enabled: true", to: "enabled: true\nenabeld: true", wantErr: "enabeld"},
		{name: "http issuer", from: "value: https://issuer.example.com\n", to: "value: http://issuer.example.com\n", wantErr: "https"},
		{name: "bad visibility", from: "visibility: hidden", to: "visibility: secret", wantErr: "visibility"},
		{name: "undeclared variable", from: "{org_id}/agent/", to: "{tenant}/agent/", wantErr: "undeclared variable"},
		{name: "wildcard stem not on a delimiter", from: "{org_id}/agent/\"", to: "{org_id}/agent\"", wantErr: "wildcard stem"},
		{name: "star in template", from: "{org_id}/agent/\"", to: "{org_id}/agent/*\"", wantErr: `"*"`},
		{name: "missing pattern", from: "    pattern: \"[a-z0-9-]+\"\n", to: "", wantErr: "pattern is required"},
		{name: "rule variable in issuer", from: "value: https://issuer.example.com\n", to: "value: https://{org_id}.issuer.example.com\n", wantErr: "platform-tier"},
		{name: "unknown block", from: "- type: agent_picker", to: "- type: script", wantErr: "unknown block type"},
		{name: "external icon", from: "icon: /access-hub/example.svg", to: "icon: https://evil.example.com/x.svg", wantErr: "dashboard's origin"},
		{name: "tab-split icon", from: "icon: /access-hub/example.svg", to: "icon: \"/\\t/evil.example.com/x.svg\"", wantErr: "dashboard's origin"},
		{name: "external image", from: "- type: agent_picker", to: "- type: image\n          src: https://evil.example.com/x.png\n          alt: x", wantErr: "dashboard's origin"},
		{name: "unknown computed value", from: "value: token_endpoint", to: "value: client_secret", wantErr: "not one Speakeasy derives"},
		{name: "connect before create", from: "      title: Organization\n      phase: collect", to: "      title: Organization\n      phase: connect", wantErr: "collect, then create, then connect"},
		{name: "no create step", from: "phase: create", to: "phase: collect", wantErr: "exactly one create step"},
		{name: "issuer with a query", from: "value: https://issuer.example.com\n", to: "value: https://issuer.example.com?tenant=x\n", wantErr: "no userinfo, query or fragment"},
		{name: "jwks_uri with a fragment", from: "value: https://issuer.example.com/jwks.json", to: "value: https://issuer.example.com/jwks.json#keys", wantErr: "no userinfo, query or fragment"},
		{name: "link with userinfo", from: "- type: agent_picker", to: "- type: link\n          href: https://user@docs.example.com\n          label: Docs", wantErr: "https URL"},
		{name: "unclosed placeholder", from: "{org_id}/agent/", to: "{org_id/agent/", wantErr: "brace"},
		{name: "named group in pattern", from: `pattern: "[a-z0-9-]+"`, to: `pattern: "(?P<id>[a-z0-9-]+)"`, wantErr: "(?:...) groups"},
		{name: "flag group in pattern", from: `pattern: "[a-z0-9-]+"`, to: `pattern: "(?i)[a-z0-9-]+"`, wantErr: "(?:...) groups"},
		{name: "unicode class in pattern", from: `pattern: "[a-z0-9-]+"`, to: `pattern: "\\pL+"`, wantErr: "Go and JavaScript"},
		{name: "posix class in pattern", from: `pattern: "[a-z0-9-]+"`, to: `pattern: "[[:alnum:]-]+"`, wantErr: "POSIX"},
		{name: "image in markdown", from: "- type: agent_picker", to: "- type: text\n          markdown: \"![x](https://evil.example.com/px)\"", wantErr: "use an image block"},
		{name: "checklist item without a label", from: "- type: agent_picker", to: "- type: checklist_item\n          markdown: Leave empty", wantErr: "needs a label"},
		{name: "checklist item with nothing to do", from: "- type: agent_picker", to: "- type: checklist_item\n          label: Resource", wantErr: "computed value or markdown"},
		{name: "checklist item with both", from: "- type: agent_picker", to: "- type: checklist_item\n          label: Resource\n          value: token_endpoint\n          markdown: Leave empty", wantErr: "not both"},
		{name: "checklist item with unknown value", from: "- type: agent_picker", to: "- type: checklist_item\n          label: Resource\n          value: client_secret", wantErr: "not one Speakeasy derives"},
		{name: "checklist item with an image", from: "- type: agent_picker", to: "- type: checklist_item\n          label: Resource\n          markdown: \"![x](https://evil.example.com/px)\"", wantErr: "use an image block"},
		{name: "form control setting on a platform block", from: "- type: agent_picker", to: "- type: agent_picker\n          placeholder: Pick one", wantErr: "belong to custom flows"},
		{name: "collect after create", from: "      title: Console\n      phase: connect", to: "      title: Console\n      phase: collect", wantErr: "collect, then create, then connect"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			require.Contains(t, validPlatform, tc.from, "test case must change the fixture")
			_, err := catalog.Parse([]byte(strings.Replace(validPlatform, tc.from, tc.to, 1)))
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}

func TestLoadRequiresTheFileNameToBeTheKey(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{"platforms/other.yaml": {Data: []byte(validPlatform)}}
	_, err := catalog.Load(fsys, "platforms")
	require.ErrorContains(t, err, "file name must be the key")
}

func TestLoadOrdersPlatformsByKey(t *testing.T) {
	t.Parallel()

	second := strings.Replace(validPlatform, "key: example", "key: another", 1)
	fsys := fstest.MapFS{
		"platforms/example.yaml": {Data: []byte(validPlatform)},
		"platforms/another.yaml": {Data: []byte(second)},
	}
	platforms, err := catalog.Load(fsys, "platforms")
	require.NoError(t, err)
	require.Equal(t, "another", platforms[0].Key)
	require.Equal(t, "example", platforms[1].Key)
}
