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
	}{
		{"import preview", []string{"import"}, true},
		{"import apply", []string{"import", "-apply", "-confirm-organization", "example-org"}, true},
		{"shadow", []string{"shadow"}, true},
		{"shadow apply", []string{"shadow", "-apply"}, false},
		{"shadow confirmation", []string{"shadow", "-confirm-organization", "example-org"}, false},
		{"unknown", []string{"cutover"}, false},
		{"positional", []string{"import", "example-org"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseOptions(tc.args)
			if tc.valid {
				require.NoError(t, err)
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

func TestDirectoryRolesRequiresSupportSession(t *testing.T) {
	t.Parallel()
	inventory := `{"organization_id":"example-org","workos_organization_id":"example-workos-org","default_role_slug":"member","assignments":[]}`
	err := run(t.Context(), []string{"shadow"}, strings.NewReader(inventory), io.Discard, func(string) string { return "" })
	require.EqualError(t, err, "GRAM_DATABASE_URL, GRAM_REDIS_CACHE_ADDR and GRAM_SUPPORT_SESSION_TOKEN are required")
}
