package environments_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/environments"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestEnvironmentsService_CreateEnvironment(t *testing.T) {
	t.Parallel()

	t.Run("create environment with entries", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestEnvironmentService(t)
		beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionEnvironmentCreate)
		require.NoError(t, err)

		payload := &gen.CreateEnvironmentPayload{
			SessionToken:     nil,
			ProjectSlugInput: nil,
			OrganizationID:   "",
			Name:             "test-env",
			Description:      new("Test environment description"),
			Entries: []*gen.EnvironmentEntryInput{
				{
					Name:     "API_KEY",
					Value:    new("secret-key-123"),
					IsSecret: new(true),
				},
				{
					Name:     "DATABASE_URL",
					Value:    new("postgres://localhost:5432/testdb"),
					IsSecret: new(false),
				},
			},
		}

		env, err := ti.service.CreateEnvironment(ctx, payload)
		require.NoError(t, err)
		require.NotNil(t, env)

		require.NotEmpty(t, env.ID)
		require.Equal(t, "test-env", env.Name)
		require.Equal(t, "test-env", string(env.Slug))
		require.Equal(t, "Test environment description", *env.Description)
		require.Len(t, env.Entries, 2)

		// Secret values come back redacted; non-secret values come back in cleartext.
		entriesByName := make(map[string]string, len(env.Entries))
		for _, entry := range env.Entries {
			require.NotEmpty(t, entry.Name)
			require.NotEmpty(t, entry.CreatedAt)
			require.NotEmpty(t, entry.UpdatedAt)
			entriesByName[entry.Name] = entry.Value
		}
		require.Equal(t, "sec*****", entriesByName["API_KEY"])
		require.Equal(t, "postgres://localhost:5432/testdb", entriesByName["DATABASE_URL"])

		afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionEnvironmentCreate)
		require.NoError(t, err)
		require.Equal(t, beforeCount+1, afterCount)
	})

	t.Run("create environment without entries", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestEnvironmentService(t)
		beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionEnvironmentCreate)
		require.NoError(t, err)

		payload := &gen.CreateEnvironmentPayload{
			SessionToken:     nil,
			ProjectSlugInput: nil,
			OrganizationID:   "",
			Name:             "empty-env",
			Description:      nil,
			Entries:          []*gen.EnvironmentEntryInput{},
		}

		env, err := ti.service.CreateEnvironment(ctx, payload)
		require.NoError(t, err)
		require.NotNil(t, env)

		require.NotEmpty(t, env.ID)
		require.Equal(t, "empty-env", env.Name)
		require.Equal(t, "empty-env", string(env.Slug))
		require.Nil(t, env.Description)
		require.Empty(t, env.Entries)
		afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionEnvironmentCreate)
		require.NoError(t, err)
		require.Equal(t, beforeCount+1, afterCount)
	})

	t.Run("create environment with slug generation", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestEnvironmentService(t)
		beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionEnvironmentCreate)
		require.NoError(t, err)

		payload := &gen.CreateEnvironmentPayload{
			SessionToken:     nil,
			ProjectSlugInput: nil,
			OrganizationID:   "",
			Name:             "Test Environment With Spaces",
			Description:      nil,
			Entries:          []*gen.EnvironmentEntryInput{},
		}

		env, err := ti.service.CreateEnvironment(ctx, payload)
		require.NoError(t, err)
		require.NotNil(t, env)

		require.Equal(t, "Test Environment With Spaces", env.Name)
		require.Equal(t, "test-environment-with-spaces", string(env.Slug))
		afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionEnvironmentCreate)
		require.NoError(t, err)
		require.Equal(t, beforeCount+1, afterCount)
	})
}

func TestEnvironmentsService_CreateEnvironment_RequiresEntryValue(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestEnvironmentService(t)

	_, err := ti.service.CreateEnvironment(ctx, &gen.CreateEnvironmentPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		OrganizationID:   "",
		Name:             "missing-value-env",
		Description:      nil,
		Entries: []*gen.EnvironmentEntryInput{
			{Name: "API_KEY", Value: nil, IsSecret: new(true)},
		},
	})
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
}

func TestEnvironmentsService_CreateEnvironment_AuditLog(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestEnvironmentService(t)
	description := "Create audit description"
	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionEnvironmentCreate)
	require.NoError(t, err)

	env, err := ti.service.CreateEnvironment(ctx, &gen.CreateEnvironmentPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		OrganizationID:   "",
		Name:             "audit-create-env",
		Description:      &description,
		Entries: []*gen.EnvironmentEntryInput{
			{Name: "API_KEY", Value: new("super-secret-create-value"), IsSecret: new(true)},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, env)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionEnvironmentCreate)
	require.NoError(t, err)
	require.Equal(t, string(audit.ActionEnvironmentCreate), record.Action)
	require.Equal(t, "environment", record.SubjectType)
	require.Equal(t, env.Name, record.SubjectDisplay)
	require.Equal(t, string(env.Slug), record.SubjectSlug)
	require.Nil(t, record.BeforeSnapshot)
	require.Nil(t, record.AfterSnapshot)
	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionEnvironmentCreate)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}
