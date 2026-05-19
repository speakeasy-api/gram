package functions

import (
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
