package skills_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/skills"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestService_List_RequiresSkillsCaptureFeature(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestSkillsServiceWithCaptureModeAndFeature(t, nil, false)

	_, err := ti.service.List(ctx, &gen.ListPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
	require.Contains(t, oopsErr.Error(), "skills capture is not enabled")
}

func TestService_GetSettings_RequiresSkillsCaptureFeature(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestSkillsServiceWithCaptureModeAndFeature(t, nil, false)

	_, err := ti.service.GetSettings(ctx, &gen.GetSettingsPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
	require.Contains(t, oopsErr.Error(), "skills capture is not enabled")
}

func TestService_SetSettings_RequiresSkillsCaptureFeature(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestSkillsServiceWithCaptureModeAndFeature(t, nil, false)

	_, err := ti.service.SetSettings(ctx, &gen.SetSettingsPayload{
		SessionToken:         nil,
		ProjectSlugInput:     nil,
		Enabled:              true,
		CaptureProjectSkills: true,
		CaptureUserSkills:    false,
	})

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
	require.Contains(t, oopsErr.Error(), "skills capture is not enabled")
}

func TestService_Get_RequiresSkillsCaptureFeature(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestSkillsServiceWithCaptureModeAndFeature(t, nil, false)

	_, err := ti.service.Get(ctx, &gen.GetPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Slug:             "missing-skill",
	})

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
	require.Contains(t, oopsErr.Error(), "skills capture is not enabled")
}
