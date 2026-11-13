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
	require.False(t, names["assistant-kit-gke-iam-project"])
	require.False(t, names["assistant-kit-gke-workload-pool-project"])
	for _, name := range []string{"project-number", "organization-id", "cluster-location", "cluster-name"} {
		require.True(t, names["assistant-kit-gke-"+name])
	}
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
			values := map[string]string{"assistant-kit-gke-ingress-cidr": "10.1.0.0/16"}
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
	c = kitCLIContext(t, map[string]string{"assistant-kit-image": "example.invalid/kit:v1", "assistant-runtime-provider": "gke", "assistant-kit-gke-project-number": "123456789012", "assistant-kit-gke-organization-id": "987654321098", "assistant-kit-gke-cluster-location": "us-central1", "assistant-kit-gke-cluster-name": "assistants"})
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

func TestKitGKEIgnoresExternalIssuerOptions(t *testing.T) {
	t.Parallel()
	values := kitGKEIdentityCLIValues()
	values["assistant-kit-image"] = "example.invalid/kit:v0.2.19"
	values["assistant-runtime-provider"] = "gke"
	values["assistant-kit-workload-issuer"] = "https://issuer.example.invalid"
	values["assistant-kit-workload-jwks-url"] = "https://issuer.example.invalid/jwks"
	require.ErrorContains(t, installKitLocalRuntimeFromCLI(kitCLIContext(t, values), nil, nil, nil, nil, nil), "ingress-cidr")
}

func kitGKEIdentityCLIValues() map[string]string {
	return map[string]string{
		"assistant-kit-gke-project-number":   "123456789012",
		"assistant-kit-gke-organization-id":  "987654321098",
		"assistant-kit-gke-cluster-location": "us-central1",
		"assistant-kit-gke-cluster-name":     "assistants",
	}
}

func TestKitGKEIdentityCLI(t *testing.T) {
	t.Parallel()
	got, err := kitGKEIdentityFromCLI(kitCLIContext(t, kitGKEIdentityCLIValues()))
	require.NoError(t, err)
	require.Equal(t, "123456789012", got.ProjectNumber)
	require.Equal(t, "987654321098", got.OrganizationID)
	require.Equal(t, "us-central1", got.ClusterLocation)
	require.Equal(t, "assistants", got.ClusterName)
}

func TestKitGKEIdentityCLIRejectsInvalidIdentity(t *testing.T) {
	t.Parallel()
	for name := range kitGKEIdentityCLIValues() {
		for _, value := range []string{"", "bad/value", " value", "https://example.invalid"} {
			t.Run(name+"/"+value, func(t *testing.T) {
				t.Parallel()
				values := kitGKEIdentityCLIValues()
				values[name] = value
				values["assistant-kit-image"] = "example.invalid/kit:v1"
				values["assistant-runtime-provider"] = "gke"
				require.ErrorContains(t, installKitLocalRuntimeFromCLI(kitCLIContext(t, values), nil, nil, nil, nil, nil), "identity flags")
			})
		}
	}
	for _, name := range []string{"project-number", "organization-id"} {
		for _, value := range []string{"sandbox-project", "0", "-123", "1.5"} {
			values := kitGKEIdentityCLIValues()
			values["assistant-kit-gke-"+name] = value
			_, err := kitGKEIdentityFromCLI(kitCLIContext(t, values))
			require.Error(t, err, "%s=%s", name, value)
		}
	}
}

//nolint:paralleltest // t.Setenv changes process-wide state and prohibits parallel execution.
func TestKitGKEIdentityEnvironment(t *testing.T) {
	values := kitGKEIdentityCLIValues()
	for name, value := range values {
		t.Setenv("GRAM_"+strings.ToUpper(strings.ReplaceAll(name, "-", "_")), value)
	}
	set := flag.NewFlagSet("kit", flag.ContinueOnError)
	for _, f := range assistantKitRuntimeFlags() {
		if _, ok := values[f.Names()[0]]; ok {
			require.NoError(t, f.Apply(set))
		}
	}
	got, err := kitGKEIdentityFromCLI(cli.NewContext(nil, set, nil))
	require.NoError(t, err)
	require.Equal(t, "123456789012", got.ProjectNumber)
	require.Equal(t, "987654321098", got.OrganizationID)
	require.Equal(t, "us-central1", got.ClusterLocation)
	require.Equal(t, "assistants", got.ClusterName)
}
