package demoseed

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/stretchr/testify/require"
)

func TestCustomerUsageSeedRefusesNonLocalTargetsBeforeConnecting(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, env, url, clickhouseHost string
	}{
		{"production environment", "production", "postgres://gram@127.0.0.1/gram", "127.0.0.1"},
		{"remote database", "local", "postgres://gram@db.example.com/gram", "127.0.0.1"},
		{"remote clickhouse", "local", "postgres://gram@127.0.0.1/gram", "clickhouse.example.com"},
		{"private network clickhouse", "local", "postgres://gram@127.0.0.1/gram", "10.0.0.1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			opened := false
			err := RunCustomerUsageSeed(t.Context(), tc.env, tc.url, tc.clickhouseHost, func(context.Context) (clickhouse.Conn, error) {
				opened = true
				return nil, errors.New("must not connect")
			})
			require.Error(t, err)
			require.False(t, opened)
		})
	}
}

func TestCustomerUsageSeedLoopbackHosts(t *testing.T) {
	t.Parallel()
	for _, host := range []string{"localhost", "127.0.0.1", "::1"} {
		require.NoError(t, validateLoopbackHost(host), host)
	}
}

func TestCustomerUsageSeedFixturesAreFictionalAndUnique(t *testing.T) {
	t.Parallel()
	ids := map[string]bool{}
	slugs := map[string]bool{}
	for _, fixture := range customerUsageSeedFixtures() {
		require.False(t, ids[fixture.ID], fixture.ID)
		require.False(t, slugs[fixture.Slug], fixture.Slug)
		ids[fixture.ID] = true
		slugs[fixture.Slug] = true
		require.True(t, strings.HasPrefix(fixture.ID, "org_local_customer_usage_"), fixture.ID)
		require.True(t, strings.HasPrefix(fixture.Name, "Fictional "), fixture.Name)
		require.Contains(t, []string{"", "running", "expired", "converted"}, fixture.Trial)
		if fixture.UsageDays < 0 {
			require.NotZero(t, fixture.AnchorDay, "usage from the current cycle needs an anchor day")
		}
	}
}
