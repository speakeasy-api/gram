package environments_test

import (
	"crypto/rand"
	"encoding/base64"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/environments"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/environments/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// newOtherKeyClient returns an encryption client whose key differs from the
// one the test service writes with, so any decryption through it fails.
func newOtherKeyClient(t *testing.T) *encryption.Client {
	t.Helper()

	key := make([]byte, encryption.DataKeyBytes)
	_, err := rand.Read(key)
	require.NoError(t, err)

	enc, err := encryption.NewWithBytes(key)
	require.NoError(t, err)

	return enc
}

func entriesByName(entries []*types.EnvironmentEntry) map[string]*types.EnvironmentEntry {
	byName := make(map[string]*types.EnvironmentEntry, len(entries))
	for _, entry := range entries {
		byName[entry.Name] = entry
	}

	return byName
}

func TestEnvironmentsService_ManagementReadsDoNotDecrypt(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestEnvironmentService(t)

	source, err := ti.service.CreateEnvironment(ctx, &gen.CreateEnvironmentPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		OrganizationID:   "",
		Name:             "no-decrypt-source",
		Description:      nil,
		Entries: []*gen.EnvironmentEntryInput{
			{Name: "API_KEY", Value: new("super-secret-value"), IsSecret: new(true)},
			{Name: "REGION", Value: new("us-east-1"), IsSecret: new(false)},
		},
	})
	require.NoError(t, err)

	placeholders, err := ti.service.CloneEnvironment(ctx, &gen.CloneEnvironmentPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Slug:             source.Slug,
		NewName:          "no-decrypt-placeholders",
		CopyValues:       nil,
	})
	require.NoError(t, err)

	otherService, otherEntries := ti.newService(newOtherKeyClient(t))

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	_, err = otherEntries.ListEnvironmentEntries(ctx, *authCtx.ProjectID, uuid.MustParse(source.ID))
	require.Error(t, err, "the other key must not decrypt the stored secrets")

	result, err := otherService.ListEnvironments(ctx, &gen.ListEnvironmentsPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Len(t, result.Environments, 2)

	environmentsByID := make(map[string]*types.Environment, len(result.Environments))
	for _, env := range result.Environments {
		environmentsByID[env.ID] = env
	}

	sourceEntries := entriesByName(environmentsByID[source.ID].Entries)
	require.Len(t, sourceEntries, 2)
	require.Equal(t, "***", sourceEntries["API_KEY"].Value)
	require.True(t, sourceEntries["API_KEY"].HasValue)
	require.Equal(t, "us-east-1", sourceEntries["REGION"].Value)
	require.True(t, sourceEntries["REGION"].HasValue)

	placeholderEntries := entriesByName(environmentsByID[placeholders.ID].Entries)
	require.Len(t, placeholderEntries, 2)
	for name, entry := range placeholderEntries {
		require.True(t, entry.IsSecret, "entry %q should be a secret placeholder", name)
		require.Equal(t, "<EMPTY>", entry.Value, "entry %q should be the empty placeholder", name)
		require.False(t, entry.HasValue, "entry %q should have no value", name)
	}
}

func TestEnvironmentsService_UpdateEnvironment_AuditSnapshotsDoNotDecrypt(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestEnvironmentService(t)

	env, err := ti.service.CreateEnvironment(ctx, &gen.CreateEnvironmentPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		OrganizationID:   "",
		Name:             "no-decrypt-update",
		Description:      nil,
		Entries: []*gen.EnvironmentEntryInput{
			{Name: "API_KEY", Value: new("super-secret-value"), IsSecret: new(true)},
		},
	})
	require.NoError(t, err)

	otherService, _ := ti.newService(newOtherKeyClient(t))

	// No entries change, so the update itself has nothing to decrypt; only the
	// before and after snapshots read the stored entries.
	updated, err := otherService.UpdateEnvironment(ctx, &gen.UpdateEnvironmentPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Slug:             env.Slug,
		Description:      new("renamed through another key"),
		Name:             nil,
		EntriesToUpdate:  nil,
		EntriesToRemove:  nil,
	})
	require.NoError(t, err)
	require.Len(t, updated.Entries, 1)
	require.Equal(t, "***", updated.Entries[0].Value)
	require.True(t, updated.Entries[0].HasValue)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionEnvironmentUpdate)
	require.NoError(t, err)

	for _, raw := range [][]byte{record.BeforeSnapshot, record.AfterSnapshot} {
		snapshot, err := audittest.DecodeAuditData(raw)
		require.NoError(t, err)

		entries, ok := snapshot["Entries"].([]any)
		require.True(t, ok)
		require.Len(t, entries, 1)

		entry, ok := entries[0].(map[string]any)
		require.True(t, ok)
		require.Equal(t, "***", entry["Value"])
		require.Equal(t, true, entry["HasValue"])
	}
}

func TestEnvironmentsService_ListEnvironments_FailsOnMalformedSecret(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		value string
	}{
		{name: "invalid base64", value: "not ciphertext!"},
		{name: "shorter than nonce and tag", value: base64.StdEncoding.EncodeToString(make([]byte, 27))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestEnvironmentService(t)

			env, err := ti.service.CreateEnvironment(ctx, &gen.CreateEnvironmentPayload{
				SessionToken:     nil,
				ProjectSlugInput: nil,
				OrganizationID:   "",
				Name:             "malformed-secret",
				Description:      nil,
				Entries:          []*gen.EnvironmentEntryInput{},
			})
			require.NoError(t, err)

			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			require.NotNil(t, authCtx.ProjectID)

			_, err = repo.New(ti.conn).UpsertEnvironmentEntry(ctx, repo.UpsertEnvironmentEntryParams{
				Name:          "BROKEN",
				Value:         tc.value,
				IsSecret:      true,
				EnvironmentID: uuid.MustParse(env.ID),
				ProjectID:     *authCtx.ProjectID,
			})
			require.NoError(t, err)

			_, err = ti.service.ListEnvironments(ctx, &gen.ListEnvironmentsPayload{
				SessionToken:     nil,
				ProjectSlugInput: nil,
			})
			require.ErrorIs(t, err, encryption.ErrMalformedCiphertext)
			require.Contains(t, oops.Detail(err), "BROKEN")
		})
	}
}
