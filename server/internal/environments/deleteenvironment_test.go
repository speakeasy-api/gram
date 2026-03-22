package environments_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/environments"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
)

func TestEnvironmentsService_DeleteEnvironment(t *testing.T) {
	t.Parallel()

	t.Run("delete existing environment", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestEnvironmentService(t)

		// Create environment
		env, err := ti.service.CreateEnvironment(ctx, &gen.CreateEnvironmentPayload{
			SessionToken:     nil,
			ProjectSlugInput: nil,
			OrganizationID:   "",
			Name:             "to-delete",
			Description:      nil,
			Entries: []*gen.EnvironmentEntryInput{
				{Name: "KEY1", Value: "value1"},
			},
		})
		require.NoError(t, err)
		beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionEnvironmentDelete)
		require.NoError(t, err)

		// Delete environment
		err = ti.service.DeleteEnvironment(ctx, &gen.DeleteEnvironmentPayload{
			SessionToken:     nil,
			ProjectSlugInput: nil,
			Slug:             env.Slug,
		})
		require.NoError(t, err)
		afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionEnvironmentDelete)
		require.NoError(t, err)
		require.Equal(t, beforeCount+1, afterCount)

		// Verify environment is deleted by trying to list environments
		result, err := ti.service.ListEnvironments(ctx, &gen.ListEnvironmentsPayload{
			SessionToken:     nil,
			ProjectSlugInput: nil,
		})
		require.NoError(t, err)
		require.Empty(t, result.Environments)
	})

	t.Run("delete non-existent environment", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestEnvironmentService(t)
		beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionEnvironmentDelete)
		require.NoError(t, err)

		// Try to delete non-existent environment
		err = ti.service.DeleteEnvironment(ctx, &gen.DeleteEnvironmentPayload{
			SessionToken:     nil,
			ProjectSlugInput: nil,
			Slug:             "non-existent",
		})
		// This should not error based on the implementation
		require.NoError(t, err)
		afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionEnvironmentDelete)
		require.NoError(t, err)
		require.Equal(t, beforeCount, afterCount)
	})
}

func TestEnvironmentsService_DeleteEnvironment_AuditLog(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestEnvironmentService(t)
	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionEnvironmentDelete)
	require.NoError(t, err)

	env, err := ti.service.CreateEnvironment(ctx, &gen.CreateEnvironmentPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		OrganizationID:   "",
		Name:             "audit-delete-env",
		Description:      nil,
		Entries: []*gen.EnvironmentEntryInput{
			{Name: "API_KEY", Value: "super-secret-delete-value"},
		},
	})
	require.NoError(t, err)

	err = ti.service.DeleteEnvironment(ctx, &gen.DeleteEnvironmentPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Slug:             env.Slug,
	})
	require.NoError(t, err)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionEnvironmentDelete)
	require.NoError(t, err)
	require.Equal(t, string(audit.ActionEnvironmentDelete), record.Action)
	require.Equal(t, "environment", record.SubjectType)
	require.Equal(t, env.Name, record.SubjectDisplay)
	require.Equal(t, string(env.Slug), record.SubjectSlug)
	require.Nil(t, record.BeforeSnapshot)
	require.Nil(t, record.AfterSnapshot)
	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionEnvironmentDelete)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}
