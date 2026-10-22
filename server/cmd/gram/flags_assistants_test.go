package gram

import (
	"flag"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"

	"github.com/speakeasy-api/gram/server/internal/assistants"
)

func TestAssistantRuntimeConfigFromCLIAcceptsGKEProvider(t *testing.T) {
	t.Parallel()

	ctx := newAssistantRuntimeCLIContext(t, map[string]string{
		"assistant-runtime-provider": "gke",
	})

	serverURL := &url.URL{Scheme: "https", Host: "gram.example.com"}
	cfg, err := assistantRuntimeConfigFromCLI(ctx, serverURL)
	require.NoError(t, err)
	require.Equal(t, assistants.RuntimeProviderGKE, cfg.Provider)
	require.Equal(t, serverURL, cfg.GKE.ServerURL)
}

func TestAssistantRuntimeConfigFromCLIRejectsUnknownProvider(t *testing.T) {
	t.Parallel()

	ctx := newAssistantRuntimeCLIContext(t, map[string]string{})
	require.NoError(t, ctx.Set("assistant-runtime-provider", "bogus"))

	serverURL := &url.URL{Scheme: "https", Host: "gram.example.com"}
	cfg, err := assistantRuntimeConfigFromCLI(ctx, serverURL)
	require.ErrorContains(t, err, "invalid assistant runtime provider: bogus")
	require.Empty(t, cfg.Provider)
}

func TestAssistantRuntimeConfigFromCLIPrefersOverrideURL(t *testing.T) {
	t.Parallel()

	ctx := newAssistantRuntimeCLIContext(t, map[string]string{
		"assistant-runtime-provider":   assistants.RuntimeProviderGKE,
		"assistant-runtime-server-url": "https://runtime.example.com",
	})

	cfg, err := assistantRuntimeConfigFromCLI(ctx, &url.URL{Scheme: "https", Host: "gram.example.com"})
	require.NoError(t, err)
	require.Equal(t, "https://runtime.example.com", cfg.GKE.ServerURL.String())
}

func TestAssistantRuntimeConfigFromCLIFallsBackToServerURL(t *testing.T) {
	t.Parallel()

	ctx := newAssistantRuntimeCLIContext(t, map[string]string{
		"assistant-runtime-provider": assistants.RuntimeProviderGKE,
	})

	serverURL := &url.URL{Scheme: "https", Host: "gram.example.com"}
	cfg, err := assistantRuntimeConfigFromCLI(ctx, serverURL)
	require.NoError(t, err)
	require.Equal(t, serverURL, cfg.GKE.ServerURL)
}

func TestAssistantRuntimeConfigFromCLIAcceptsLocalProvider(t *testing.T) {
	t.Parallel()

	ctx := newAssistantRuntimeCLIContext(t, map[string]string{
		"assistant-runtime-provider": assistants.RuntimeProviderLocal,
		"environment":                "local",
	})

	serverURL := &url.URL{Scheme: "https", Host: "localhost:8080"}
	cfg, err := assistantRuntimeConfigFromCLI(ctx, serverURL)
	require.NoError(t, err)
	require.Equal(t, assistants.RuntimeProviderLocal, cfg.Provider)
	require.True(t, cfg.Local.Enabled)
	// The container-facing URL swaps the host for Docker's host gateway alias
	// while preserving scheme and port.
	require.Equal(t, "https://host.docker.internal:8080", cfg.Local.ServerURL.String())
}

func TestAssistantRuntimeConfigFromCLILocalHonorsOverrideURL(t *testing.T) {
	t.Parallel()

	ctx := newAssistantRuntimeCLIContext(t, map[string]string{
		"assistant-runtime-provider":   assistants.RuntimeProviderLocal,
		"assistant-runtime-server-url": "https://runtime.example.com",
		"environment":                  "local",
	})

	cfg, err := assistantRuntimeConfigFromCLI(ctx, &url.URL{Scheme: "https", Host: "localhost:8080"})
	require.NoError(t, err)
	require.Equal(t, "https://runtime.example.com", cfg.Local.ServerURL.String())
}

func TestAssistantRuntimeConfigFromCLIEnablesLocalInLocalEnvironment(t *testing.T) {
	t.Parallel()

	ctx := newAssistantRuntimeCLIContext(t, map[string]string{
		"assistant-runtime-provider": assistants.RuntimeProviderGKE,
		"environment":                "local",
	})

	cfg, err := assistantRuntimeConfigFromCLI(ctx, &url.URL{Scheme: "https", Host: "localhost:8080"})
	require.NoError(t, err)
	require.Equal(t, assistants.RuntimeProviderGKE, cfg.Provider)
	// Locally created rows stay routable for cleanup even when targeting
	// another backend.
	require.True(t, cfg.Local.Enabled)
}

func TestAssistantRuntimeConfigFromCLIImageTag(t *testing.T) {
	for _, tt := range []struct {
		name    string
		envTag  string
		values  map[string]string
		want    string
		wantErr bool
	}{
		{name: "invalid colon", envTag: "pr:123", wantErr: true},
		{name: "invalid digest", envTag: "sha256@abc", wantErr: true},
		{name: "invalid whitespace", envTag: "pr 123", wantErr: true},
		{name: "invalid leading dash", envTag: "-pr", wantErr: true},
		{name: "too long", envTag: strings.Repeat("a", 129), wantErr: true},
		{name: "maximum length", envTag: strings.Repeat("a", 128), want: strings.Repeat("a", 128)},
		{name: "valid punctuation", envTag: "_Preview.123-abc", want: "_Preview.123-abc"},
		{name: "default", want: AssistantRuntimeImageHash},
		{name: "environment override", envTag: "pr-123", want: "pr-123"},
		{name: "flag override", values: map[string]string{"assistant-runtime-image-tag": "pr-456"}, want: "pr-456"},
		{name: "flag takes precedence", envTag: "pr-123", values: map[string]string{"assistant-runtime-image-tag": "pr-456"}, want: "pr-456"},
		{name: "empty flag falls back", envTag: "pr-123", values: map[string]string{"assistant-runtime-image-tag": ""}, want: AssistantRuntimeImageHash},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("GRAM_ASSISTANT_RUNTIME_IMAGE_TAG", tt.envTag)
			ctx := newAssistantRuntimeCLIContext(t, tt.values)
			cfg, err := assistantRuntimeConfigFromCLI(ctx, &url.URL{Scheme: "https", Host: "runtime.example.com"})
			if tt.wantErr {
				require.ErrorContains(t, err, "invalid assistant runtime image tag")
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, cfg.GKE.ImageTag)
			require.Equal(t, tt.want, cfg.Local.ImageTag)
		})
	}
}

func newAssistantRuntimeCLIContext(t *testing.T, values map[string]string) *cli.Context {
	t.Helper()

	set := flag.NewFlagSet("test", flag.ContinueOnError)
	require.NoError(t, (&cli.StringFlag{Name: "server-url"}).Apply(set))
	require.NoError(t, (&cli.StringFlag{Name: "environment"}).Apply(set))
	for _, item := range functionsFlags() {
		require.NoError(t, item.Apply(set))
	}
	for _, item := range assistantRuntimeFlags() {
		require.NoError(t, item.Apply(set))
	}
	for key, value := range values {
		require.NoError(t, set.Set(key, value))
	}

	return cli.NewContext(cli.NewApp(), set, nil)
}
