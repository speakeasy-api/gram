package aiproviderkeys_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	aiintegrationsrepo "github.com/speakeasy-api/gram/server/internal/aiintegrations/repo"
	"github.com/speakeasy-api/gram/server/internal/aiproviderkeys"
	"github.com/speakeasy-api/gram/server/internal/conv"
	modelkeysrepo "github.com/speakeasy-api/gram/server/internal/modelkeys/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/orgencryption/orgencryptiontest"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	openrouterrepo "github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter/repo"
)

func TestQueriesFenceEveryKeyWrite(t *testing.T) {
	t.Parallel()

	for _, check := range []struct {
		path    string
		columns []string
	}{
		{path: "queries.sql", columns: []string{"api_key_encrypted", "api_key_org_encrypted", "key_encrypted", "key_org_encrypted"}},
		{path: "../aiintegrations/queries.sql", columns: []string{"api_key_encrypted", "api_key_org_encrypted"}},
		{path: "../modelkeys/queries.sql", columns: []string{"api_key_encrypted", "api_key_org_encrypted"}},
		{path: "../thirdparty/openrouter/queries.sql", columns: []string{"key_encrypted", "key_org_encrypted"}},
	} {
		queries, err := os.ReadFile(check.path)
		require.NoError(t, err)
		orgencryptiontest.RequireFencedWrites(t, string(queries), check.columns...)
	}
}

func TestErrorMapsKeyProblemsToAPICodes(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		err  error
		want oops.Code
	}{
		{err: fmt.Errorf("open: %w", orgencryption.ErrSuspended), want: oops.CodeFailedPrecondition},
		{err: fmt.Errorf("open: %w", orgencryption.ErrInvalidKey), want: oops.CodeFailedPrecondition},
		{err: fmt.Errorf("open: %w", orgencryption.ErrKeyUnavailable), want: oops.CodeUnavailable},
		{err: fmt.Errorf("open: %w", orgencryption.ErrUnreadable), want: oops.CodeUnexpected},
		{err: errors.New("connection reset"), want: oops.CodeUnexpected},
	} {
		require.Equal(t, tc.want, aiproviderkeys.Error(tc.err, "store key").Code, "%v", tc.err)
	}
}

func TestStoreInPlatformKeyWritesOnlyThePlatformCopy(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)

	require.NoError(t, ti.keys.Store(t.Context(), ti.conn, ti.orgID, aiproviderkeys.OpenRouterAPIKeys, []byte("sk-or-key"), ti.createKey("chat")))

	stored := ti.stored(t, "chat")
	require.True(t, stored.Platform.Valid)
	require.False(t, stored.Org.Valid)

	opened, err := ti.keys.Open(t.Context(), nil, ti.orgID, aiproviderkeys.OpenRouterAPIKeys, stored)
	require.NoError(t, err)
	require.Equal(t, "sk-or-key", string(opened))
}

func TestStoreInDualWriteWritesBothCopies(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	ti.enroll(t)

	require.NoError(t, ti.keys.Store(t.Context(), nil, ti.orgID, aiproviderkeys.OpenRouterAPIKeys, []byte("sk-or-key"), ti.createKey("chat")))

	stored := ti.stored(t, "chat")
	require.True(t, stored.Platform.Valid)
	require.True(t, stored.Org.Valid)

	ti.finalize(t)
	opened, err := ti.keys.Open(t.Context(), nil, ti.orgID, aiproviderkeys.OpenRouterAPIKeys, orgencryption.Copies{Org: stored.Org})
	require.NoError(t, err)
	require.Equal(t, "sk-or-key", string(opened))
}

func TestStoreAfterFinalizeWritesOnlyTheOrganizationCopy(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	ti.enroll(t)
	ti.finalize(t)

	require.NoError(t, ti.keys.Store(t.Context(), ti.conn, ti.orgID, aiproviderkeys.OpenRouterAPIKeys, []byte("sk-or-key"), ti.createKey("chat")))

	stored := ti.stored(t, "chat")
	require.False(t, stored.Platform.Valid)
	require.True(t, stored.Org.Valid)

	opened, err := ti.keys.Open(t.Context(), ti.conn, ti.orgID, aiproviderkeys.OpenRouterAPIKeys, stored)
	require.NoError(t, err)
	require.Equal(t, "sk-or-key", string(opened))
}

func TestStoreSealsAgainWhenTheTransitionMoves(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	write := ti.createKey("chat")
	attempts := 0

	err := ti.keys.Store(t.Context(), ti.conn, ti.orgID, aiproviderkeys.OpenRouterAPIKeys, []byte("sk-or-key"), func(ctx context.Context, copies orgencryption.Copies, generation int64) (bool, error) {
		attempts++
		if attempts == 1 {
			ti.enroll(t)
		}
		return write(ctx, copies, generation)
	})
	require.NoError(t, err)
	require.Equal(t, 2, attempts)

	stored := ti.stored(t, "chat")
	require.True(t, stored.Org.Valid, "the second attempt seals for dual_write")
}

