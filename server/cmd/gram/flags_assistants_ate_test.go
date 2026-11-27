package gram

import (
	"net/url"
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/assistants"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestATEEgressHostsAreExplicit(t *testing.T) {
	t.Parallel()
	c := kitCLIContext(t, map[string]string{"assistant-ate-egress-host": "tools.example.com"})
	require.Equal(t, []string{"tools.example.com"}, c.StringSlice("assistant-ate-egress-host"))
}

func TestATEOnlyRuntimeConstructionInAnyEnvironment(t *testing.T) {
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
		{
			name:   "ATE enabled in production without GKE cluster uses unavailable primary",
			values: map[string]string{"environment": "production", "assistant-ate-enabled": "true", "assistant-runtime-image-tag": "test"},
		},
		{
			name:   "ATE enabled in dev without GKE cluster uses unavailable primary",
			values: map[string]string{"environment": "dev", "assistant-ate-enabled": "true", "assistant-runtime-image-tag": "test"},
		},
		{
			name:      "default GKE provider without ATE still requires a backend",
			values:    map[string]string{"environment": "production", "assistant-runtime-image-tag": "test"},
			wantError: "requires at least one backend",
		},
		{
			name:      "configured GKE cluster is preserved alongside ATE",
			values:    map[string]string{"environment": "production", "assistant-ate-enabled": "true", "assistant-runtime-image-tag": "test", "assistant-runtime-gke-cluster-endpoint": "https://cluster.example.com", "assistant-runtime-gke-cluster-ca": "not-base64"},
			wantError: "decode --assistant-runtime-gke-cluster-ca",
		},
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

func TestATEStartupValidationAndDisabledProvider(t *testing.T) {
	t.Parallel()
	serverURL, err := url.Parse("https://api.example.com")
	require.NoError(t, err)

	t.Run("disabled ATE preserves default path", func(t *testing.T) {
		t.Parallel()
		err := installKitLocalRuntimeFromCLI(testenv.NewLogger(t), kitCLIContext(t, nil), nil, nil, nil, nil, serverURL)
		require.NoError(t, err)
	})

	t.Run("disabled runtime provider ignores ATE settings", func(t *testing.T) {
		t.Parallel()
		c := kitCLIContext(t, map[string]string{
			"environment":                "production",
			"assistant-runtime-provider": assistants.RuntimeProviderDisabled,
			"assistant-ate-enabled":      "true",
		})
		err := installKitLocalRuntimeFromCLI(testenv.NewLogger(t), c, nil, nil, nil, nil, serverURL)
		require.NoError(t, err)
	})

	t.Run("enabled ATE validates required flags at startup via inv.Require", func(t *testing.T) {
		t.Parallel()
		c := kitCLIContext(t, map[string]string{
			"environment":           "production",
			"assistant-ate-enabled": "true",
		})
		require.Panics(t, func() {
			_ = installKitLocalRuntimeFromCLI(testenv.NewLogger(t), c, nil, nil, nil, nil, serverURL)
		})
	})

	t.Run("enabled ATE rejects missing assistant-ate-image even when assistant-kit-image is set", func(t *testing.T) {
		t.Parallel()
		c := kitCLIContext(t, map[string]string{
			"environment":                     "production",
			"assistant-ate-enabled":           "true",
			"assistant-kit-image":             "ghcr.io/speakeasy-api/kit@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			"assistant-ate-worker-selector":   "workload-type=ate-assistants",
			"assistant-kit-workload-issuer":   "https://ate.example.com",
			"assistant-kit-workload-jwks-url": "https://ate.example.com/.well-known/jwks.json",
			"assistant-ate-api-address":       "127.0.0.1:1",
			"assistant-ate-router-address":    "127.0.0.1:2",
			"assistant-ate-service-ca-file":   "/nonexistent/ca.pem",
			"assistant-ate-client-cert-file":  "/nonexistent/cert.pem",
			"assistant-ate-client-key-file":   "/nonexistent/key.pem",
			"assistant-ate-audience-jwt-file": "/nonexistent/token",
			"assistant-ate-atespace":          "prod-assistants",
			"assistant-ate-template":          "kit-v1",
		})
		require.Panics(t, func() {
			_ = installKitLocalRuntimeFromCLI(testenv.NewLogger(t), c, &assistants.ServiceCore{}, nil, nil, redis.NewClient(&redis.Options{Addr: "127.0.0.1:0"}), serverURL)
		})
	})

	t.Run("enabled ATE rejects missing or invalid assistant-ate-worker-selector", func(t *testing.T) {
		t.Parallel()
		for _, selector := range []string{"", "not-a-selector", "a..b/key=val"} {
			flags := map[string]string{
				"environment":                     "production",
				"assistant-ate-enabled":           "true",
				"assistant-ate-image":             "gcr.io/example/gram-ate-runtime:sha-deadbeef@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
				"assistant-kit-workload-issuer":   "https://ate.example.com",
				"assistant-kit-workload-jwks-url": "https://ate.example.com/.well-known/jwks.json",
				"assistant-ate-api-address":       "127.0.0.1:1",
				"assistant-ate-router-address":    "127.0.0.1:2",
				"assistant-ate-service-ca-file":   "/nonexistent/ca.pem",
				"assistant-ate-client-cert-file":  "/nonexistent/cert.pem",
				"assistant-ate-client-key-file":   "/nonexistent/key.pem",
				"assistant-ate-audience-jwt-file": "/nonexistent/token",
				"assistant-ate-atespace":          "prod-assistants",
				"assistant-ate-template":          "kit-v1",
			}
			if selector != "" {
				flags["assistant-ate-worker-selector"] = selector
			}
			c := kitCLIContext(t, flags)
			require.Panics(t, func() {
				_ = installKitLocalRuntimeFromCLI(testenv.NewLogger(t), c, &assistants.ServiceCore{}, nil, nil, redis.NewClient(&redis.Options{Addr: "127.0.0.1:0"}), serverURL)
			}, selector)
		}
	})

	t.Run("enabled ATE in production passes composition root config check when all flags set without assistant-kit-image", func(t *testing.T) {
		t.Parallel()
		c := kitCLIContext(t, map[string]string{
			"environment":                     "production",
			"assistant-ate-enabled":           "true",
			"assistant-ate-image":             "gcr.io/example/gram-ate-runtime:sha-0123456789abcdef0123456789abcdef01234567@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			"assistant-ate-worker-selector":   "workload-type=ate-assistants,tier=prod",
			"assistant-kit-workload-issuer":   "https://ate.example.com",
			"assistant-kit-workload-jwks-url": "https://ate.example.com/.well-known/jwks.json",
			"assistant-ate-api-address":       "127.0.0.1:1",
			"assistant-ate-router-address":    "127.0.0.1:2",
			"assistant-ate-service-ca-file":   "/nonexistent/ca.pem",
			"assistant-ate-client-cert-file":  "/nonexistent/cert.pem",
			"assistant-ate-client-key-file":   "/nonexistent/key.pem",
			"assistant-ate-audience-jwt-file": "/nonexistent/token",
			"assistant-ate-atespace":          "prod-assistants",
			"assistant-ate-template":          "kit-v1",
		})
		// Reaches ServiceCore.InstallKitATERuntime (does not panic on inv.Require
		// and does not fail on any environment == "dev" gate).
		err := installKitLocalRuntimeFromCLI(testenv.NewLogger(t), c, &assistants.ServiceCore{}, nil, nil, redis.NewClient(&redis.Options{Addr: "127.0.0.1:0"}), serverURL)
		require.ErrorContains(t, err, "ATE requires guarded HTTP")
	})
}

func TestATEFlagsHaveNoEnablementDefaults(t *testing.T) {
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
