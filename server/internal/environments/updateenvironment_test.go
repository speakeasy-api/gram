package environments_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/environments"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/oops"
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
				{Name: "KEY1", Value: new("value1"), IsSecret: new(true)},
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
				{Name: "KEY1", Value: new("value1"), IsSecret: new(true)},
				{Name: "KEY2", Value: new("value2"), IsSecret: new(true)},
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
				{Name: "KEY1", Value: new("updated-value1"), IsSecret: new(true)},
				{Name: "KEY3", Value: new("new-value3"), IsSecret: new(true)},
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

func TestEnvironmentsService_UpdateEnvironment_SecretToNonSecretRequiresValue(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestEnvironmentService(t)

	env, err := ti.service.CreateEnvironment(ctx, &gen.CreateEnvironmentPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		OrganizationID:   "",
		Name:             "flip-reveal-env",
		Description:      nil,
		Entries: []*gen.EnvironmentEntryInput{
			{Name: "API_KEY", Value: new("super-secret-value"), IsSecret: new(true)},
		},
	})
	require.NoError(t, err)

	// Flipping secret -> non-secret without a new value must be rejected:
	// allowing it would let environment write access read stored secrets back.
	_, err = ti.service.UpdateEnvironment(ctx, &gen.UpdateEnvironmentPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Slug:             env.Slug,
		Description:      nil,
		Name:             nil,
		EntriesToUpdate: []*gen.EnvironmentEntryInput{
			{Name: "API_KEY", Value: nil, IsSecret: new(false)},
		},
		EntriesToRemove: []string{},
	})
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)

	// The same flip with a fresh value succeeds and the value is readable.
	updatedEnv, err := ti.service.UpdateEnvironment(ctx, &gen.UpdateEnvironmentPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Slug:             env.Slug,
		Description:      nil,
		Name:             nil,
		EntriesToUpdate: []*gen.EnvironmentEntryInput{
			{Name: "API_KEY", Value: new("https://api.example.com"), IsSecret: new(false)},
		},
		EntriesToRemove: []string{},
	})
	require.NoError(t, err)
	require.Len(t, updatedEnv.Entries, 1)
	require.Equal(t, "https://api.example.com", updatedEnv.Entries[0].Value)
	require.False(t, updatedEnv.Entries[0].IsSecret)
}

func TestEnvironmentsService_UpdateEnvironment_NonSecretToSecretPreservesValue(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestEnvironmentService(t)

	env, err := ti.service.CreateEnvironment(ctx, &gen.CreateEnvironmentPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		OrganizationID:   "",
		Name:             "flip-hide-env",
		Description:      nil,
		Entries: []*gen.EnvironmentEntryInput{
			{Name: "BASE_URL", Value: new("https://api.example.com"), IsSecret: new(false)},
		},
	})
	require.NoError(t, err)
	require.Equal(t, "https://api.example.com", env.Entries[0].Value)

	// Flipping non-secret -> secret without a value encrypts the stored
	// plaintext in place.
	updatedEnv, err := ti.service.UpdateEnvironment(ctx, &gen.UpdateEnvironmentPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Slug:             env.Slug,
		Description:      nil,
		Name:             nil,
		EntriesToUpdate: []*gen.EnvironmentEntryInput{
			{Name: "BASE_URL", Value: nil, IsSecret: new(true)},
		},
		EntriesToRemove: []string{},
	})
	require.NoError(t, err)
	require.Len(t, updatedEnv.Entries, 1)
	require.True(t, updatedEnv.Entries[0].IsSecret)
	// The redaction prefix proves the original plaintext survived the flip.
	require.Equal(t, "htt*****", updatedEnv.Entries[0].Value)
}

