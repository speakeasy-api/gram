package demoseed

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestAdminSeedGuard(t *testing.T) {
	for _, tc := range []struct {
		name, env, url string
		valid          bool
	}{
		{"local", "local", "postgres://gram@127.0.0.1/gram", true},
		{"localhost", "local", "postgres://gram@localhost/gram", true},
		{"ipv6", "local", "postgres://gram@[::1]/gram", true},
		{"production", "production", "postgres://gram@127.0.0.1/gram", false},
		{"missing environment", "", "postgres://gram@127.0.0.1/gram", false},
		{"remote", "local", "postgres://gram@db.example.com/gram", false},
		{"private network", "local", "postgres://gram@10.0.0.1/gram", false},
		{"wrong database", "local", "postgres://gram@127.0.0.1/production", false},
		{"wrong user", "local", "postgres://postgres@127.0.0.1/gram", false},
		{"socket", "local", "host=/tmp user=gram dbname=gram", false},
		{"remote fallback", "local", "host=127.0.0.1,db.example.com user=gram dbname=gram", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config, err := pgxpool.ParseConfig(tc.url)
			require.NoError(t, err)
			err = validateAdminSeedTarget(tc.env, config.ConnConfig)
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestAdminSeedFixtures(t *testing.T) {
	now := time.Date(2026, 7, 15, 13, 20, 0, 0, time.FixedZone("offset", 3600))
	fixtures := adminSeedFixtures(now)
	require.Len(t, fixtures, 120)
	ids := map[string]bool{}
	counts := map[int]bool{}
	disabled := 0
	for _, fixture := range fixtures {
		require.False(t, ids[fixture.ID])
		ids[fixture.ID] = true
		counts[fixture.Members] = true
		require.Contains(t, fixture.Name, "Fictional")
		require.Equal(t, time.UTC, fixture.CreatedAt.Location())
		require.False(t, fixture.CreatedAt.After(now))
		if fixture.Disabled {
			disabled++
		}
	}
	require.Len(t, counts, 6)
	for _, count := range []int{0, 1, 5, 10, 25, 100} {
		require.True(t, counts[count])
	}
	require.Equal(t, 60, disabled)
	next := adminSeedFixtures(now.Add(24 * time.Hour))
	for i := range fixtures {
		require.Equal(t, fixtures[i].ID, next[i].ID)
		require.Equal(t, fixtures[i].CreatedAt.Add(24*time.Hour), next[i].CreatedAt)
	}
}

// Expectations deliberately name UTC dates rather than reuse the generator's
// offset arithmetic. Inclusive N-day presets begin N-1 calendar days ago.
func TestAdminSeedCalendarBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, invocation                          string
		today, seven, fourteen, thirty, customEnd string
	}{
		{"midday", "2026-07-15T12:00:00Z", "2026-07-15", "2026-07-09", "2026-07-02", "2026-06-16", "2026-07-13"},
		{"midnight", "2026-07-15T00:00:00Z", "2026-07-15", "2026-07-09", "2026-07-02", "2026-06-16", "2026-07-13"},
		{"timezone crosses UTC date", "2026-07-15T00:30:00+02:00", "2026-07-14", "2026-07-08", "2026-07-01", "2026-06-15", "2026-07-12"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now, err := time.Parse(time.RFC3339, tc.invocation)
			require.NoError(t, err)
			dates := [2]map[string]bool{{}, {}}
			for _, fixture := range adminSeedFixtures(now) {
				require.False(t, fixture.CreatedAt.After(now), "future fixture: %s", fixture.ID)
				status := 0
				if fixture.Disabled {
					status = 1
				}
				dates[status][fixture.CreatedAt.Format(time.RFC3339)] = true
			}
			require.Equal(t, dates[0], dates[1], "both status sets cover the same dates")
			for _, day := range []string{tc.today, tc.seven, tc.fourteen, tc.thirty, tc.customEnd} {
				boundary, err := time.Parse(time.RFC3339, day+"T00:00:00Z")
				require.NoError(t, err)
				for _, offset := range []time.Duration{-time.Second, 0, time.Second} {
					expected := boundary.Add(offset)
					if expected.After(now) {
						continue
					}
					require.True(t, dates[0][expected.Format(time.RFC3339)], "missing exact/before/after UTC boundary %s", expected)
				}
			}
		})
	}
}
