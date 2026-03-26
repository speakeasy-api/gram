package environments_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/environments"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
)

func TestEnvironmentsService_UpdateEnvironment(t *testing.T) {
	t.Parallel()

	t.Run("update environment name and description", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestEnvironmentService(t)

		// Create initial environment
		env, err := ti.service.CreateEnvironment(ctx, &gen.CreateEnvironmentPayload{
			SessionToken:     nil,
			ProjectSlugInput: nil,
			OrganizationID:   "",
			Name:             "initial-env",
			Description:      new("Initial description"),
			Entries: []*gen.EnvironmentEntryInput{
				{Name: "KEY1", Value: "value1"},
			},
		})
		require.NoError(t, err)
		beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionEnvironmentUpdate)
		require.NoError(t, err)

		// Update environment
		updatedEnv, err := ti.service.UpdateEnvironment(ctx, &gen.UpdateEnvironmentPayload{
			SessionToken:     nil,
			ProjectSlugInput: nil,
			Slug:             env.Slug,
			Description:      new("Updated description"),
			Name:             new("updated-env"),
			EntriesToUpdate:  []*gen.EnvironmentEntryInput{},
			EntriesToRemove:  []string{},
		})
		require.NoError(t, err)
		require.NotNil(t, updatedEnv)

		require.Equal(t, env.ID, updatedEnv.ID)
		require.Equal(t, "updated-env", updatedEnv.Name)
		require.Equal(t, "Updated description", *updatedEnv.Description)
		require.Len(t, updatedEnv.Entries, 1)
		afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionEnvironmentUpdate)
		require.NoError(t, err)
		require.Equal(t, beforeCount+1, afterCount)
	})

	t.Run("update environment entries", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestEnvironmentService(t)

		// Create initial environment
		env, err := ti.service.CreateEnvironment(ctx, &gen.CreateEnvironmentPayload{
			SessionToken:     nil,
			ProjectSlugInput: nil,
			OrganizationID:   "",
			Name:             "test-env",
			Description:      nil,
			Entries: []*gen.EnvironmentEntryInput{
				{Name: "KEY1", Value: "value1"},
				{Name: "KEY2", Value: "value2"},
			},
		})
		require.NoError(t, err)
		beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionEnvironmentUpdate)
		require.NoError(t, err)

		// Update environment entries
		updatedEnv, err := ti.service.UpdateEnvironment(ctx, &gen.UpdateEnvironmentPayload{
			SessionToken:     nil,
			ProjectSlugInput: nil,
			Slug:             env.Slug,
			Description:      nil,
			Name:             nil,
			EntriesToUpdate: []*gen.EnvironmentEntryInput{
				{Name: "KEY1", Value: "updated-value1"},
				{Name: "KEY3", Value: "new-value3"},
			},
			EntriesToRemove: []string{"KEY2"},
		})
		require.NoError(t, err)
		require.NotNil(t, updatedEnv)

		require.Len(t, updatedEnv.Entries, 2)

		// Check that entries are properly updated
		entryMap := make(map[string]string)
		for _, entry := range updatedEnv.Entries {
			entryMap[entry.Name] = entry.Value
		}

		require.Contains(t, entryMap, "KEY1")
		require.Contains(t, entryMap, "KEY3")
		require.NotContains(t, entryMap, "KEY2")
		afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionEnvironmentUpdate)
		require.NoError(t, err)
		require.Equal(t, beforeCount+1, afterCount)
	})

	t.Run("update non-existent environment", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestEnvironmentService(t)
		beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionEnvironmentUpdate)
		require.NoError(t, err)

		_, err = ti.service.UpdateEnvironment(ctx, &gen.UpdateEnvironmentPayload{
			SessionToken:     nil,
			ProjectSlugInput: nil,
			Slug:             "non-existent",
			Description:      nil,
			Name:             nil,
			EntriesToUpdate:  []*gen.EnvironmentEntryInput{},
			EntriesToRemove:  []string{},
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "environment not found")
		afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionEnvironmentUpdate)
		require.NoError(t, err)
		require.Equal(t, beforeCount, afterCount)
	})
}

