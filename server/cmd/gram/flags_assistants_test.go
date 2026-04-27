package gram

import (
	"flag"
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

	cfg, err := assistantRuntimeConfigFromCLI(ctx)
	require.NoError(t, err)
	require.Equal(t, assistants.RuntimeProviderFlyIO, cfg.Provider)
}

func TestAssistantRuntimeConfigFromCLIRejectsUnknownProvider(t *testing.T) {
	t.Parallel()

	ctx := newAssistantRuntimeCLIContext(t, map[string]string{})
	require.NoError(t, ctx.Set("assistant-runtime-provider", "bogus"))

	cfg, err := assistantRuntimeConfigFromCLI(ctx)
	require.ErrorContains(t, err, "invalid assistant runtime provider: bogus")
	require.Empty(t, cfg.Provider)
}

func TestAssistantRuntimeConfigFromCLIDefaultsToLocalProvider(t *testing.T) {
	t.Parallel()

	ctx := newAssistantRuntimeCLIContext(t, map[string]string{})

	cfg, err := assistantRuntimeConfigFromCLI(ctx)
	require.NoError(t, err)
	require.Equal(t, assistants.RuntimeProviderLocal, cfg.Provider)
}

func TestAssistantRuntimeConfigFromCLIPreviewDoesNotSelectFly(t *testing.T) {
	t.Parallel()

	ctx := newAssistantRuntimeCLIContext(t, map[string]string{
		"server-url":                 "https://pr-123.dev.getgram.ai",
		"functions-provider":         "flyio",
		"functions-runner-oci-image": "registry.fly.io/gfr-dev-dca1j103",
		"functions-flyio-api-token":  "FlyV1 test-token",
		"functions-flyio-org":        "speakeasy-lab",
		"functions-flyio-region":     "ord",
		"functions-runner-version":   "pr-123-deadbeef",
	})

	cfg, err := assistantRuntimeConfigFromCLI(ctx)
	require.NoError(t, err)
	require.Equal(t, assistants.RuntimeProviderLocal, cfg.Provider)
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
