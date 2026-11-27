package gram

import (
	"flag"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"

	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestKitWorkloadAssertionFlagsAndRemovedGKEFlags(t *testing.T) {
	t.Parallel()
	names := map[string]bool{}
	for _, f := range assistantKitRuntimeFlags() {
		for _, name := range f.Names() {
			names[name] = true
		}
	}
	require.True(t, names["assistant-kit-workload-issuer"])
	require.True(t, names["assistant-kit-workload-jwks-url"])
	require.True(t, names["assistant-kit-workload-subject"])
	require.True(t, names["assistant-kit-workload-assertion-file"])
	require.True(t, names["assistant-kit-network"])
	require.True(t, names["assistant-kit-image"])
	require.True(t, names["assistant-ate-enabled"])

	// All removed Kit GKE flags must be absent so no GKE Kit path remains.
	for _, removed := range []string{
		"assistant-kit-gke-project-number",
		"assistant-kit-gke-organization-id",
		"assistant-kit-gke-cluster-location",
		"assistant-kit-gke-cluster-name",
		"assistant-kit-gke-ingress-cidr",
		"assistant-kit-gke-cpu-request",
		"assistant-kit-gke-cpu-limit",
		"assistant-kit-gke-memory-request",
		"assistant-kit-gke-memory-limit",
		"assistant-kit-gke-work-size-limit",
		"assistant-kit-gke-tmp-size-limit",
		"assistant-kit-gke-snapshots-enabled",
		"assistant-kit-gke-iam-project",
		"assistant-kit-gke-workload-pool-project",
		"assistant-kit-mcp-auth-method",
	} {
		require.False(t, names[removed], "removed flag %s must not be registered", removed)
	}
}

func kitCLIContext(t *testing.T, values map[string]string) *cli.Context {
	t.Helper()
	set := flag.NewFlagSet("kit", flag.ContinueOnError)
	for _, f := range assistantRuntimeFlags() {
		// Apply mutates flag.Value when an environment override is present.
		var defaultValue string
		switch value := f.(type) {
		case *cli.StringFlag:
			defaultValue = value.Value
		case *cli.BoolFlag:
			defaultValue = strconv.FormatBool(value.Value)
		}
		require.NoError(t, f.Apply(set))
		switch f.(type) {
		case *cli.StringFlag, *cli.BoolFlag:
			require.NoError(t, set.Set(f.Names()[0], defaultValue))
		}
	}
	set.String("environment", "production", "")
	for name, value := range values {
		require.NoError(t, set.Set(name, value))
	}
	return cli.NewContext(nil, set, nil)
}

func TestKitCLIOptInAndLocalOnlyEnforcement(t *testing.T) {
	t.Parallel()
	// Unset image preserves legacy composition without needing service dependencies.
	require.NoError(t, installKitLocalRuntimeFromCLI(testenv.NewLogger(t), kitCLIContext(t, nil), nil, nil, nil, nil, nil, nil))

	// Local Kit is impossible to enable in non-local environments or with non-local providers.
	for _, tc := range []struct {
		name   string
		values map[string]string
	}{
		{
			name:   "local provider in production environment",
			values: map[string]string{"assistant-kit-image": "example.invalid/kit:v1", "assistant-runtime-provider": "local", "environment": "production"},
		},
		{
			name:   "local provider in staging environment",
			values: map[string]string{"assistant-kit-image": "example.invalid/kit:v1", "assistant-runtime-provider": "local", "environment": "staging"},
		},
		{
			name:   "gke provider in production environment",
			values: map[string]string{"assistant-kit-image": "example.invalid/kit:v1", "assistant-runtime-provider": "gke", "environment": "production"},
		},
		{
			name:   "gke provider even in local environment",
			values: map[string]string{"assistant-kit-image": "example.invalid/kit:v1", "assistant-runtime-provider": "gke", "environment": "local"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := kitCLIContext(t, tc.values)
			require.ErrorContains(t, installKitLocalRuntimeFromCLI(testenv.NewLogger(t), c, nil, nil, nil, nil, nil, nil), "local Kit requires --assistant-runtime-provider=local and --environment=local")
		})
	}
}

func TestKitCLIIgnoredWhenRuntimeDisabled(t *testing.T) {
	t.Parallel()
	c := kitCLIContext(t, map[string]string{"assistant-kit-image": "example.invalid/kit:v1", "assistant-runtime-provider": "disabled", "assistant-kit-workload-subject": "local-subject"})
	require.NoError(t, installKitLocalRuntimeFromCLI(testenv.NewLogger(t), c, nil, nil, nil, nil, nil, nil))
}

func TestPrivateIngressRegistersAssistantRuntimeFlags(t *testing.T) {
	t.Parallel()
	flags := map[string]bool{}
	for _, f := range privateIngressServerFlags() {
		for _, name := range f.Names() {
			require.False(t, flags[name], "duplicate flag %s", name)
			flags[name] = true
		}
	}
	for _, f := range assistantRuntimeFlags() {
		require.True(t, flags[f.Names()[0]], "missing flag %s", f.Names()[0])
	}
	require.False(t, flags["assistant-kit-allow-insecure-inference"], "Kit requires its fixed HTTPS provider origin")
	for _, removed := range []string{"assistant-kit-mcp-resource", "assistant-kit-mcp-issuer", "assistant-kit-mcp-issuer-id"} {
		require.False(t, flags[removed], "legacy per-resource flag %s must not be registered", removed)
	}
}
