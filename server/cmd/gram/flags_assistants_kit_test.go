package gram

import (
	"flag"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
)

func TestKitWorkloadAssertionFlags(t *testing.T) {
	t.Parallel()
	names := map[string]bool{}
	for _, f := range assistantKitRuntimeFlags() {
		for _, name := range f.Names() {
			names[name] = true
		}
	}
	require.True(t, names["assistant-kit-workload-issuer"])
	require.True(t, names["assistant-kit-gke-iam-project"])
	require.True(t, names["assistant-kit-gke-metadata-image"])
	require.True(t, names["assistant-kit-workload-jwks-url"])
	require.True(t, names["assistant-kit-workload-subject"])
	require.True(t, names["assistant-kit-workload-assertion-file"])
	require.False(t, names["assistant-kit-mcp-auth-method"])
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

func TestKitGKEProvisionCLI(t *testing.T) {
	t.Parallel()
	c := kitCLIContext(t, map[string]string{
		"assistant-kit-gke-ingress-cidr":      "10.1.0.0/16",
		"assistant-kit-gke-iam-project":       "sandbox-project",
		"assistant-kit-gke-metadata-image":    "example.invalid/runtime@sha256:" + strings.Repeat("a", 64),
		"assistant-kit-gke-snapshots-enabled": "true",
		"assistant-kit-gke-cpu-request":       "200m",
		"assistant-kit-gke-work-size-limit":   "2Gi",
	})
	got, err := kitGKEProvisionFromCLI(c)
	require.NoError(t, err)
	require.Equal(t, "10.1.0.0/16", got.IngressCIDR)
	require.True(t, got.SnapshotsEnabled)
	require.Equal(t, "2Gi", got.WorkSizeLimit)
	require.Equal(t, "128Mi", got.TmpSizeLimit)
	require.Equal(t, "200m", got.Resources.Requests.Cpu().String())
	require.Equal(t, "1", got.Resources.Limits.Cpu().String())
	require.Equal(t, "128Mi", got.Resources.Requests.Memory().String())
	require.Equal(t, "1Gi", got.Resources.Limits.Memory().String())
	require.Empty(t, got.Image) // Composition owns image, identity and gateway settings.
	require.Empty(t, got.Identity.AssistantID)
	require.Zero(t, got.GuestPort)
}

func TestKitGKEProvisionCLIRejectsUnsafeConfiguration(t *testing.T) {
	t.Parallel()
	for name, value := range map[string]string{
		"assistant-kit-gke-iam-project":     "",
		"assistant-kit-gke-metadata-image":  "example.invalid/runtime:latest",
		"assistant-kit-network":             "local-network",
		"assistant-runtime-local-ca-file":   "/local/ca.pem",
		"assistant-kit-gke-ingress-cidr":    "",
		"assistant-kit-gke-cpu-request":     "2",
		"assistant-kit-gke-cpu-limit":       "0",
		"assistant-kit-gke-memory-request":  "2Gi",
		"assistant-kit-gke-memory-limit":    "-1Gi",
		"assistant-kit-gke-work-size-limit": "invalid",
		"assistant-kit-gke-tmp-size-limit":  "0",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			values := map[string]string{"assistant-kit-gke-ingress-cidr": "10.1.0.0/16", "assistant-kit-gke-iam-project": "sandbox-project", "assistant-kit-gke-metadata-image": "example.invalid/runtime@sha256:" + strings.Repeat("a", 64)}
			values[name] = value
			_, err := kitGKEProvisionFromCLI(kitCLIContext(t, values))
			require.Error(t, err)
		})
	}
}

func TestKitCLIOptInAndProviderValidation(t *testing.T) {
	t.Parallel()
	// Unset image preserves legacy composition without needing service dependencies.
	require.NoError(t, installKitLocalRuntimeFromCLI(kitCLIContext(t, nil), nil, nil, nil, nil, nil))
	c := kitCLIContext(t, map[string]string{"assistant-kit-image": "example.invalid/kit:v1", "assistant-runtime-provider": "local"})
	require.ErrorContains(t, installKitLocalRuntimeFromCLI(c, nil, nil, nil, nil, nil), "local environment")
	c = kitCLIContext(t, map[string]string{"assistant-kit-image": "example.invalid/kit:v1", "assistant-runtime-provider": "gke", "assistant-kit-workload-issuer": "https://container.googleapis.com/v1/projects/cluster-project/locations/us-central1/clusters/assistants", "assistant-kit-gke-iam-project": "identity-project", "assistant-kit-gke-metadata-image": "example.invalid/runtime@sha256:" + strings.Repeat("a", 64)})
	require.ErrorContains(t, installKitLocalRuntimeFromCLI(c, nil, nil, nil, nil, nil), "ingress-cidr")
	c = kitCLIContext(t, map[string]string{"assistant-kit-image": "example.invalid/kit:v1", "assistant-runtime-provider": "gke", "assistant-kit-workload-subject": "local-subject"})
	require.ErrorContains(t, installKitLocalRuntimeFromCLI(c, nil, nil, nil, nil, nil), "local-only")
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
}

func TestKitGKERejectsLocalProjectionOptions(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"assistant-kit-workload-subject", "assistant-kit-workload-assertion-file"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c := kitCLIContext(t, map[string]string{"assistant-kit-image": "example.invalid/kit:v0.2.19", "assistant-runtime-provider": "gke", name: "local-projection-value"})
			require.ErrorContains(t, installKitLocalRuntimeFromCLI(c, nil, nil, nil, nil, nil), name+" is local-only")
		})
	}
}

func TestKitGKEWorkloadPoolProject(t *testing.T) {
	t.Parallel()
	got, err := kitGKEWorkloadPoolProject("https://container.googleapis.com/v1/projects/cluster-project/locations/us-central1/clusters/assistants")
	require.NoError(t, err)
	require.Equal(t, "cluster-project", got)
	for _, raw := range []string{"", "https://accounts.google.com", "http://container.googleapis.com/v1/projects/cluster-project/locations/us-central1/clusters/assistants", "https://container.googleapis.com.evil.invalid/v1/projects/cluster-project/locations/us-central1/clusters/assistants", "https://container.googleapis.com/v1/projects/123456/locations/us-central1/clusters/assistants", "https://container.googleapis.com/v1/projects/cluster-project/locations/us-central1/clusters/assistants?project=other", "https://container.googleapis.com/v1/projects/cluster-project/locations/us-central1/clusters/assistants#ignored"} {
		_, err := kitGKEWorkloadPoolProject(raw)
		require.Error(t, err, raw)
	}
}

func TestKitGKEProvisionCLIRejectsMalformedMetadataImage(t *testing.T) {
	t.Parallel()
	for _, image := range []string{
		"example.invalid/Runtime@sha256:" + strings.Repeat("a", 64),
		"example.invalid/runtime@sha256:abc",
		"example.invalid/runtime:latest",
	} {
		t.Run(image, func(t *testing.T) {
			t.Parallel()
			_, err := kitGKEProvisionFromCLI(kitCLIContext(t, map[string]string{
				"assistant-kit-gke-iam-project":    "sandbox-project",
				"assistant-kit-gke-metadata-image": image,
			}))
			require.ErrorContains(t, err, "assistant-kit-gke-metadata-image")
		})
	}
}