func TestEnvironmentsService_UpdateEnvironment_AuditLogRedactsValues(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestEnvironmentService(t)
	initialDescription := "Initial description"
	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionEnvironmentUpdate)
	require.NoError(t, err)

	env, err := ti.service.CreateEnvironment(ctx, &gen.CreateEnvironmentPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		OrganizationID:   "",
		Name:             "audit-update-env",
		Description:      &initialDescription,
		Entries: []*gen.EnvironmentEntryInput{
			{Name: "API_KEY", Value: "super-secret-before"},
			{Name: "UNCHANGED", Value: "keep-me-secret"},
		},
	})
	require.NoError(t, err)

	updatedDescription := "Updated description"
	updatedName := "audit-update-env-renamed"

	updatedEnv, err := ti.service.UpdateEnvironment(ctx, &gen.UpdateEnvironmentPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Slug:             env.Slug,
		Description:      &updatedDescription,
		Name:             &updatedName,
		EntriesToUpdate: []*gen.EnvironmentEntryInput{
			{Name: "API_KEY", Value: "super-secret-after"},
			{Name: "NEW_KEY", Value: "brand-new-secret"},
		},
		EntriesToRemove: []string{"UNCHANGED"},
	})
	require.NoError(t, err)
	require.NotNil(t, updatedEnv)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionEnvironmentUpdate)
	require.NoError(t, err)
	require.Equal(t, string(audit.ActionEnvironmentUpdate), record.Action)
	require.Equal(t, "environment", record.SubjectType)
	require.Equal(t, updatedEnv.Name, record.SubjectDisplay)
	require.Equal(t, string(updatedEnv.Slug), record.SubjectSlug)
	require.NotNil(t, record.BeforeSnapshot)
	require.NotNil(t, record.AfterSnapshot)

	beforeRaw := string(record.BeforeSnapshot)
	afterRaw := string(record.AfterSnapshot)
	require.NotContains(t, beforeRaw, "super-secret-before")
	require.NotContains(t, beforeRaw, "keep-me-secret")
	require.NotContains(t, afterRaw, "super-secret-after")
	require.NotContains(t, afterRaw, "brand-new-secret")

	beforeSnapshot, err := audittest.DecodeAuditData(record.BeforeSnapshot)
	require.NoError(t, err)
	afterSnapshot, err := audittest.DecodeAuditData(record.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, env.Name, beforeSnapshot["Name"])
	require.Equal(t, updatedEnv.Name, afterSnapshot["Name"])

	beforeEntries, ok := beforeSnapshot["Entries"].([]any)
	require.True(t, ok)
	require.Len(t, beforeEntries, 2)
	for _, entry := range beforeEntries {
		entryMap, ok := entry.(map[string]any)
		require.True(t, ok)
		value, ok := entryMap["Value"].(string)
		require.True(t, ok)
		require.Contains(t, value, "*")
	}

	afterEntries, ok := afterSnapshot["Entries"].([]any)
	require.True(t, ok)
	require.Len(t, afterEntries, 2)

	entryNames := make(map[string]bool, len(afterEntries))
	for _, entry := range afterEntries {
		entryMap, ok := entry.(map[string]any)
		require.True(t, ok)
		value, ok := entryMap["Value"].(string)
		require.True(t, ok)
		require.Contains(t, value, "*")
		entryName, ok := entryMap["Name"].(string)
		require.True(t, ok)
		entryNames[entryName] = true
	}

	require.Contains(t, entryNames, "API_KEY")
	require.Contains(t, entryNames, "NEW_KEY")
	require.NotContains(t, entryNames, "UNCHANGED")
	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionEnvironmentUpdate)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}
