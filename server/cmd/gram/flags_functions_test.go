package gram

import (
	"bytes"
	"flag"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"

	"github.com/speakeasy-api/gram/server/internal/functions"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// newFunctionsCLIContext builds a CLI context with the real Functions flags,
// their defaults included, plus the server URL the orchestrator reads. The
// flags are registered without their environment variables, so the developer's
// GRAM_FUNCTIONS_* settings cannot leak into a test.
func newFunctionsCLIContext(t *testing.T, values map[string]string) *cli.Context {
	t.Helper()

	set := flag.NewFlagSet("functions", flag.ContinueOnError)
	for _, f := range functionsFlags() {
		stringFlag, ok := f.(*cli.StringFlag)
		require.True(t, ok, "functions flag %v is not a string flag", f.Names())
		set.String(stringFlag.Name, stringFlag.Value, "")
	}
	set.String("server-url", "https://speakeasy.example", "")
	for name, value := range values {
		require.NoError(t, set.Set(name, value))
	}

	return cli.NewContext(cli.NewApp(), set, nil)
}

func TestFunctionsProviderDisabledNeedsNoProviderSettings(t *testing.T) {
	t.Parallel()

	c := newFunctionsCLIContext(t, map[string]string{"functions-provider": functions.ProviderDisabled})
	logger := testenv.NewLogger(t)

	store, shutdown, err := newTigrisStore(t.Context(), c, logger)
	require.NoError(t, err)
	require.Nil(t, store)
	require.NoError(t, shutdown(t.Context()))

	orchestrator, shutdown, err := newFunctionOrchestrator(c, logger, testenv.NewTracerProvider(t), nil, nil, nil, store, nil)
	require.NoError(t, err)
	require.Equal(t, functions.Disabled{}, orchestrator)
	require.NoError(t, shutdown(t.Context()))
}

func TestFunctionsProviderDisabledIgnoresProviderSettingsWithoutLoggingValues(t *testing.T) {
	t.Parallel()

	const secret = "tigris-secret-value"
	c := newFunctionsCLIContext(t, map[string]string{
		"functions-provider":          functions.ProviderDisabled,
		"functions-flyio-api-token":   "fly-token-value",
		"functions-flyio-org":         "fly-org",
		"functions-tigris-bucket-uri": "s3://bucket",
		"functions-tigris-key":        "tigris-key-value",
		"functions-tigris-secret":     secret,
		"functions-runner-oci-image":  "gram-runner",
	})
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))

	store, _, err := newTigrisStore(t.Context(), c, logger)
	require.NoError(t, err)
	require.Nil(t, store)

	orchestrator, _, err := newFunctionOrchestrator(c, logger, testenv.NewTracerProvider(t), nil, nil, nil, store, nil)
	require.NoError(t, err)
	require.Equal(t, functions.Disabled{}, orchestrator)

	out := logs.String()
	for _, envVar := range []string{
		"GRAM_FUNCTIONS_FLYIO_API_TOKEN",
		"GRAM_FUNCTIONS_FLYIO_ORG",
		"GRAM_FUNCTIONS_TIGRIS_BUCKET_URI",
		"GRAM_FUNCTIONS_TIGRIS_KEY",
		"GRAM_FUNCTIONS_TIGRIS_SECRET",
	} {
		require.Contains(t, out, envVar)
	}
	// The runner image is shared by every provider and set by default, and
	// the Fly.io region has a default, so neither is reported.
	require.NotContains(t, out, "GRAM_FUNCTIONS_RUNNER_OCI_IMAGE")
	require.NotContains(t, out, "GRAM_FUNCTIONS_FLYIO_REGION")
	for _, value := range []string{"fly-token-value", "fly-org", "s3://bucket", "tigris-key-value", secret} {
		require.NotContains(t, out, value)
	}
}

func TestFunctionsProviderLocalBuildsLocalRunner(t *testing.T) {
	t.Parallel()

	c := newFunctionsCLIContext(t, map[string]string{
		"functions-provider":          functions.ProviderLocal,
		"functions-local-runner-root": t.TempDir(),
	})
	logger := testenv.NewLogger(t)

	store, shutdown, err := newTigrisStore(t.Context(), c, logger)
	require.NoError(t, err)
	require.NotNil(t, store)
	t.Cleanup(func() { require.NoError(t, shutdown(t.Context())) })

	orchestrator, _, err := newFunctionOrchestrator(c, logger, testenv.NewTracerProvider(t), nil, nil, nil, store, nil)
	require.NoError(t, err)
	require.IsType(t, &functions.LocalRunner{}, orchestrator)
}

func TestFunctionsProviderFlyIORequiresProviderSettings(t *testing.T) {
	t.Parallel()

	c := newFunctionsCLIContext(t, map[string]string{"functions-provider": functions.ProviderFlyIO})
	logger := testenv.NewLogger(t)

	_, _, err := newTigrisStore(t.Context(), c, logger)
	require.ErrorContains(t, err, "invalid configuration for tigris")

	_, _, err = newFunctionOrchestrator(c, logger, testenv.NewTracerProvider(t), nil, nil, nil, nil, nil)
	require.ErrorContains(t, err, "invalid configuration for functions")
}

func TestFunctionsProviderRejectsUnsetAndUnknownValues(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		provider string
	}{
		{name: "unset", provider: ""},
		{name: "unknown", provider: "lambda"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			c := newFunctionsCLIContext(t, map[string]string{"functions-provider": tc.provider})
			logger := testenv.NewLogger(t)

			_, _, err := newTigrisStore(t.Context(), c, logger)
			require.ErrorContains(t, err, "unrecognized functions provider")

			_, _, err = newFunctionOrchestrator(c, logger, testenv.NewTracerProvider(t), nil, nil, nil, nil, nil)
			require.ErrorContains(t, err, "unrecognized functions provider")
		})
	}
}