func TestEnvironmentsService_UpdateEnvironment_OmittedValuePreservesSecret(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestEnvironmentService(t)

	env, err := ti.service.CreateEnvironment(ctx, &gen.CreateEnvironmentPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		OrganizationID:   "",
		Name:             "preserve-env",
		Description:      nil,
		Entries: []*gen.EnvironmentEntryInput{
			{Name: "API_KEY", Value: new("original-secret"), IsSecret: new(true)},
		},
	})
	require.NoError(t, err)

	updatedEnv, err := ti.service.UpdateEnvironment(ctx, &gen.UpdateEnvironmentPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Slug:             env.Slug,
		Description:      nil,
		Name:             nil,
		EntriesToUpdate: []*gen.EnvironmentEntryInput{
			{Name: "API_KEY", Value: nil, IsSecret: new(true)},
		},
		EntriesToRemove: []string{},
	})
	require.NoError(t, err)
	require.Len(t, updatedEnv.Entries, 1)
	require.True(t, updatedEnv.Entries[0].IsSecret)
	require.Equal(t, "ori*****", updatedEnv.Entries[0].Value)
}

func TestEnvironmentsService_UpdateEnvironment_OmittedFlagPreservesSecrecy(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestEnvironmentService(t)

	env, err := ti.service.CreateEnvironment(ctx, &gen.CreateEnvironmentPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		OrganizationID:   "",
		Name:             "omitted-flag-env",
		Description:      nil,
		Entries: []*gen.EnvironmentEntryInput{
			{Name: "API_KEY", Value: new("secret-value"), IsSecret: new(true)},
			{Name: "BASE_URL", Value: new("https://api.example.com"), IsSecret: new(false)},
		},
	})
	require.NoError(t, err)

	// Value-only updates without a flag keep each entry's current secrecy —
	// callers that predate is_secret must behave exactly as before.
	updatedEnv, err := ti.service.UpdateEnvironment(ctx, &gen.UpdateEnvironmentPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Slug:             env.Slug,
		Description:      nil,
		Name:             nil,
		EntriesToUpdate: []*gen.EnvironmentEntryInput{
			{Name: "API_KEY", Value: new("rotated-secret"), IsSecret: nil},
			{Name: "BASE_URL", Value: new("https://api2.example.com"), IsSecret: nil},
			{Name: "BRAND_NEW", Value: new("implicitly-secret"), IsSecret: nil},
		},
		EntriesToRemove: []string{},
	})
	require.NoError(t, err)

	entriesByName := make(map[string]struct {
		value    string
		isSecret bool
	}, len(updatedEnv.Entries))
	for _, entry := range updatedEnv.Entries {
		entriesByName[entry.Name] = struct {
			value    string
			isSecret bool
		}{value: entry.Value, isSecret: entry.IsSecret}
	}

	require.True(t, entriesByName["API_KEY"].isSecret)
	require.Equal(t, "rot*****", entriesByName["API_KEY"].value)
	require.False(t, entriesByName["BASE_URL"].isSecret)
	require.Equal(t, "https://api2.example.com", entriesByName["BASE_URL"].value)
	// New entries with no flag default to secret.
	require.True(t, entriesByName["BRAND_NEW"].isSecret)
	require.Equal(t, "imp*****", entriesByName["BRAND_NEW"].value)
}

func TestEnvironmentsService_UpdateEnvironment_NewEntryRequiresValue(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestEnvironmentService(t)

	env, err := ti.service.CreateEnvironment(ctx, &gen.CreateEnvironmentPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		OrganizationID:   "",
		Name:             "new-entry-env",
		Description:      nil,
		Entries:          []*gen.EnvironmentEntryInput{},
	})
	require.NoError(t, err)

	_, err = ti.service.UpdateEnvironment(ctx, &gen.UpdateEnvironmentPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Slug:             env.Slug,
		Description:      nil,
		Name:             nil,
		EntriesToUpdate: []*gen.EnvironmentEntryInput{
			{Name: "MISSING_VALUE", Value: nil, IsSecret: new(false)},
		},
		EntriesToRemove: []string{},
	})
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
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
			{Name: "API_KEY", Value: new("super-secret-before"), IsSecret: new(true)},
			{Name: "UNCHANGED", Value: new("keep-me-secret"), IsSecret: new(true)},
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
			{Name: "API_KEY", Value: new("super-secret-after"), IsSecret: new(true)},
			{Name: "NEW_KEY", Value: new("brand-new-secret"), IsSecret: new(true)},
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
