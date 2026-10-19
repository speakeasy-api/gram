package workflow

import (
	"os"
	"testing"

	"github.com/speakeasy-api/gram/cli/internal/flags"
	"github.com/speakeasy-api/gram/cli/internal/profile"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
)

func TestResolveURLPrecedence(t *testing.T) {
	tests := []struct {
		name, profileURL, envURL, legacyURL, flagURL, want string
	}{
		{name: "default", profileURL: "", envURL: "", legacyURL: "", flagURL: "", want: "https://ai.speakeasy.com"},
		{name: "canonical profile", profileURL: "https://ai.speakeasy.com", envURL: "", legacyURL: "", flagURL: "", want: "https://ai.speakeasy.com"},
		{name: "legacy profile preserved", profileURL: "https://app.getgram.ai", envURL: "", legacyURL: "", flagURL: "", want: "https://app.getgram.ai"},
		{name: "local profile preserved", profileURL: "http://localhost:8080/api", envURL: "", legacyURL: "", flagURL: "", want: "http://localhost:8080/api"},
		{name: "environment beats profile", profileURL: "https://ai.speakeasy.com", envURL: "http://localhost:9090/api", legacyURL: "", flagURL: "", want: "http://localhost:9090/api"},
		{name: "legacy environment beats profile", profileURL: "https://ai.speakeasy.com", envURL: "", legacyURL: "http://localhost:9091/api", flagURL: "", want: "http://localhost:9091/api"},
		{name: "flag beats environment", profileURL: "", envURL: "https://ai.speakeasy.com", legacyURL: "", flagURL: "http://localhost:9092/api", want: "http://localhost:9092/api"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("SPEAKEASY_AI_API_URL", tt.envURL)
			t.Setenv("GRAM_API_URL", tt.legacyURL)
			if tt.envURL == "" {
				require.NoError(t, os.Unsetenv("SPEAKEASY_AI_API_URL"))
			}
			if tt.legacyURL == "" {
				require.NoError(t, os.Unsetenv("GRAM_API_URL"))
			}
			_, err := flags.ApplyLegacyEnv(os.LookupEnv, func(key, value string) error {
				t.Setenv(key, value)
				return nil
			})
			require.NoError(t, err)
			app := &cli.App{
				Flags: []cli.Flag{flags.APIEndpoint()},
				Action: func(c *cli.Context) error {
					require.Equal(t, "https://ai.speakeasy.com", flags.APIEndpoint().Value)
					var prof *profile.Profile
					if tt.profileURL != "" {
						prof = new(profile.Profile)
						prof.APIUrl = tt.profileURL
					}
					got, err := ResolveURL(c, prof)
					require.NoError(t, err)
					require.Equal(t, tt.want, got.String())
					return nil
				},
			}
			args := []string{"test"}
			if tt.flagURL != "" {
				args = append(args, "--api-url", tt.flagURL)
			}
			require.NoError(t, app.Run(args))
		})
	}
}
