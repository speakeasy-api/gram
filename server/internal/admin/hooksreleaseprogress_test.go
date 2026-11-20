package admin

import (
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/hooksrollout"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
)

func TestSummarizeHooksPublishState(t *testing.T) {
	t.Parallel()
	canary := hooksrollout.CanaryOrganizationSlugs()[0]
	rows := []repo.AdminListHooksPublishStateRow{
		{OrganizationID: "org-canary", OrganizationSlug: canary, PublishedHooksVersion: "66"},
		{OrganizationID: "org-override", OrganizationSlug: "override", PublishedHooksVersion: "66"},
		{OrganizationID: "org-override", OrganizationSlug: "override", PublishedHooksVersion: "64"},
		{OrganizationID: "org-default", OrganizationSlug: "default", PublishedHooksVersion: "64"},
		{OrganizationID: "org-new", OrganizationSlug: "new", PublishedHooksVersion: ""},
	}
	overrides := []hooksrollout.Override{{OrganizationID: "org-override", OrganizationName: "", OrganizationSlug: "override", Pin: hooksrollout.Pin{Version: 66, SetBy: "", SetAt: time.Time{}}}}

	t.Run("with a default pin", func(t *testing.T) {
		t.Parallel()
		result := summarizeHooksPublishState(rows, 66, &hooksrollout.Pin{Version: 64, SetBy: "", SetAt: time.Time{}}, overrides)
		require.Equal(t, int64(4), result.Organizations)
		require.Equal(t, int64(2), result.OrganizationsCleared)
		require.Equal(t, int64(2), result.OrganizationsHeld)
		require.Equal(t, int64(0), result.OrganizationsOnLegacyFlag)
		require.Equal(t, int64(5), result.Repos)
		require.Equal(t, int64(2), result.ReposOnCurrent)
		require.Equal(t, []*gen.AdminHooksReleaseRepoVersion{
			{Version: "66", Repos: 2},
			{Version: "64", Repos: 2},
			{Version: "", Repos: 1},
		}, result.RepoVersions)
	})

	t.Run("without a default pin", func(t *testing.T) {
		t.Parallel()
		result := summarizeHooksPublishState(rows, 66, nil, overrides)
		require.Equal(t, int64(2), result.OrganizationsCleared)
		require.Equal(t, int64(0), result.OrganizationsHeld)
		require.Equal(t, int64(2), result.OrganizationsOnLegacyFlag)
	})
}

func TestSummarizeHooksReleaseFleet(t *testing.T) {
	t.Parallel()
	rows := []telemetry.HookVersionMixRow{
		{OrganizationID: "a", BinaryVersion: "0.3.40", EndpointGeneration: "ingest_binary", Installs: 2, Events: 0, LegacyEvents: 0},
		{OrganizationID: "a", BinaryVersion: "0.3.46", EndpointGeneration: "ingest_binary", Installs: 3, Events: 0, LegacyEvents: 0},
		{OrganizationID: "b", BinaryVersion: "0.3.46", EndpointGeneration: "ingest_binary", Installs: 1, Events: 0, LegacyEvents: 0},
		{OrganizationID: "b", BinaryVersion: "", EndpointGeneration: "legacy_keyed", Installs: 4, Events: 0, LegacyEvents: 0},
		{OrganizationID: "b", BinaryVersion: "", EndpointGeneration: "ingest_script", Installs: 1, Events: 0, LegacyEvents: 0},
	}
	fleet := summarizeHooksReleaseFleet(rows, hookFleetTestVersions(""))
	require.Equal(t, hooksReleaseFleetWindowDays, fleet.WindowDays)
	require.Equal(t, int64(11), fleet.Installs)
	require.Equal(t, int64(4), fleet.InstallsOnCurrent)
	require.Equal(t, []*gen.AdminHooksReleaseInstallVersion{
		{Version: "0.3.46", Status: "current", Installs: 4},
		{Version: "0.3.40", Status: "behind", Installs: 2},
		{Version: "", Status: "unknown", Installs: 1},
		{Version: "", Status: "unsupported", Installs: 4},
	}, fleet.Versions)
}

func TestGetHooksReleaseProgress(t *testing.T) {
	t.Parallel()
	ctx, svc, conn := newTestAdminService(t)
	ctx = hooksRolloutContext(ctx)
	reader := &stubHookFleet{mix: []telemetry.HookVersionMixRow{
		{OrganizationID: "a", BinaryVersion: "0.3.46", EndpointGeneration: "ingest_binary", Installs: 2, Events: 0, LegacyEvents: 0},
	}}
	svc.SetHookFleet(reader, hookFleetTestVersions(""))

	current := currentHooksVersion(t)
	orgID := createHooksRolloutOrganization(t, ctx, conn, "release-progress")
	for i, published := range []string{strconv.Itoa(current), ""} {
		slug := fmt.Sprintf("release-progress-%d", i)
		project, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{Name: slug, Slug: slug, OrganizationID: orgID})
		require.NoError(t, err)
		_, err = pluginsrepo.New(conn).UpsertGitHubConnection(ctx, pluginsrepo.UpsertGitHubConnectionParams{
			ProjectID:                project.ID,
			InstallationID:           int64(i + 1),
			RepoOwner:                "example",
			RepoName:                 slug,
			MarketplaceToken:         pgtype.Text{String: "", Valid: false},
			PublishedMcpFingerprints: nil,
			PublishedHooksVersion:    pgtype.Text{String: published, Valid: published != ""},
			PublishedHooksConfig:     nil,
		})
		require.NoError(t, err)
	}

	result, err := svc.GetHooksReleaseProgress(ctx, &gen.GetHooksReleaseProgressPayload{AdminSessionToken: nil})
	require.NoError(t, err)
	require.Equal(t, current, result.CurrentVersion)
	require.Equal(t, int64(1), result.Organizations)
	require.Equal(t, int64(1), result.OrganizationsOnLegacyFlag)
	require.Equal(t, int64(2), result.Repos)
	require.Equal(t, int64(1), result.ReposOnCurrent)
	require.Equal(t, "0.3.46", *result.CurrentBinaryVersion)
	require.NotNil(t, result.Fleet)
	require.Equal(t, int64(2), result.Fleet.InstallsOnCurrent)
}
