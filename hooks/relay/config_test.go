package relay

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadConfigServerURL(t *testing.T) { //nolint:paralleltest // sets environment variables
	tests := []struct {
		name, configured, currentEnv, legacyEnv, want string
	}{
		{name: "canonical default", want: "https://ai.speakeasy.com"},
		{name: "configured override", configured: "http://localhost:8080/api", want: "http://localhost:8080/api"},
		{name: "environment override", configured: "https://ai.speakeasy.com", currentEnv: "http://localhost:9090/api", want: "http://localhost:9090/api"},
		{name: "legacy environment override", legacyEnv: "http://localhost:9091/api", want: "http://localhost:9091/api"},
		{name: "current environment wins", currentEnv: "http://localhost:9092/api", legacyEnv: "http://localhost:9091/api", want: "http://localhost:9092/api"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("SPEAKEASY_AI_HOOKS_SERVER_URL", tt.currentEnv)
			t.Setenv("GRAM_HOOKS_SERVER_URL", tt.legacyEnv)
			t.Setenv("SPEAKEASY_AI_HOOKS_SITE_URL", "http://localhost:5173")
			got := LoadConfig(Config{ServerURL: tt.configured})
			require.Equal(t, tt.want, got.ServerURL)
			require.Equal(t, "http://localhost:5173", got.SiteURL)
		})
	}
}
