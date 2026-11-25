package admin

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/hookversions"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type stubHookFleet struct {
	page  telemetry.HookInstallPage
	err   error
	org   string
	since time.Time
	limit int
}

func (s *stubHookFleet) HookInstalls(_ context.Context, organizationID string, since time.Time, limit int) (telemetry.HookInstallPage, error) {
	s.org, s.since, s.limit = organizationID, since, limit
	return s.page, s.err
}

func hookFleetTestVersions(minimum string) hookversions.Policy {
	versions := hookversions.Policy{Latest: semver.MustParse("0.3.46"), Minimum: nil}
	if minimum != "" {
		versions.Minimum = semver.MustParse(minimum)
	}
	return versions
}

func TestSummarizeHookFleetClassifiesAndBreaksDown(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	install := func(host, version, generation, route string, lastSeen time.Time, events map[string]uint64) telemetry.HookInstall {
		var total uint64
		for _, n := range events {
			total += n
		}
		return telemetry.HookInstall{
			ProjectID: "project-a", UserEmail: "dev@example.com", Hostname: host, Harness: "claude",
			BinaryVersion: version, EndpointGeneration: generation, InstallRoute: route,
			FirstSeen: lastSeen.Add(-time.Hour), LastSeen: lastSeen,
			Events: total, EventsByGeneration: events, Errors: 1,
		}
	}
	page := telemetry.HookInstallPage{Truncated: true, Installs: []telemetry.HookInstall{
		install("a", "0.3.46", "ingest_binary", "marketplace", now, map[string]uint64{"ingest_binary": 10}),
		install("b", "v0.3.40", "ingest_binary", "marketplace", now.Add(-time.Hour), map[string]uint64{"ingest_binary": 4, "ingest_script": 2}),
		install("c", "0.2.0", "ingest_script", "download", now.AddDate(0, 0, -4), map[string]uint64{"ingest_script": 3}),
		install("d", "", "legacy_keyed", "", now.AddDate(0, 0, -5), map[string]uint64{"legacy_keyed": 7, "legacy_unkeyed": 1}),
		install("e", "", "ingest_script", "download", now, map[string]uint64{"ingest_script": 1}),
	}}

	result := summarizeHookFleet(page, hookFleetTestVersions("0.3.0"), now)

	require.Equal(t, "0.3.46", result.LatestVersion)
	require.NotNil(t, result.MinimumVersion)
	require.Equal(t, "0.3.0", *result.MinimumVersion)
	require.True(t, result.Truncated)
	require.Equal(t, int64(5), result.TotalInstalls)
	require.Equal(t, int64(2), result.QuietInstalls)
	require.Equal(t, int64(8), result.LegacyEvents)

	statuses := map[string]string{}
	for _, i := range result.Installs {
		statuses[i.Hostname] = i.VersionStatus
	}
	require.Equal(t, map[string]string{"a": "current", "b": "behind", "c": "unsupported", "d": "unsupported", "e": "unknown"}, statuses)
	require.False(t, result.Installs[0].Quiet)
	require.True(t, result.Installs[2].Quiet)

	versions := make([]string, 0, len(result.Versions))
	for _, v := range result.Versions {
		versions = append(versions, v.Version+"/"+v.Status)
	}
	require.Equal(t, []string{"0.3.46/current", "v0.3.40/behind", "0.2.0/unsupported", "/unknown", "/unsupported"}, versions)

	require.Equal(t, []*gen.AdminHookFleetBucket{
		{Value: "download", Installs: 2, Events: 0},
		{Value: "marketplace", Installs: 2, Events: 0},
		{Value: "", Installs: 1, Events: 0},
	}, result.Routes)
	require.Equal(t, []*gen.AdminHookFleetBucket{
		{Value: "ingest_binary", Installs: 2, Events: 14},
		{Value: "ingest_script", Installs: 2, Events: 6},
		{Value: "legacy_keyed", Installs: 1, Events: 7},
		{Value: "legacy_unkeyed", Installs: 0, Events: 1},
	}, result.Generations)
}

func TestSummarizeHookFleetWithoutMinimumNeverUnsupported(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	page := telemetry.HookInstallPage{Truncated: false, Installs: []telemetry.HookInstall{
		{BinaryVersion: "0.0.1", LastSeen: now, FirstSeen: now, EventsByGeneration: map[string]uint64{}},
		{BinaryVersion: "dev", LastSeen: now, FirstSeen: now, EventsByGeneration: map[string]uint64{}},
	}}

	result := summarizeHookFleet(page, hookFleetTestVersions(""), now)

	require.Nil(t, result.MinimumVersion)
	require.Equal(t, "behind", result.Installs[0].VersionStatus)
	require.Equal(t, "unknown", result.Installs[1].VersionStatus)
}

func TestGetOrganizationHookFleet(t *testing.T) {
	t.Parallel()
	logger := testenv.NewLogger(t)

	t.Run("unavailable without a reader", func(t *testing.T) {
		t.Parallel()
		svc := &Service{logger: logger}
		_, err := svc.GetOrganizationHookFleet(t.Context(), &gen.GetOrganizationHookFleetPayload{OrganizationID: "org-a", WindowDays: 30})
		var shareable *oops.ShareableError
		require.ErrorAs(t, err, &shareable)
		require.Equal(t, oops.CodeUnavailable, shareable.Code)
	})

	t.Run("reads the window for the organization", func(t *testing.T) {
		t.Parallel()
		reader := &stubHookFleet{page: telemetry.HookInstallPage{Installs: []telemetry.HookInstall{}, Truncated: false}}
		svc := &Service{logger: logger}
		svc.SetHookFleet(reader, hookFleetTestVersions(""))

		before := time.Now().UTC()
		result, err := svc.GetOrganizationHookFleet(t.Context(), &gen.GetOrganizationHookFleetPayload{OrganizationID: "org-a", WindowDays: 7})
		require.NoError(t, err)
		require.Equal(t, "org-a", reader.org)
		require.Equal(t, hookFleetInstallLimit, reader.limit)
		require.WithinDuration(t, before.AddDate(0, 0, -7), reader.since, time.Minute)
		require.Equal(t, "org-a", result.OrganizationID)
		require.Equal(t, 7, result.WindowDays)
		require.Empty(t, result.Installs)
	})

	t.Run("keeps a reader's invalid error", func(t *testing.T) {
		t.Parallel()
		reader := &stubHookFleet{err: oops.E(oops.CodeInvalid, nil, "organization id is required")}
		svc := &Service{logger: logger}
		svc.SetHookFleet(reader, hookFleetTestVersions(""))
		_, err := svc.GetOrganizationHookFleet(t.Context(), &gen.GetOrganizationHookFleetPayload{OrganizationID: "", WindowDays: 7})
		var shareable *oops.ShareableError
		require.ErrorAs(t, err, &shareable)
		require.Equal(t, oops.CodeInvalid, shareable.Code)
	})

	t.Run("wraps an unexpected error", func(t *testing.T) {
		t.Parallel()
		reader := &stubHookFleet{err: errors.New("boom")}
		svc := &Service{logger: logger}
		svc.SetHookFleet(reader, hookFleetTestVersions(""))
		_, err := svc.GetOrganizationHookFleet(t.Context(), &gen.GetOrganizationHookFleetPayload{OrganizationID: "org-a", WindowDays: 7})
		var shareable *oops.ShareableError
		require.ErrorAs(t, err, &shareable)
		require.Equal(t, oops.CodeUnexpected, shareable.Code)
	})
}
