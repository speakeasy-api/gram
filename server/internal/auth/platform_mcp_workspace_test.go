package auth_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/organizations/orgprovision"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	featurerepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	projectsRepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	trialsRepo "github.com/speakeasy-api/gram/server/internal/trials/repo"
)

func TestProvisionPlatformMCPWorkspace(t *testing.T) {
	t.Parallel()

	userInfo := platformMCPWorkspaceUser()
	ctx, instance := newTestAuthServiceForOrganizationProvisioning(t, userInfo)

	armedAt := time.Now().UTC()
	orgID, orgName, err := instance.service.ProvisionPlatformMCPWorkspace(ctx, userInfo.UserID, "  Acme   Workspace ", "Claude Code")
	require.NoError(t, err)
	require.Equal(t, "Acme Workspace", orgName)
	require.Equal(t, []string{orgID}, instance.trialNotifier.trialStarted)

	// The signup is attributed to the flow and to the MCP client connecting.
	activities := instance.growthEvents.captured()
	require.Len(t, activities, 1)
	require.Equal(t, "organization_created", activities[0]["activity"])
	require.Equal(t, orgID, activities[0]["organization_id"])
	require.Equal(t, "platform_mcp", activities[0]["created_via"])
	require.Equal(t, "platform_mcp", activities[0]["signup_source"])
	require.Equal(t, "Claude Code", activities[0]["client"])

	org, err := orgRepo.New(instance.conn).GetOrganizationMetadata(ctx, orgID)
	require.NoError(t, err)
	require.Equal(t, orgprovision.SourcePlatformMCP, conv.FromPGTextOrEmpty[string](org.CreationSource))
	require.True(t, org.Whitelisted)
	require.Equal(t, "enterprise", org.GramAccountType)

	// Same 14-day trial as a dashboard signup.
	trial, err := trialsRepo.New(instance.conn).GetTrial(ctx, orgID)
	require.NoError(t, err)
	require.Equal(t, "enterprise", trial.Tier)
	require.WithinDuration(t, armedAt.Add(14*24*time.Hour), trial.EndsAt.Time, time.Minute)

	platformMCPEnabled, err := featurerepo.New(instance.conn).IsFeatureEnabled(ctx, featurerepo.IsFeatureEnabledParams{
		OrganizationID: orgID,
		FeatureName:    string(productfeatures.FeaturePlatformMCP),
	})
	require.NoError(t, err)
	require.True(t, platformMCPEnabled)

	projects, err := projectsRepo.New(instance.conn).ListProjectsByOrganization(ctx, orgID)
	require.NoError(t, err)
	require.Len(t, projects, 1)
	require.Equal(t, "default", projects[0].Slug)

	member, err := orgRepo.New(instance.conn).HasActiveOrganizationUser(ctx, orgRepo.HasActiveOrganizationUserParams{
		UserID:         userInfo.UserID,
		OrganizationID: orgID,
	})
	require.NoError(t, err)
	require.True(t, member)

	// The user now has an organization, so a second workspace is refused.
	_, _, err = instance.service.ProvisionPlatformMCPWorkspace(ctx, userInfo.UserID, "Second Workspace", "Claude Code")
	require.ErrorIs(t, err, auth.ErrUserHasOrganization)
}

func TestProvisionPlatformMCPWorkspaceRejectsInvalidName(t *testing.T) {
	t.Parallel()

	userInfo := defaultMockUserInfo()
	userInfo.Organizations = nil
	ctx, instance := newTestAuthServiceForOrganizationProvisioning(t, userInfo)

	_, _, err := instance.service.ProvisionPlatformMCPWorkspace(ctx, userInfo.UserID, "!", "Claude Code")
	require.Error(t, err)

	organizations, err := orgRepo.New(instance.conn).ListOrganizationsForUser(ctx, conv.ToPGText(userInfo.UserID))
	require.NoError(t, err)
	require.Empty(t, organizations)
}

func TestProvisionPlatformMCPWorkspaceRefusesUserWithPendingInvitation(t *testing.T) {
	t.Parallel()

	userInfo := platformMCPWorkspaceUser()
	ctx, instance := newTestAuthServiceForOrganizationProvisioning(t, userInfo)

	const teamOrgID = "org-team"
	_, err := orgRepo.New(instance.conn).UpsertOrganizationMetadata(ctx, orgRepo.UpsertOrganizationMetadataParams{
		ID: teamOrgID, Name: "Team Org", Slug: "team-org", WorkosID: conv.ToPGText(teamOrgID), Whitelisted: pgtype.Bool{Bool: true, Valid: true},
	})
	require.NoError(t, err)
	_, err = orgRepo.New(instance.conn).CreateInvitation(ctx, orgRepo.CreateInvitationParams{
		OrganizationID: teamOrgID,
		Email:          conv.NormalizeEmail(userInfo.Email),
		TokenHash:      "token-hash-" + uuid.NewString(),
		InviterUserID:  pgtype.Text{String: "", Valid: false},
		RoleSlug:       pgtype.Text{String: "", Valid: false},
		ExpiresInDays:  7,
	})
	require.NoError(t, err)

	_, _, err = instance.service.ProvisionPlatformMCPWorkspace(ctx, userInfo.UserID, "Acme Workspace", "Claude Code")
	require.ErrorIs(t, err, auth.ErrUserHasPendingInvitation)

	organizations, err := orgRepo.New(instance.conn).ListOrganizationsForUser(ctx, conv.ToPGText(userInfo.UserID))
	require.NoError(t, err)
	require.Empty(t, organizations)
	require.Empty(t, instance.trialNotifier.trialStarted)
}

func TestProvisionPlatformMCPWorkspaceConcurrentSubmitsCreateOneOrganization(t *testing.T) {
	t.Parallel()

	userInfo := platformMCPWorkspaceUser()
	ctx, instance := newTestAuthServiceForOrganizationProvisioning(t, userInfo)

	const submits = 2
	errs := make([]error, submits)
	var wg sync.WaitGroup
	for i := range submits {
		wg.Go(func() {
			_, _, errs[i] = instance.service.ProvisionPlatformMCPWorkspace(ctx, userInfo.UserID, "Acme Workspace", "Claude Code")
		})
	}
	wg.Wait()

	var created, refused int
	for _, err := range errs {
		switch {
		case err == nil:
			created++
		case errors.Is(err, auth.ErrUserHasOrganization), errors.Is(err, auth.ErrWorkspaceCreationInProgress):
			refused++
		default:
			require.NoError(t, err)
		}
	}
	require.Equal(t, 1, created)
	require.Equal(t, 1, refused)

	organizations, err := orgRepo.New(instance.conn).ListOrganizationsForUser(ctx, conv.ToPGText(userInfo.UserID))
	require.NoError(t, err)
	require.Len(t, organizations, 1)
	require.Len(t, instance.trialNotifier.trialStarted, 1)
}

// platformMCPWorkspaceUser returns a user with no organization and a user ID
// of its own. The workspace lease is keyed by user ID in a Redis that parallel
// tests share, so a shared ID would let one test's lease refuse another's.
func platformMCPWorkspaceUser() *MockUserInfo {
	userInfo := defaultMockUserInfo()
	userInfo.UserID = "test-user-" + uuid.NewString()
	userInfo.Organizations = nil
	return userInfo
}
