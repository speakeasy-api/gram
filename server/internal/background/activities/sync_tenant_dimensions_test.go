package activities_test

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	trialsrepo "github.com/speakeasy-api/gram/server/internal/trials/repo"
)

// This test replaces package-global ClickHouse dimension tables and therefore
// must not overlap another tenant-dimension generation swap.
func TestSyncTenantDimensions_PublishesReportingProjectionAndLifecycleChanges(t *testing.T) { //nolint:paralleltest // Swaps package-global ClickHouse tables.
	ctx := t.Context()
	conn, err := infra.CloneTestDatabase(t, "sync_tenant_dimensions")
	require.NoError(t, err)
	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	orgID := "org-tenant-dimensions"
	emptyOrgID := "org-tenant-dimensions-empty"
	workosID := "workos-tenant-dimensions"
	freeTrialStartedAt := time.Date(2026, 1, 2, 3, 4, 5, 123456000, time.UTC)
	freeTrialEndsAt := freeTrialStartedAt.Add(14 * 24 * time.Hour)
	organizationCreatedAt := freeTrialStartedAt.Add(-24 * time.Hour)
	workosUpdatedAt := freeTrialStartedAt.Add(2 * time.Hour)
	trialCreatedAt := freeTrialStartedAt.Add(3 * time.Hour)
	trialEndsAt := trialCreatedAt.Add(30 * 24 * time.Hour)
	trialConvertedAt := trialCreatedAt.Add(7 * 24 * time.Hour)
	enabled := true
	disabled := false

	fixtures := testrepo.New(conn)
	require.NoError(t, fixtures.CreateOrganizationMetadataFixture(ctx, testrepo.CreateOrganizationMetadataFixtureParams{
		ID:                 orgID,
		Name:               "Tenant Dimensions Org",
		Slug:               "tenant-dimensions-org",
		GramAccountType:    "enterprise",
		WorkosID:           conv.ToPGText(workosID),
		Whitelisted:        false,
		FreeTrialStartedAt: conv.ToPGTimestamptz(freeTrialStartedAt),
		FreeTrialEndsAt:    conv.ToPGTimestamptz(freeTrialEndsAt),
		DisabledAt:         pgtype.Timestamptz{},
		CreatedAt:          conv.ToPGTimestamptz(organizationCreatedAt),
	}))
	require.NoError(t, fixtures.CreateOrganizationMetadataFixture(ctx, testrepo.CreateOrganizationMetadataFixtureParams{
		ID:                 emptyOrgID,
		Name:               "Empty Tenant Dimensions Org",
		Slug:               "empty-tenant-dimensions-org",
		GramAccountType:    "free",
		WorkosID:           pgtype.Text{},
		Whitelisted:        false,
		FreeTrialStartedAt: conv.ToPGTimestamptz(freeTrialStartedAt),
		FreeTrialEndsAt:    conv.ToPGTimestamptz(freeTrialEndsAt),
		DisabledAt:         pgtype.Timestamptz{},
		CreatedAt:          conv.ToPGTimestamptz(organizationCreatedAt),
	}))

	organizations := orgrepo.New(conn)
	_, err = organizations.UpdateOrganizationMetadataFromWorkOS(ctx, orgrepo.UpdateOrganizationMetadataFromWorkOSParams{
		Name:              "Tenant Dimensions Org",
		WorkosID:          conv.ToPGText(workosID),
		WorkosUpdatedAt:   conv.ToPGTimestamptz(workosUpdatedAt),
		WorkosLastEventID: conv.ToPGText("event-tenant-dimensions"),
		ID:                orgID,
	})
	require.NoError(t, err)
	_, err = organizations.SetWebhooksEnabled(ctx, orgrepo.SetWebhooksEnabledParams{
		Enabled: conv.PtrToPGBool(&enabled),
		ID:      orgID,
	})
	require.NoError(t, err)
	require.NoError(t, organizations.SetSCIMEnabled(ctx, orgrepo.SetSCIMEnabledParams{
		Enabled:           conv.PtrToPGBool(&enabled),
		WorkosLastEventID: conv.ToPGText("event-scim-enabled"),
		WorkosID:          conv.ToPGText(workosID),
	}))
	require.NoError(t, organizations.SetSSOEnabled(ctx, orgrepo.SetSSOEnabledParams{
		Enabled:           conv.PtrToPGBool(&disabled),
		WorkosLastEventID: conv.ToPGText("event-sso-disabled"),
		WorkosID:          conv.ToPGText(workosID),
	}))

	require.NoError(t, trialsrepo.New(conn).InsertTrialFixture(ctx, trialsrepo.InsertTrialFixtureParams{
		OrganizationID: orgID,
		Tier:           "enterprise",
		CreatedAt:      conv.ToPGTimestamptz(trialCreatedAt),
		EndsAt:         conv.ToPGTimestamptz(trialEndsAt),
		ConvertedAt:    conv.ToPGTimestamptz(trialConvertedAt),
		DemotedAt:      pgtype.Timestamptz{},
	}))

	project, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           "Tenant Dimensions Project",
		Slug:           "tenant-dimensions-project",
		OrganizationID: orgID,
	})
	require.NoError(t, err)

	activity := activities.NewSyncTenantDimensions(testenv.NewLogger(t), conn, chConn, cache.NewRedisCacheAdapter(redisClient))
	result, err := activity.Do(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, result.Organizations)
	require.Equal(t, 1, result.Projects)
	require.Zero(t, result.OrphanProjects)

	var emptyOrganizationCount uint64
	require.NoError(t, chConn.QueryRow(ctx, `
		SELECT count()
		FROM organization_metadata
		WHERE id = ?`, emptyOrgID).Scan(&emptyOrganizationCount))
	require.Zero(t, emptyOrganizationCount)

	var (
		organizationSlug string
		accountType      string
		gotWorkosID      *string
		gotWorkosUpdated *time.Time
		webhooksEnabled  *bool
		scimEnabled      *bool
		ssoEnabled       *bool
		whitelisted      bool
		gotFreeStart     time.Time
		gotFreeEnd       time.Time
		trialTier        *string
		gotTrialEnd      *time.Time
		gotConvertedAt   *time.Time
		gotDemotedAt     *time.Time
		gotTrialCreated  *time.Time
		gotTrialUpdated  *time.Time
		gotOrgCreated    time.Time
		gotOrgUpdated    time.Time
		gotDisabledAt    *time.Time
	)
	require.NoError(t, chConn.QueryRow(ctx, `
		SELECT slug, account_type, workos_id, workos_updated_at,
		       webhooks_enabled, scim_enabled, sso_enabled, whitelisted,
		       free_trial_started_at, free_trial_ends_at,
		       trial_tier, trial_ends_at, trial_converted_at, trial_demoted_at,
		       trial_created_at, trial_updated_at, created_at, updated_at, disabled_at
		FROM organization_metadata
		WHERE id = ?`, orgID).Scan(
		&organizationSlug, &accountType, &gotWorkosID, &gotWorkosUpdated,
		&webhooksEnabled, &scimEnabled, &ssoEnabled, &whitelisted,
		&gotFreeStart, &gotFreeEnd, &trialTier, &gotTrialEnd, &gotConvertedAt,
		&gotDemotedAt, &gotTrialCreated, &gotTrialUpdated, &gotOrgCreated,
		&gotOrgUpdated, &gotDisabledAt,
	))
	require.Equal(t, "tenant-dimensions-org", organizationSlug)
	require.Equal(t, "enterprise", accountType)
	require.Equal(t, workosID, *gotWorkosID)
	require.Equal(t, workosUpdatedAt, *gotWorkosUpdated)
	require.True(t, *webhooksEnabled)
	require.True(t, *scimEnabled)
	require.False(t, *ssoEnabled)
	require.False(t, whitelisted)
	require.Equal(t, freeTrialStartedAt, gotFreeStart)
	require.Equal(t, freeTrialEndsAt, gotFreeEnd)
	require.Equal(t, "enterprise", *trialTier)
	require.Equal(t, trialEndsAt, *gotTrialEnd)
	require.Equal(t, trialConvertedAt, *gotConvertedAt)
	require.Nil(t, gotDemotedAt)
	require.Equal(t, trialCreatedAt, *gotTrialCreated)
	require.NotNil(t, gotTrialUpdated)
	require.Equal(t, organizationCreatedAt, gotOrgCreated)
	require.False(t, gotOrgUpdated.IsZero())
	require.Nil(t, gotDisabledAt)

	var projectSlug, joinedOrganizationSlug string
	var projectCreatedAt, projectUpdatedAt time.Time
	var projectDeletedAt *time.Time
	require.NoError(t, chConn.QueryRow(ctx, `
		SELECT p.slug, p.created_at, p.updated_at, p.deleted_at, o.slug
		FROM projects p
		LEFT ANY JOIN organization_metadata o ON p.organization_id = o.id
		WHERE p.id = ?`, project.ID).Scan(
		&projectSlug, &projectCreatedAt, &projectUpdatedAt, &projectDeletedAt, &joinedOrganizationSlug,
	))
	require.Equal(t, "tenant-dimensions-project", projectSlug)
	require.Equal(t, "tenant-dimensions-org", joinedOrganizationSlug)
	require.Equal(t, project.CreatedAt.Time.UTC(), projectCreatedAt)
	require.Equal(t, project.UpdatedAt.Time.UTC(), projectUpdatedAt)
	require.Nil(t, projectDeletedAt)

	require.NoError(t, fixtures.SetProjectSlugFixture(ctx, testrepo.SetProjectSlugFixtureParams{
		Slug: "renamed-project",
		ID:   project.ID,
	}))
	_, err = projectsrepo.New(conn).DeleteProject(ctx, project.ID)
	require.NoError(t, err)
	_, err = organizations.UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:          orgID,
		Name:        "Tenant Dimensions Org",
		Slug:        "renamed-org",
		WorkosID:    conv.ToPGText(workosID),
		Whitelisted: conv.PtrToPGBool(&disabled),
	})
	require.NoError(t, err)

	result, err = activity.Do(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, result.Organizations)
	require.Equal(t, 1, result.Projects)

	require.NoError(t, chConn.QueryRow(ctx, `
		SELECT p.slug, p.deleted_at, o.slug
		FROM projects p
		LEFT ANY JOIN organization_metadata o ON p.organization_id = o.id
		WHERE p.id = ?`, project.ID).Scan(&projectSlug, &projectDeletedAt, &joinedOrganizationSlug))
	require.Equal(t, "renamed-project", projectSlug)
	require.NotNil(t, projectDeletedAt)
	require.Equal(t, "renamed-org", joinedOrganizationSlug)
}
