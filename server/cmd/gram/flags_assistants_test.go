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
		"assistant-runtime-server-url": "https://runtime.example.com",
	})

	cfg, err := assistantRuntimeConfigFromCLI(ctx, &url.URL{Scheme: "https", Host: "gram.example.com"})
	require.NoError(t, err)
	require.Equal(t, "https://runtime.example.com", cfg.Fly.ServerURL.String())
}

func TestAssistantRuntimeConfigFromCLIFallsBackToServerURL(t *testing.T) {
	t.Parallel()

	ctx := newAssistantRuntimeCLIContext(t, map[string]string{})

	serverURL := &url.URL{Scheme: "https", Host: "gram.example.com"}
	cfg, err := assistantRuntimeConfigFromCLI(ctx, serverURL)
	require.NoError(t, err)
	require.Equal(t, serverURL, cfg.Fly.ServerURL)
}

func newAssistantRuntimeCLIContext(t *testing.T, values map[string]string) *cli.Context {
	t.Helper()

	set := flag.NewFlagSet("test", flag.ContinueOnError)
	require.NoError(t, (&cli.StringFlag{Name: "server-url"}).Apply(set))
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
