package skills_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/skills"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	skillsrepo "github.com/speakeasy-api/gram/server/internal/skills/repo"
)

func TestService_GetSettings_ReturnsInheritedEffectiveMode(t *testing.T) {
	t.Parallel()

	mode := "user_only"
	ctx, ti := newTestSkillsServiceWithCaptureMode(t, &mode)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	require.NotNil(t, authCtx.ProjectID)

	result, err := ti.service.GetSettings(ctx, &gen.GetSettingsPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, "user_only", result.EffectiveMode)
	require.Equal(t, "user_only", *result.OrgDefaultMode)
	require.Nil(t, result.ProjectOverrideMode)
	require.True(t, result.Enabled)
	require.False(t, result.CaptureProjectSkills)
	require.True(t, result.CaptureUserSkills)
	require.True(t, result.InheritedFromOrganization)
}

func TestService_SetSettings_PersistsProjectOverride(t *testing.T) {
	t.Parallel()

	mode := "user_only"
	ctx, ti := newTestSkillsServiceWithCaptureMode(t, &mode)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	require.NotNil(t, authCtx.ProjectID)

	result, err := ti.service.SetSettings(ctx, &gen.SetSettingsPayload{
		SessionToken:         nil,
		ProjectSlugInput:     nil,
		Enabled:              true,
		CaptureProjectSkills: true,
		CaptureUserSkills:    false,
	})
	require.NoError(t, err)
	require.Equal(t, "project_only", result.EffectiveMode)
	require.Equal(t, "user_only", *result.OrgDefaultMode)
	require.Equal(t, "project_only", *result.ProjectOverrideMode)
	require.True(t, result.Enabled)
	require.True(t, result.CaptureProjectSkills)
	require.False(t, result.CaptureUserSkills)
	require.False(t, result.InheritedFromOrganization)

	row, err := ti.skillsRepo.GetCaptureSettings(ctx, skillsrepo.GetCaptureSettingsParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
	})
	require.NoError(t, err)
	require.Equal(t, "user_only", row.OrgDefaultMode)
	require.Equal(t, "project_only", row.ProjectOverrideMode)
	require.Equal(t, "project_only", row.EffectiveMode)
}
