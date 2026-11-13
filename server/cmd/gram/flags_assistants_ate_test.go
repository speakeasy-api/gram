package gram

import (
	"github.com/speakeasy-api/gram/server/internal/assistants"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"go.opentelemetry.io/otel/trace"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
)

func TestATEPreviewEgressHostsAreExplicit(t *testing.T) {
	t.Parallel()
	c := kitCLIContext(t, map[string]string{"assistant-ate-egress-host": "tools.example.com"})
	require.Equal(t, []string{"tools.example.com"}, c.StringSlice("assistant-ate-egress-host"))
}

func TestATEOnlyRuntimeConstruction(t *testing.T) {
	t.Parallel()
	serverURL, err := url.Parse("https://1.1.1.1")
	require.NoError(t, err)
	tp := trace.NewNoopTracerProvider()
	policy := guardian.NewDefaultPolicy(tp)
	for _, tt := range []struct {
		name      string
		values    map[string]string
		wantError string
	}{
		{name: "ATE only has explicit unavailable legacy", values: map[string]string{"environment": "dev", "assistant-ate-enabled": "true", "assistant-ate-preview-id": "preview-one", "assistant-runtime-image-tag": "test"}},
		{name: "legacy still requires a backend", values: map[string]string{"environment": "dev", "assistant-runtime-image-tag": "test"}, wantError: "requires at least one backend"},
		{name: "configured GKE is not discarded", values: map[string]string{"environment": "dev", "assistant-ate-enabled": "true", "assistant-ate-preview-id": "preview-one", "assistant-runtime-image-tag": "test", "assistant-runtime-gke-cluster-endpoint": "https://cluster.example.com", "assistant-runtime-gke-cluster-ca": "not-base64"}, wantError: "decode --assistant-runtime-gke-cluster-ca"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			backend, err := newAssistantRuntime(t.Context(), nil, tp, kitCLIContext(t, tt.values), policy, nil, serverURL)
			if tt.wantError != "" {
				require.ErrorContains(t, err, tt.wantError)
				return
			}
			require.NoError(t, err)
			require.Equal(t, assistants.RuntimeProviderGKE, backend.Backend())
			require.True(t, backend.SupportsBackend(assistants.RuntimeProviderGKE))
			require.False(t, backend.SupportsBackend("ate"))
			require.False(t, backend.SupportsBackend(assistants.RuntimeProviderLocal))
			require.Empty(t, backend.ImageRef())
		})
	}
}

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
			t.Parallel()
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
		case *cli.StringSliceFlag:
			require.Nil(t, f.Value, f.Name)
		}
	}
}
