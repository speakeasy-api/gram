package gram

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPlatformMCPLocalFixtureConfigFromCLI(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		environment string
		enabled     bool
		serverURL   string
		wantOrigin  string
		wantErr     string
	}{
		{
			name:        "disabled remains fail closed",
			environment: "local",
			enabled:     false,
			serverURL:   "https://localhost:8080",
		},
		{
			name:        "enabled local HTTPS origin",
			environment: "local",
			enabled:     true,
			serverURL:   "https://localhost:8080",
			wantOrigin:  "https://localhost:8080",
		},
		{
			name:        "rejects non local environment",
			environment: "dev",
			enabled:     true,
			serverURL:   "https://localhost:8080",
			wantErr:     "only supported when environment is local",
		},
		{
			name:        "rejects HTTP origin",
			environment: "local",
			enabled:     true,
			serverURL:   "http://localhost:8080",
			wantErr:     "requires an HTTPS server origin",
		},
		{
			name:        "rejects credentialed origin",
			environment: "local",
			enabled:     true,
			serverURL:   "https://user@localhost:8080",
			wantErr:     "requires an HTTPS server origin",
		},
		{
			name:        "rejects origin path",
			environment: "local",
			enabled:     true,
			serverURL:   "https://localhost:8080/gram",
			wantErr:     "requires an HTTPS server origin",
		},
		{
			name:        "rejects query and fragment",
			environment: "local",
			enabled:     true,
			serverURL:   "https://localhost:8080/?fixture=1#fragment",
			wantErr:     "requires an HTTPS server origin",
		},
		{
			name:        "rejects hostless or bare-query origin",
			environment: "local",
			enabled:     true,
			serverURL:   "https://:443",
			wantErr:     "requires an HTTPS server origin",
		},
		{
			name:        "rejects bare-query origin",
			environment: "local",
			enabled:     true,
			serverURL:   "https://localhost:8080?",
			wantErr:     "requires an HTTPS server origin",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			config, err := platformMCPLocalFixtureConfigFromCLI(test.environment, test.enabled, test.serverURL)
			if test.wantErr != "" {
				require.ErrorContains(t, err, test.wantErr)
				require.Nil(t, config)
				return
			}

			require.NoError(t, err)
			if test.wantOrigin == "" {
				require.Nil(t, config)
				return
			}
			require.NotNil(t, config)
			require.Equal(t, test.wantOrigin, config.Origin.String())
			require.NotNil(t, config.Fixture)
			require.Equal(t, test.wantOrigin, config.Fixture.Registry().URL)
			require.Equal(t, test.wantOrigin+"/platform-mcp/local-fixture/mcp", config.Fixture.RemoteURL())
		})
	}
}
