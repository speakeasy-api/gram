package projects_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/projects"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsRepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
)

func TestSetLogo_CreatesAuditLog(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestProjectsService(t, true)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	ctx = withAccessGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeProjectWrite, authCtx.ProjectID.String()))
	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionProjectUpdate)
	require.NoError(t, err)

	assetID := uuid.New()
	result, err := ti.service.SetLogo(ctx, &projects.SetLogoPayload{
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		SessionToken:     nil,
		AssetID:          assetID.String(),
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Project)
	require.NotNil(t, result.Project.LogoAssetID)
	require.Equal(t, assetID.String(), *result.Project.LogoAssetID)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionProjectUpdate)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}

func TestSetLogo_InvalidAssetID_DoesNotCreateAuditLog(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestProjectsService(t, true)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	ctx = withAccessGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeProjectWrite, authCtx.ProjectID.String()))
	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionProjectUpdate)
	require.NoError(t, err)

	result, err := ti.service.SetLogo(ctx, &projects.SetLogoPayload{
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		SessionToken:     nil,
		AssetID:          "invalid-uuid",
	})
	require.Error(t, err)
	require.Nil(t, result)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionProjectUpdate)
	require.NoError(t, err)
	require.Equal(t, beforeCount, afterCount)
}

func TestSetLogo_AuditLogSnapshots(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestProjectsService(t, true)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	ctx = withAccessGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeProjectWrite, authCtx.ProjectID.String()))
	assetID := uuid.New()

	result, err := ti.service.SetLogo(ctx, &projects.SetLogoPayload{
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		SessionToken:     nil,
		AssetID:          assetID.String(),
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Project)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionProjectUpdate)
	require.NoError(t, err)
	require.Equal(t, string(audit.ActionProjectUpdate), record.Action)
	require.Equal(t, "project", record.SubjectType)
	require.Equal(t, result.Project.Name, record.SubjectDisplay)
	require.Equal(t, string(result.Project.Slug), record.SubjectSlug)
	require.Nil(t, record.Metadata)
	require.NotNil(t, record.BeforeSnapshot)
	require.NotNil(t, record.AfterSnapshot)

	beforeSnapshot, err := audittest.DecodeAuditData(record.BeforeSnapshot)
	require.NoError(t, err)
	require.Equal(t, result.Project.ID, beforeSnapshot["ID"])
	require.Equal(t, result.Project.Name, beforeSnapshot["Name"])
	require.Equal(t, string(result.Project.Slug), beforeSnapshot["Slug"])
	require.Nil(t, beforeSnapshot["LogoAssetID"])

	afterSnapshot, err := audittest.DecodeAuditData(record.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, result.Project.ID, afterSnapshot["ID"])
	require.Equal(t, result.Project.Name, afterSnapshot["Name"])
	require.Equal(t, string(result.Project.Slug), afterSnapshot["Slug"])
	require.Equal(t, assetID.String(), afterSnapshot["LogoAssetID"])
}

func TestSetLogo_Success(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestProjectsService(t, true)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	ctx = withAccessGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeProjectWrite, authCtx.ProjectID.String()))

	// Create a test asset ID
	assetID := uuid.New()

	// Call SetLogo
	payload := &projects.SetLogoPayload{
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
	authCtx, ok = contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	project, err := projectsRepo.New(ti.conn).GetProjectByID(ctx, *authCtx.ProjectID)
	require.NoError(t, err)
	assert.True(t, project.LogoAssetID.Valid)
	assert.Equal(t, assetID, project.LogoAssetID.UUID)
}

func TestSetLogo_InvalidAssetID(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestProjectsService(t, true)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	ctx = withAccessGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeProjectWrite, authCtx.ProjectID.String()))

	payload := &projects.SetLogoPayload{
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		SessionToken:     nil,
		AssetID:          "invalid-uuid",
	}

	result, err := ti.service.SetLogo(ctx, payload)

	require.Error(t, err)
	assert.Nil(t, result)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	assert.Equal(t, oops.CodeInvalid, oopsErr.Code)
	assert.Contains(t, oopsErr.Error(), "error parsing asset ID")
}

func TestSetLogo_ForbiddenWithoutBuildWriteGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestProjectsService(t, true)
	ctx = authz.GrantsToContext(ctx, nil)

	result, err := ti.service.SetLogo(ctx, &projects.SetLogoPayload{
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		SessionToken:     nil,
		AssetID:          uuid.New().String(),
	})

	require.Error(t, err)
	assert.Nil(t, result)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	assert.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestSetLogo_SkipsRBACWhenDisabled(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestProjectsService(t, false)
	ctx = authz.GrantsToContext(ctx, nil)

	assetID := uuid.New()
	result, err := ti.service.SetLogo(ctx, &projects.SetLogoPayload{
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		SessionToken:     nil,
		AssetID:          assetID.String(),
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Project)
	require.NotNil(t, result.Project.LogoAssetID)
	require.Equal(t, assetID.String(), *result.Project.LogoAssetID)
}

func TestSetLogo_UnauthorizedNoAuthContext(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ti := newTestProjectsService(t, true)

	// Call SetLogo without auth context
	payload := &projects.SetLogoPayload{
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
	ctx, ti := newTestProjectsService(t, true)

	// Clear project ID from auth context
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.ProjectID = nil
	authCtx.ProjectSlug = nil

	// Call SetLogo without project ID in auth context
	payload := &projects.SetLogoPayload{
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
	ctx, ti := newTestProjectsService(t, true)
	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionProjectUpdate)
	require.NoError(t, err)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	// Set a non-existent project ID
	nonExistentProjectID := uuid.New()
	authCtx.ProjectID = &nonExistentProjectID
	ctx = withAccessGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeProjectWrite, nonExistentProjectID.String()))

	// Call SetLogo
	payload := &projects.SetLogoPayload{
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
	assert.Contains(t, oopsErr.Error(), "error getting project")

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionProjectUpdate)
	require.NoError(t, err)
	require.Equal(t, beforeCount, afterCount)
}

func TestSetLogo_EmptyAssetID(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestProjectsService(t, true)

	// Call SetLogo with empty asset ID
	payload := &projects.SetLogoPayload{
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
	assert.Equal(t, oops.CodeInvalid, oopsErr.Code)
	assert.Contains(t, oopsErr.Error(), "error parsing asset ID")
}

func TestSetLogo_NilPayload(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestProjectsService(t, true)

	// Call SetLogo with nil payload - this should panic
	assert.Panics(t, func() {
		_, _ = ti.service.SetLogo(ctx, nil)
	}, "SetLogo should panic when called with nil payload")
}

func TestSetLogo_UpdateExistingLogo(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestProjectsService(t, true)

	// First, set an initial logo
	firstAssetID := uuid.New()
	payload1 := &projects.SetLogoPayload{
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
	payload2 := &projects.SetLogoPayload{
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