func TestStoreReportsAWriteThatMatchesNoRow(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)

	err := ti.keys.Store(t.Context(), ti.conn, ti.orgID, aiproviderkeys.OpenRouterAPIKeys, []byte("sk-or-key"), func(context.Context, orgencryption.Copies, int64) (bool, error) {
		return false, nil
	})
	require.ErrorIs(t, err, aiproviderkeys.ErrNotStored)
}

func TestSuspendedKeyIsACustomerKeyProblemNotUnreadable(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	ti.enroll(t)
	require.NoError(t, ti.keys.Store(t.Context(), ti.conn, ti.orgID, aiproviderkeys.OpenRouterAPIKeys, []byte("sk-or-key"), ti.createKey("chat")))
	ti.finalize(t)
	require.NoError(t, ti.fixture.Provisioner.Suspend(t.Context(), ti.orgID, "customer disabled the key", orgencryption.SystemActor()))

	_, err := ti.keys.Open(t.Context(), ti.conn, ti.orgID, aiproviderkeys.OpenRouterAPIKeys, ti.stored(t, "chat"))
	require.Error(t, err)
	require.False(t, aiproviderkeys.IsUnreadable(err), "a suspended key never asks for a new key: %v", err)
	require.True(t, aiproviderkeys.IsCustomerKeyProblem(err), "%v", err)

	require.Error(t, ti.keys.RequireUsable(t.Context(), ti.conn, ti.orgID))

	err = ti.keys.Store(t.Context(), ti.conn, ti.orgID, aiproviderkeys.OpenRouterAPIKeys, []byte("sk-or-key"), ti.createKey("internal"))
	require.Error(t, err, "nothing falls back to the platform key")
}

func TestBackfillAndFinalizeMoveKeysToTheOrganizationKey(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	require.NoError(t, ti.keys.Store(t.Context(), ti.conn, ti.orgID, aiproviderkeys.OpenRouterAPIKeys, []byte("sk-or-chat"), ti.createKey("chat")))
	require.NoError(t, ti.keys.Store(t.Context(), ti.conn, ti.orgID, aiproviderkeys.OpenRouterAPIKeys, []byte("sk-or-internal"), ti.createKey("internal")))

	stores := aiproviderkeys.CopyStores(ti.conn)
	var waiting int64
	for _, store := range stores {
		counts, err := store.Count(t.Context(), ti.orgID)
		require.NoError(t, err)
		waiting += counts.Waiting
	}
	require.Equal(t, int64(2), waiting)

	ti.enroll(t)
	result, err := orgencryption.NewRunner(testenv.NewLogger(t), ti.conn, ti.cipher, testenv.NewMeterProvider(t)).Run(t.Context(), ti.orgID, orgencryption.UseCaseAIProviderKeys, stores)
	require.NoError(t, err)
	require.Equal(t, int64(2), result.Converted)

	ti.finalize(t)

	for keyType, want := range map[string]string{"chat": "sk-or-chat", "internal": "sk-or-internal"} {
		stored := ti.stored(t, keyType)
		require.False(t, stored.Platform.Valid, "the platform-key copy is cleared")
		require.True(t, stored.Org.Valid)

		opened, err := ti.keys.Open(t.Context(), ti.conn, ti.orgID, aiproviderkeys.OpenRouterAPIKeys, stored)
		require.NoError(t, err)
		require.Equal(t, want, string(opened))
	}
}

