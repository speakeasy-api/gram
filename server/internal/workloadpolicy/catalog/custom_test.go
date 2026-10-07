package catalog_test

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/workloadpolicy/catalog"
)

func TestEmbeddedCustomFlowsAreValid(t *testing.T) {
	t.Parallel()

	flows, err := catalog.Embedded().CustomFlows(t.Context())
	require.NoError(t, err)
	require.Equal(t, "Register new access", flows.RegisterPlatform.Title)
	require.Equal(t, "Edit platform", flows.EditPlatform.Title)
	require.Equal(t, "Allow access", flows.AllowAccess.Title)
	require.Equal(t, "Edit access", flows.EditAccess.Title)
}

func readCustomFlows(t *testing.T) string {
	t.Helper()

	raw, err := os.ReadFile("custom.yaml")
	require.NoError(t, err)
	return string(raw)
}

func TestParseCustomFlowsRefusesContractViolations(t *testing.T) {
	t.Parallel()

	valid := readCustomFlows(t)
	editPlatformIssuer := "          placeholder: https://identity.example.com\n          read_only: true\n"
	tests := []struct {
		name    string
		from    string
		to      string
		wantErr string
	}{
		{
			name:    "missing required field",
			from:    "- type: input\n          field: jwks_uri\n          format: jwks_uri\n          label: JWKS URI\n          placeholder: https://identity.example.com/.well-known/jwks.json\n",
			to:      "- type: link\n          href: https://identity.example.com/.well-known/jwks.json\n          label: JWKS URI\n",
			wantErr: `register_platform: step "platform": input jwks_uri is required`,
		},
		{
			name:    "wrong format",
			from:    "field: issuer\n          format: issuer_url",
			to:      "field: issuer\n          format: jwks_uri",
			wantErr: `input issuer must have format "issuer_url"`,
		},
		{
			name:    "editable issuer when editing a platform",
			from:    editPlatformIssuer,
			to:      "          placeholder: https://identity.example.com\n",
			wantErr: "edit_platform: step \"platform\" block 3: input issuer must be read_only",
		},
		{
			name:    "read-only field the form submits",
			from:    "          field: label\n          format: none\n",
			to:      "          field: label\n          format: none\n          read_only: true\n",
			wantErr: "input label must not be read_only",
		},
		{
			name:    "two steps",
			from:    "  steps:\n    - id: access\n      title: Access\n      blocks:\n",
			to:      "  steps:\n    - id: intro\n      title: Intro\n      blocks: []\n    - id: access\n      title: Access\n      blocks:\n",
			wantErr: "allow_access: a custom flow has exactly one step, found 2",
		},
		{
			name:    "duplicate field",
			from:    "        - type: wildcard_caution\n",
			to:      "        - type: wildcard_caution\n        - type: input\n          field: subject\n          format: subject_rule\n          label: Subject again\n",
			wantErr: "input subject is placed more than once",
		},
		{
			name:    "field the flow does not submit",
			from:    "        - type: wildcard_caution\n",
			to:      "        - type: wildcard_caution\n        - type: input\n          field: jwks_uri\n          format: jwks_uri\n          label: JWKS URI\n",
			wantErr: "input jwks_uri is not one this flow submits",
		},
		{
			name:    "catalog-only block",
			from:    "        - type: wildcard_caution\n",
			to:      "        - type: wildcard_caution\n        - type: subject_rule\n",
			wantErr: "subject_rule blocks are not allowed here",
		},
		{
			name:    "agent picker in a platform flow",
			from:    "        - type: tags\n          label: Tags\n          placeholder: production, ci\n",
			to:      "        - type: agent_picker\n          label: Agent\n        - type: tags\n          label: Tags\n          placeholder: production, ci\n",
			wantErr: "register_platform: step \"platform\" block 5: agent_picker is not allowed in this flow",
		},
		{
			name:    "missing agent picker",
			from:    "        - type: agent_picker\n          label: Agent\n",
			to:      "        - type: text\n          markdown: Agent\n          label: Agent\n",
			wantErr: "allow_access: step \"access\": agent_picker is required",
		},
		{
			name:    "phase on a step",
			from:    "    - id: access\n      title: Access\n",
			to:      "    - id: access\n      title: Access\n      phase: create\n",
			wantErr: "has no phase",
		},
		{
			name:    "unknown field",
			from:    "  submit_label: Register\n",
			to:      "  submit_label: Register\n  submit_lable: Register\n",
			wantErr: "submit_lable",
		},
		{
			name:    "missing pending label",
			from:    "  pending_label: Registering…\n",
			to:      "",
			wantErr: "register_platform: pending_label is required",
		},
		{
			name:    "input setting on tags",
			from:    "          placeholder: support, production\n",
			to:      "          placeholder: support, production\n          read_only: true\n",
			wantErr: "tags: field, format, multiline and read_only belong to inputs",
		},
		{
			name:    "catalog setting on a custom block",
			from:    "        - type: wildcard_caution\n",
			to:      "        - type: wildcard_caution\n          value: token_endpoint\n",
			wantErr: "belong to catalog platforms",
		},
		{
			name:    "image in help",
			from:    "help: Optional labels for finding this machine later. Not used for matching.",
			to:      `help: "![x](https://evil.example.com/px)"`,
			wantErr: "help must not hold an image",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			require.Contains(t, valid, tc.from, "test case must change the fixture")
			_, err := catalog.ParseCustomFlows([]byte(strings.Replace(valid, tc.from, tc.to, 1)))
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}

func TestParseRefusesCustomFlowBlocksInAPlatform(t *testing.T) {
	t.Parallel()

	for _, block := range []string{
		"- type: input\n          field: name\n          format: platform_name\n          label: Name",
		"- type: wildcard_caution",
	} {
		_, err := catalog.Parse([]byte(strings.Replace(validPlatform, "- type: agent_picker", block, 1)))
		require.ErrorContains(t, err, "blocks are not allowed here")
	}
}
