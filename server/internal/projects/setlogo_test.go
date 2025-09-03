package projects_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/projects"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsRepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
)

func TestSetLogo_Success(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestProjectsService(t)

	// Create a test asset ID
	assetID := uuid.New()

	// Call SetLogo
	payload := &projects.SetLogoForm{
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		SessionToken:     nil,
		AssetID:          assetID.String(),
	}

	result, err := ti.service.SetLogo(ctx, payload)

	// Verify success
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Project)

	// Verify project data
	assert.NotEmpty(t, result.Project.ID)
	assert.NotEmpty(t, result.Project.Name)
	assert.NotEmpty(t, result.Project.Slug)
	assert.NotEmpty(t, result.Project.OrganizationID)
	assert.NotEmpty(t, result.Project.CreatedAt)
	assert.NotEmpty(t, result.Project.UpdatedAt)

	// Verify logo asset ID is set
	require.NotNil(t, result.Project.LogoAssetID)
	expectedLogoAssetID := assetID.String()
	assert.Equal(t, expectedLogoAssetID, *result.Project.LogoAssetID)

	// Verify database was updated
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	project, err := projectsRepo.New(ti.conn).GetProjectByID(ctx, *authCtx.ProjectID)
	require.NoError(t, err)
	assert.True(t, project.LogoAssetID.Valid)
	assert.Equal(t, assetID, project.LogoAssetID.UUID)
}

func TestSetLogo_InvalidAssetID(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestProjectsService(t)

	// Call SetLogo with invalid asset ID
	payload := &projects.SetLogoForm{
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		SessionToken:     nil,
		AssetID:          "invalid-uuid",
	}

	result, err := ti.service.SetLogo(ctx, payload)

	// Verify error
	require.Error(t, err)
	assert.Nil(t, result)

	// Check error type
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	assert.Equal(t, oops.CodeUnexpected, oopsErr.Code)
	assert.Contains(t, oopsErr.Error(), "error parsing asset ID")
}

func TestSetLogo_UnauthorizedNoAuthContext(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ti := newTestProjectsService(t)

	// Call SetLogo without auth context
	payload := &projects.SetLogoForm{
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		SessionToken:     nil,
		AssetID:          uuid.New().String(),
	}

	result, err := ti.service.SetLogo(ctx, payload)

	// Verify error
	require.Error(t, err)
	assert.Nil(t, result)

	// Check error type
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	assert.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
}

func TestSetLogo_UnauthorizedNoProjectID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ti := newTestProjectsService(t)

	// Create auth context without project ID
	ctx, err := ti.sessionManager.Authenticate(ctx, "", true)
	require.NoError(t, err)

	// Call SetLogo without project ID in auth context
	payload := &projects.SetLogoForm{
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		SessionToken:     nil,
		AssetID:          uuid.New().String(),
	}

	result, err := ti.service.SetLogo(ctx, payload)

	// Verify error
	require.Error(t, err)
	assert.Nil(t, result)

	// Check error type
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	assert.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
}

func TestSetLogo_DatabaseErrorProjectNotFound(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestProjectsService(t)

	// Create auth context with non-existent project ID
	ctx, err := ti.sessionManager.Authenticate(ctx, "", true)
	require.NoError(t, err)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	// Set a non-existent project ID
	nonExistentProjectID := uuid.New()
	authCtx.ProjectID = &nonExistentProjectID

	// Call SetLogo
	payload := &projects.SetLogoForm{
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		SessionToken:     nil,
		AssetID:          uuid.New().String(),
	}

	result, err := ti.service.SetLogo(ctx, payload)

	// Verify error
	require.Error(t, err)
	assert.Nil(t, result)

	// Check error type
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	assert.Equal(t, oops.CodeUnexpected, oopsErr.Code)
	assert.Contains(t, oopsErr.Error(), "error updating project logo")
}

func TestSetLogo_EmptyAssetID(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestProjectsService(t)

	// Call SetLogo with empty asset ID
	payload := &projects.SetLogoForm{
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		SessionToken:     nil,
		AssetID:          "",
	}

	result, err := ti.service.SetLogo(ctx, payload)

	// Verify error
	require.Error(t, err)
	assert.Nil(t, result)

	// Check error type
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	assert.Equal(t, oops.CodeUnexpected, oopsErr.Code)
	assert.Contains(t, oopsErr.Error(), "error parsing asset ID")
}

func TestSetLogo_MalformedUUID(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestProjectsService(t)

	// Call SetLogo with malformed UUID
	payload := &projects.SetLogoForm{
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		SessionToken:     nil,
		AssetID:          "not-a-uuid-at-all",
	}

	result, err := ti.service.SetLogo(ctx, payload)

	// Verify error
	require.Error(t, err)
	assert.Nil(t, result)

	// Check error type
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	assert.Equal(t, oops.CodeUnexpected, oopsErr.Code)
	assert.Contains(t, oopsErr.Error(), "error parsing asset ID")
}

func TestSetLogo_NilPayload(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestProjectsService(t)

	// Call SetLogo with nil payload - this should panic
	assert.Panics(t, func() {
		_, _ = ti.service.SetLogo(ctx, nil)
	}, "SetLogo should panic when called with nil payload")
}

func TestSetLogo_UpdateExistingLogo(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestProjectsService(t)

	// First, set an initial logo
	firstAssetID := uuid.New()
	payload1 := &projects.SetLogoForm{
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		SessionToken:     nil,
		AssetID:          firstAssetID.String(),
	}

	result1, err := ti.service.SetLogo(ctx, payload1)
	require.NoError(t, err)
	require.NotNil(t, result1.Project.LogoAssetID)

	// Then update to a different logo
	secondAssetID := uuid.New()
	payload2 := &projects.SetLogoForm{
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		SessionToken:     nil,
		AssetID:          secondAssetID.String(),
	}

	result2, err := ti.service.SetLogo(ctx, payload2)
	require.NoError(t, err)
	require.NotNil(t, result2.Project.LogoAssetID)

	// Verify the logo was updated
	expectedLogoAssetID := secondAssetID.String()
	assert.Equal(t, expectedLogoAssetID, *result2.Project.LogoAssetID)

	// Verify database was updated
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	project, err := projectsRepo.New(ti.conn).GetProjectByID(ctx, *authCtx.ProjectID)
	require.NoError(t, err)
	assert.True(t, project.LogoAssetID.Valid)
	assert.Equal(t, secondAssetID, project.LogoAssetID.UUID)
}
