package main

import (
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDirectoryRolesOptions(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		args  []string
		valid bool
		want  commandOptions
	}{
		{"import preview", []string{"import"}, true, commandOptions{}},
		{"import apply", []string{"import", "-apply", "-confirm-organization", "example-org"}, true, commandOptions{apply: true, confirmOrganization: "example-org"}},
		{"shadow", []string{"shadow"}, true, commandOptions{shadow: true}},
		{"shadow apply", []string{"shadow", "-apply"}, false, commandOptions{}},
		{"shadow confirmation", []string{"shadow", "-confirm-organization", "example-org"}, false, commandOptions{}},
		{"unknown", []string{"cutover"}, false, commandOptions{}},
		{"positional", []string{"import", "example-org"}, false, commandOptions{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			options, err := parseOptions(tc.args)
			if tc.valid {
				require.NoError(t, err)
				require.Equal(t, tc.want, options)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestDirectoryRolesRefusesUnconfirmedWriteBeforeConnecting(t *testing.T) {
	t.Parallel()
	inventory := `{"organization_id":"example-org","workos_organization_id":"example-workos-org","default_role_slug":"member","assignments":[]}`
	err := run(t.Context(), []string{"import", "-apply", "-confirm-organization", "other-org"}, strings.NewReader(inventory), io.Discard, func(string) string {
		t.Fatal("must not read credentials before confirmation")
		return ""
	})
	require.EqualError(t, err, "writes require -confirm-organization to match the inventory")
}

func TestDirectoryRolesConnectionSafety(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, databaseURL, redisAddress, redisURL string
		valid                                     bool
	}{
		{"local", "postgres://127.0.0.1/example?sslmode=disable", "127.0.0.1:6379", "", true},
		{"local IPv6", "postgres://[::1]/example?sslmode=disable", "[::1]:6379", "", true},
		{"unix", "postgres:///example?host=/tmp&sslmode=disable", "", "unix:///tmp/example-redis.sock", true},
		{"verified remote", "postgres://db.example.com/example?sslmode=verify-full", "", "rediss://cache.example.com:6379", true},
		{"plaintext database", "postgres://db.example.com/example?sslmode=disable", "localhost:6379", "", false},
		{"unverified database", "postgres://db.example.com/example?sslmode=require", "localhost:6379", "", false},
		{"database downgrade", "postgres://db.example.com/example?sslmode=prefer", "localhost:6379", "", false},
		{"remote fallback", "host=localhost,db.example.com dbname=example sslmode=disable", "localhost:6379", "", false},
		{"plaintext Redis address", "postgres://localhost/example?sslmode=disable", "cache.example.com:6379", "", false},
		{"plaintext Redis URL", "postgres://localhost/example?sslmode=disable", "", "redis://cache.example.com:6379", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			db, redisConfig, err := parseConnections(tc.databaseURL, tc.redisAddress, "", tc.redisURL)
			if tc.valid {
				require.NoError(t, err)
				require.NotNil(t, db)
				require.NotNil(t, redisConfig)
			} else {
				require.Error(t, err)
				require.Nil(t, db)
				require.Nil(t, redisConfig)
			}
		})
	}
}

func TestDirectoryRolesRequiresSupportSession(t *testing.T) {
	t.Parallel()
	inventory := `{"organization_id":"example-org","workos_organization_id":"example-workos-org","default_role_slug":"member","assignments":[]}`
	err := run(t.Context(), []string{"shadow"}, strings.NewReader(inventory), io.Discard, func(string) string { return "" })
	require.EqualError(t, err, "database, Redis connection and support-session credentials are required")
}
