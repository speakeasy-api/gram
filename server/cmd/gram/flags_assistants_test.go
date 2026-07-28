package gram

import (
	"flag"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"

	"github.com/speakeasy-api/gram/server/internal/assistants"
)

func TestAssistantRuntimeConfigFromCLIAcceptsFlyProvider(t *testing.T) {
	t.Parallel()

	ctx := newAssistantRuntimeCLIContext(t, map[string]string{
		"assistant-runtime-provider": "flyio",
	})

	serverURL := &url.URL{Scheme: "https", Host: "gram.example.com"}
	cfg, err := assistantRuntimeConfigFromCLI(ctx, serverURL)
	require.NoError(t, err)
	require.Equal(t, assistants.RuntimeProviderFlyIO, cfg.Provider)
	require.Equal(t, serverURL, cfg.Fly.ServerURL)
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
		"assistant-runtime-provider":   assistants.RuntimeProviderFlyIO,
		"assistant-runtime-server-url": "https://runtime.example.com",
	})

	cfg, err := assistantRuntimeConfigFromCLI(ctx, &url.URL{Scheme: "https", Host: "gram.example.com"})
	require.NoError(t, err)
	require.Equal(t, "https://runtime.example.com", cfg.Fly.ServerURL.String())
}

func TestAssistantRuntimeConfigFromCLIFallsBackToServerURL(t *testing.T) {
	t.Parallel()

	ctx := newAssistantRuntimeCLIContext(t, map[string]string{
		"assistant-runtime-provider": assistants.RuntimeProviderFlyIO,
	})

	serverURL := &url.URL{Scheme: "https", Host: "gram.example.com"}
	cfg, err := assistantRuntimeConfigFromCLI(ctx, serverURL)
	require.NoError(t, err)
	require.Equal(t, serverURL, cfg.Fly.ServerURL)
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
		"assistant-runtime-provider": assistants.RuntimeProviderFlyIO,
		"environment":                "local",
	})

	cfg, err := assistantRuntimeConfigFromCLI(ctx, &url.URL{Scheme: "https", Host: "localhost:8080"})
	require.NoError(t, err)
	require.Equal(t, assistants.RuntimeProviderFlyIO, cfg.Provider)
	// Locally created rows stay routable for cleanup even when targeting
	// another backend.
	require.True(t, cfg.Local.Enabled)
}

func newAssistantRuntimeCLIContext(t *testing.T, values map[string]string) *cli.Context {
	t.Helper()

	set := flag.NewFlagSet("test", flag.ContinueOnError)
	require.NoError(t, (&cli.StringFlag{Name: "server-url"}).Apply(set))
	require.NoError(t, (&cli.StringFlag{Name: "environment"}).Apply(set))
	for _, item := range functionsFlags {
		require.NoError(t, item.Apply(set))
	}
	for _, item := range assistantRuntimeFlags {
		require.NoError(t, item.Apply(set))
	}
	for key, value := range values {
		require.NoError(t, set.Set(key, value))
	}

	return cli.NewContext(cli.NewApp(), set, nil)
}