func TestCopyStoresReportValues(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	ctx := t.Context()
	project, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           "AI Provider Keys Test Project",
		Slug:           "ai-provider-keys-" + uuid.NewString()[:8],
		OrganizationID: ti.orgID,
	})
	require.NoError(t, err)

	none := pgtype.Text{String: "", Valid: false}

	// Each seed stores a platform-only value and an organization-only value
	// for the test organization, soft-deleting the first where the table
	// supports it and adding a row that holds neither copy, and returns the
	// latest updated_at of the rows storing a value.
	seeds := map[string]func(t *testing.T) time.Time{
		aiproviderkeys.AIIntegrationConfigs.PlatformColumn: func(t *testing.T) time.Time {
			t.Helper()

			q := aiintegrationsrepo.New(ti.conn)
			insert := func(provider string) uuid.UUID {
				row, err := q.InsertConfig(ctx, aiintegrationsrepo.InsertConfigParams{
					OrganizationID: ti.orgID, Provider: provider, ProjectID: project.ID,
					ExternalOrganizationID: none, Enabled: true, BillingMode: none, Settings: nil,
				})
				require.NoError(t, err)
				return row.ID
			}
			set := func(id uuid.UUID, platform, org pgtype.Text) time.Time {
				row, err := q.SetConfigAPIKey(ctx, aiintegrationsrepo.SetConfigAPIKeyParams{PlatformCopy: platform, OrgCopy: org, ID: id, OrganizationID: ti.orgID, DecidedGeneration: 0})
				require.NoError(t, err)
				return row.UpdatedAt.Time
			}

			deleted := set(insert("openai"), conv.ToPGText("platform-copy"), none)
			require.NoError(t, q.SoftDeleteConfig(ctx, aiintegrationsrepo.SoftDeleteConfigParams{OrganizationID: ti.orgID, Provider: "openai"}))
			live := set(insert("openai"), none, conv.ToPGText("org-copy"))
			insert("anthropic")

			return latest(deleted, live)
		},
		aiproviderkeys.ModelProviderKeys.PlatformColumn: func(t *testing.T) time.Time {
			t.Helper()

			q := modelkeysrepo.New(ti.conn)
			insert := func(slot string) uuid.UUID {
				row, err := q.InsertKey(ctx, modelkeysrepo.InsertKeyParams{OrganizationID: ti.orgID, ProjectID: project.ID, Slot: slot, Provider: "openrouter", Enabled: true})
				require.NoError(t, err)
				return row.ID
			}
			set := func(id uuid.UUID, platform, org pgtype.Text) time.Time {
				row, err := q.SetKeyAPIKey(ctx, modelkeysrepo.SetKeyAPIKeyParams{PlatformCopy: platform, OrgCopy: org, ID: id, OrganizationID: ti.orgID, DecidedGeneration: 0})
				require.NoError(t, err)
				return row.UpdatedAt.Time
			}

			deletedID := insert("playground")
			deleted := set(deletedID, conv.ToPGText("platform-copy"), none)
			_, err := q.SoftDeleteKeyByID(ctx, modelkeysrepo.SoftDeleteKeyByIDParams{ID: deletedID, ProjectID: project.ID})
			require.NoError(t, err)
			live := set(insert("assistants"), none, conv.ToPGText("org-copy"))
			insert("default")

			return latest(deleted, live)
		},
		aiproviderkeys.OpenRouterAPIKeys.PlatformColumn: func(t *testing.T) time.Time {
			t.Helper()

			create := func(keyType string, platform, org pgtype.Text) time.Time {
				row, err := openrouterrepo.New(ti.conn).CreateOpenRouterAPIKey(ctx, openrouterrepo.CreateOpenRouterAPIKeyParams{
					OrganizationID: ti.orgID, KeyType: keyType, KeyEncrypted: platform, KeyOrgEncrypted: org,
					KeyHash: "hash-" + keyType, MonthlyCredits: 10, DecidedGeneration: 0,
				})
				require.NoError(t, err)
				return row.UpdatedAt.Time
			}

			return latest(create("chat", conv.ToPGText("platform-copy"), none), create("internal", none, conv.ToPGText("org-copy")))
		},
	}

	emptyOrgID := "org_" + uuid.NewString()
	for _, store := range aiproviderkeys.CopyStores(ti.conn) {
		column := store.Columns().PlatformColumn
		seed, ok := seeds[column]
		require.True(t, ok, "no seed for %s", column)
		lastWrittenAt := seed(t)

		listed, err := store.ListOrganizationsWithValues(ctx)
		require.NoError(t, err)
		organization, ok := findOrganization(listed, ti.orgID)
		require.True(t, ok, "%s lists the organization", column)
		require.Equal(t, int64(2), organization.Values, column)
		require.True(t, lastWrittenAt.Equal(organization.LastWrittenAt), "%s: listed %v, stored %v", column, organization.LastWrittenAt, lastWrittenAt)
		_, ok = findOrganization(listed, emptyOrgID)
		require.False(t, ok, column)

		counted, err := store.Count(ctx, ti.orgID)
		require.NoError(t, err)
		require.Equal(t, organization.Values, counted.Values, column)
		require.True(t, organization.LastWrittenAt.Equal(counted.LastWrittenAt), column)

		empty, err := store.Count(ctx, emptyOrgID)
		require.NoError(t, err)
		require.Zero(t, empty.Values, column)
		require.True(t, empty.LastWrittenAt.IsZero(), column)
	}
}

func findOrganization(organizations []orgencryption.OrganizationValues, organizationID string) (orgencryption.OrganizationValues, bool) {
	for _, h := range organizations {
		if h.OrganizationID == organizationID {
			return h, true
		}
	}
	return orgencryption.OrganizationValues{}, false
}

func latest(times ...time.Time) time.Time {
	var last time.Time
	for _, at := range times {
		if at.After(last) {
			last = at
		}
	}
	return last
}
