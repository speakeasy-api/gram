package remotemcp

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

func TestConfiguredHeadersFromRepo_EnvFillsOptInHeader(t *testing.T) {
	t.Parallel()

	env := toolconfig.CIEnvFrom(map[string]string{
		"X_UPSTREAM_API_KEY": "from-env",
		"UNRELATED_SECRET":   "must-not-leak",
	})

	headers := []remotemcprepo.RemoteMcpServerHeader{
		{
			Name:       "X-Upstream-Api-Key",
			Value:      pgtype.Text{String: "", Valid: true},
			IsRequired: true,
		},
	}

	configured := configuredHeadersFromRepo(headers, env)
	require.Len(t, configured, 1)
	require.Equal(t, "from-env", configured[0].StaticValue)
}

func TestConfiguredHeadersFromRepo_StaticValueWinsOverEnv(t *testing.T) {
	t.Parallel()

	env := toolconfig.CIEnvFrom(map[string]string{
		"X_UPSTREAM_API_KEY": "from-env",
	})

	headers := []remotemcprepo.RemoteMcpServerHeader{
		{
			Name:       "X-Upstream-Api-Key",
			Value:      pgtype.Text{String: "static-secret", Valid: true},
			IsRequired: true,
		},
	}

	configured := configuredHeadersFromRepo(headers, env)
	require.Len(t, configured, 1)
	require.Equal(t, "static-secret", configured[0].StaticValue)
}

func TestConfiguredHeadersFromRepo_RequestHeaderSourceUnchanged(t *testing.T) {
	t.Parallel()

	env := toolconfig.CIEnvFrom(map[string]string{
		"X_UPSTREAM_API_KEY": "from-env",
	})

	headers := []remotemcprepo.RemoteMcpServerHeader{
		{
			Name:                   "X-Upstream-Api-Key",
			Value:                  pgtype.Text{Valid: false},
			ValueFromRequestHeader: pgtype.Text{String: "X-Inbound", Valid: true},
			IsRequired:             true,
		},
	}

	configured := configuredHeadersFromRepo(headers, env)
	require.Len(t, configured, 1)
	require.Empty(t, configured[0].StaticValue)
	require.Equal(t, "X-Inbound", configured[0].ValueFromRequestHeader)
}

func TestConfiguredHeadersFromRepo_OptInWithoutEnvLeavesEmpty(t *testing.T) {
	t.Parallel()

	headers := []remotemcprepo.RemoteMcpServerHeader{
		{
			Name:       "X-Upstream-Api-Key",
			Value:      pgtype.Text{String: "", Valid: true},
			IsRequired: true,
		},
	}

	configured := configuredHeadersFromRepo(headers, nil)
	require.Len(t, configured, 1)
	require.Empty(t, configured[0].StaticValue)
}
