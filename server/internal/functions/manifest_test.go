package functions

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateManifestToolV0_NameUsesMCPToolNamePattern(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		tool    ManifestToolV0
		wantErr bool
	}{
		{
			name: "valid camelCase name",
			tool: ManifestToolV0{
				Name:        "getAccessibleResources",
				Description: "Get accessible resources",
				InputSchema: nil,
				Variables:   nil,
				AuthInput:   nil,
				Annotations: nil,
				Tags:        nil,
				Meta:        nil,
			},
			wantErr: false,
		},
		{
			name: "valid dotted name",
			tool: ManifestToolV0{
				Name:        "admin.tools.list",
				Description: "List admin tools",
				InputSchema: nil,
				Variables:   nil,
				AuthInput:   nil,
				Annotations: nil,
				Tags:        nil,
				Meta:        nil,
			},
			wantErr: false,
		},
		{
			name: "invalid slash name",
			tool: ManifestToolV0{
				Name:        "admin/tools/list",
				Description: "List admin tools",
				InputSchema: nil,
				Variables:   nil,
				AuthInput:   nil,
				Annotations: nil,
				Tags:        nil,
				Meta:        nil,
			},
			wantErr: true,
		},
		{
			name: "invalid space name",
			tool: ManifestToolV0{
				Name:        "admin tools list",
				Description: "List admin tools",
				InputSchema: nil,
				Variables:   nil,
				AuthInput:   nil,
				Annotations: nil,
				Tags:        nil,
				Meta:        nil,
			},
			wantErr: true,
		},
		{
			name: "invalid comma name",
			tool: ManifestToolV0{
				Name:        "admin,tools,list",
				Description: "List admin tools",
				InputSchema: nil,
				Variables:   nil,
				AuthInput:   nil,
				Annotations: nil,
				Tags:        nil,
				Meta:        nil,
			},
			wantErr: true,
		},
		{
			name: "invalid long name",
			tool: ManifestToolV0{
				Name:        strings.Repeat("a", 129),
				Description: "List admin tools",
				InputSchema: nil,
				Variables:   nil,
				AuthInput:   nil,
				Annotations: nil,
				Tags:        nil,
				Meta:        nil,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := validateManifestToolV0(tt.tool)
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
		})
	}
}

func TestValidateManifestToolV0_TagsWithinLimit(t *testing.T) {
	t.Parallel()

	tool := ManifestToolV0{
		Name:        "tagged_tool",
		Description: "A tool with tags",
		InputSchema: nil,
		Variables:   nil,
		AuthInput:   nil,
		Annotations: nil,
		Tags:        []string{"alpha", "beta", "gamma"},
		Meta:        nil,
	}

	require.NoError(t, validateManifestToolV0(tool))
}

func TestValidateManifestToolV0_TagsAtLimit(t *testing.T) {
	t.Parallel()

	tags := make([]string, 40)
	for i := range tags {
		tags[i] = "tag"
	}

	tool := ManifestToolV0{
		Name:        "tagged_tool",
		Description: "A tool with the max tags",
		InputSchema: nil,
		Variables:   nil,
		AuthInput:   nil,
		Annotations: nil,
		Tags:        tags,
		Meta:        nil,
	}

	require.NoError(t, validateManifestToolV0(tool))
}

func TestValidateManifestToolV0_TagsExceedLimit(t *testing.T) {
	t.Parallel()

	tags := make([]string, 41)
	for i := range tags {
		tags[i] = "tag"
	}

	tool := ManifestToolV0{
		Name:        "tagged_tool",
		Description: "A tool with too many tags",
		InputSchema: nil,
		Variables:   nil,
		AuthInput:   nil,
		Annotations: nil,
		Tags:        tags,
		Meta:        nil,
	}

	err := validateManifestToolV0(tool)
	require.Error(t, err)
	require.Contains(t, err.Error(), "maximum of 40")
}

func TestManifestToolV0_UnmarshalTags(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"name": "tagged_tool",
		"description": "with tags",
		"inputSchema": null,
		"variables": null,
		"tags": ["alpha", "beta"],
		"meta": null
	}`)

	var tool ManifestToolV0
	require.NoError(t, json.Unmarshal(raw, &tool))
	require.Equal(t, []string{"alpha", "beta"}, tool.Tags)
}

func TestManifestToolV0_UnmarshalMissingTagsIsNil(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"name": "untagged_tool",
		"description": "no tags",
		"inputSchema": null,
		"variables": null,
		"meta": null
	}`)

	var tool ManifestToolV0
	require.NoError(t, json.Unmarshal(raw, &tool))
	require.Nil(t, tool.Tags)
}
