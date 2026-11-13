package gram

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
)

func TestATEPreviewStartupIsExplicit(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name      string
		values    map[string]string
		wantError string
	}{
		{name: "disabled preserves legacy"},
		{name: "production refused", values: map[string]string{"assistant-ate-enabled": "true", "assistant-ate-preview-id": "preview-one"}, wantError: "dev environment"},
		{name: "local refused", values: map[string]string{"environment": "local", "assistant-ate-enabled": "true", "assistant-ate-preview-id": "preview-one"}, wantError: "dev environment"},
		{name: "unnamed dev refused", values: map[string]string{"environment": "dev", "assistant-ate-enabled": "true"}, wantError: "preview identifier"},
		{name: "implicit enable refused", values: map[string]string{"environment": "dev", "assistant-ate-preview-id": "preview-one"}, wantError: "explicit assistant ATE enablement"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := installKitLocalRuntimeFromCLI(kitCLIContext(t, tt.values), nil, nil, nil, nil, nil)
			if tt.wantError == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tt.wantError)
			}
		})
	}
}

func TestATEPreviewFlagsHaveNoEnablementDefaults(t *testing.T) {
	t.Parallel()
	for _, flag := range assistantATERuntimeFlags() {
		switch f := flag.(type) {
		case *cli.BoolFlag:
			require.False(t, f.Value, f.Name)
		case *cli.StringFlag:
			require.Empty(t, f.Value, f.Name)
		}
	}
}
