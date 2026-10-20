package deviceagentversions_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/deviceagentversions"
)

func TestParseUserAgent(t *testing.T) {
	t.Parallel()

	cases := []struct {
		userAgent string
		version   string
		ok        bool
	}{
		{userAgent: "speakeasyd/0.3.31", version: "0.3.31", ok: true},
		{userAgent: "speakeasyd/v1.2.0-rc.1 (darwin; arm64)", version: "1.2.0-rc.1", ok: true},
		{userAgent: "speakeasyd/dev", version: "", ok: false},
		{userAgent: "curl/8.4.0", version: "", ok: false},
		{userAgent: "", version: "", ok: false},
	}
	for _, c := range cases {
		version, ok := deviceagentversions.ParseUserAgent(c.userAgent)
		require.Equal(t, c.ok, ok, c.userAgent)
		require.Equal(t, c.version, version, c.userAgent)
	}
}

func TestStore_CountsEachMachineOncePerDayByLatestVersion(t *testing.T) {
	t.Parallel()

	server := miniredis.RunT(t)
	store := deviceagentversions.NewStore(redis.NewClient(&redis.Options{Addr: server.Addr()}))
	ctx := t.Context()
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	yesterday := now.Add(-24 * time.Hour)

	// m-1 upgrades today, so it counts once under its latest version.
	require.NoError(t, store.Record(ctx, "org-a", "m-1", "0.3.30", now.Add(-2*time.Hour)))
	require.NoError(t, store.Record(ctx, "org-a", "m-1", "0.3.31", now))
	require.NoError(t, store.Record(ctx, "org-a", "m-2", "0.3.31", now))
	require.NoError(t, store.Record(ctx, "org-a", "m-1", "0.3.30", yesterday))
	require.NoError(t, store.Record(ctx, "org-b", "m-9", "0.3.29", now))

	rows, err := store.Read(ctx, 2, now)
	require.NoError(t, err)
	require.Equal(t, []deviceagentversions.Row{
		{OrganizationID: "org-a", Day: "2026-10-19", Version: "", Machines: 1},
		{OrganizationID: "org-a", Day: "2026-10-19", Version: "0.3.30", Machines: 1},
		{OrganizationID: "org-a", Day: "2026-10-20", Version: "", Machines: 2},
		{OrganizationID: "org-a", Day: "2026-10-20", Version: "0.3.31", Machines: 2},
		{OrganizationID: "org-b", Day: "2026-10-20", Version: "", Machines: 1},
		{OrganizationID: "org-b", Day: "2026-10-20", Version: "0.3.29", Machines: 1},
	}, rows)

	// Each day's keys expire on their own, so nothing needs a sweep.
	for _, key := range []string{
		"device_agent_versions:2026-10-20:org-a",
		"device_agent_versions:2026-10-20:orgs",
		"device_agent_versions:2026-10-19:org-a",
		"device_agent_versions:2026-10-19:orgs",
	} {
		require.Equal(t, deviceagentversions.Retention, server.TTL(key), key)
	}
}

func TestStore_ReadOutsideWindowIsEmpty(t *testing.T) {
	t.Parallel()

	server := miniredis.RunT(t)
	store := deviceagentversions.NewStore(redis.NewClient(&redis.Options{Addr: server.Addr()}))
	ctx := t.Context()
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	require.NoError(t, store.Record(ctx, "org-a", "m-1", "0.3.31", now.AddDate(0, 0, -5)))

	rows, err := store.Read(ctx, 3, now)
	require.NoError(t, err)
	require.Empty(t, rows)
}

func TestStore_BoundsMachineIdentities(t *testing.T) {
	t.Parallel()

	server := miniredis.RunT(t)
	store := deviceagentversions.NewStore(redis.NewClient(&redis.Options{Addr: server.Addr()}))
	ctx := t.Context()
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)

	require.NoError(t, store.Record(ctx, "org-a", strings.Repeat("x", deviceagentversions.MaxMachineLength+1), "0.3.31", now))
	require.NoError(t, store.Record(ctx, "org-a", "", "0.3.31", now))
	rows, err := store.Read(ctx, 1, now)
	require.NoError(t, err)
	require.Empty(t, rows)

	// Fill the day to the cap, then a new machine is dropped but a known one
	// still updates.
	key := "device_agent_versions:2026-10-20:org-a"
	for i := range deviceagentversions.MaxMachinesPerDay {
		server.HSet(key, fmt.Sprintf("m-%d", i), "0.3.30")
	}
	require.NoError(t, store.Record(ctx, "org-a", "m-new", "0.3.31", now))
	require.NoError(t, store.Record(ctx, "org-a", "m-0", "0.3.31", now))
	require.False(t, server.Exists(key) && server.HGet(key, "m-new") != "")
	require.Equal(t, "0.3.31", server.HGet(key, "m-0"))
}
