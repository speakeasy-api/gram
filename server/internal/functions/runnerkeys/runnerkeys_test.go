package runnerkeys_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/functions/runnerkeys"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestCreate_PlatformKeyStoresOnlyThePlatformCopy(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	fn := ti.createFunction(t, ti.orgID, ti.createProject(t, ti.orgID))

	id := ti.create(t, fn)

	row := ti.stored(t, id)
	require.Equal(t, conv.ToPGText(ti.orgID), row.OrganizationID, "the row stores the project's organization")
	require.Equal(t, fn.functionID, row.FunctionID)
	require.Equal(t, conv.ToPGText("v01"), row.BearerFormat)
	require.False(t, row.EncryptionKeyOrgEncrypted.Valid, "platform_key writes no organization-key copy")

	key := ti.open(t, ti.keys, id)
	require.Len(t, key, 32)
	platform, err := ti.platform.Decrypt(string(row.EncryptionKey.Reveal()))
	require.NoError(t, err)
	require.Equal(t, key, []byte(platform), "the platform-key copy is the key runners already hold")
}

func TestCreate_DualWriteStoresBothCopies(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	ti.enroll(t)
	fn := ti.createFunction(t, ti.orgID, ti.createProject(t, ti.orgID))

	id := ti.create(t, fn)

	row := ti.stored(t, id)
	require.NotEmpty(t, row.EncryptionKey.Reveal())
	require.True(t, row.EncryptionKeyOrgEncrypted.Valid)

	key := ti.open(t, ti.keys, id)
	platform, err := ti.platform.Decrypt(string(row.EncryptionKey.Reveal()))
	require.NoError(t, err)
	require.Equal(t, key, []byte(platform), "both copies hold the same key")
}

func TestLifecycle_KeysKeepOpeningThroughEveryTransition(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	project := ti.createProject(t, ti.orgID)
	id := ti.create(t, ti.createFunction(t, ti.orgID, project))
	key := ti.open(t, ti.keys, id)

	ti.enroll(t)
	result := ti.run(t)
	require.EqualValues(t, 1, result.Converted)
	require.True(t, ti.stored(t, id).EncryptionKeyOrgEncrypted.Valid, "the backfill seals the organization-key copy")
	require.Equal(t, key, ti.open(t, ti.keys, id), "the key does not change, so the deployed runner keeps working")

	ti.finalize(t)
	row := ti.stored(t, id)
	require.Empty(t, row.EncryptionKey.Reveal(), "finalize clears the platform-key copy")
	require.Equal(t, key, ti.open(t, ti.keys, id))
	require.Equal(t, key, ti.open(t, ti.otherProcessKeys(t), id), "another process reads org_kms from the database")

	// org_kms writes only the organization-key copy.
	later := ti.create(t, ti.createFunction(t, ti.orgID, project))
	laterRow := ti.stored(t, later)
	require.Empty(t, laterRow.EncryptionKey.Reveal())
	require.True(t, laterRow.EncryptionKeyOrgEncrypted.Valid)
	require.Len(t, ti.open(t, ti.keys, later), 32)
}

func TestOpen_RowWithoutOrganizationUsesThePlatformKey(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	project := ti.createProject(t, ti.orgID)
	id := ti.create(t, ti.createFunction(t, ti.orgID, project))
	key := ti.open(t, ti.keys, id)
	require.NoError(t, testrepo.New(ti.conn).ClearFunctionsAccessOrganizationFixture(t.Context(), id))

	ti.enroll(t)
	require.Equal(t, key, ti.open(t, ti.keys, id), "a row the organization backfill has not reached opens with the platform key")

	counts, err := runnerkeys.NewCopyStore(ti.conn).Count(t.Context(), ti.orgID)
	require.NoError(t, err)
	require.Equal(t, orgencryption.CopyCounts{Waiting: 0, Unreadable: 0, Unattributed: 1, PlatformCopies: 0}, counts,
		"the row keeps the organization from finalizing until the organization backfill fills it in")
}

func TestCreate_RefusesAProjectWithoutAnOrganization(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	// projects.organization_id has no foreign key, so a project can name an
	// organization with no metadata row. Its function's asset is stored under
	// a real organization, since assets reference organization_metadata.
	fn := ti.createFunction(t, ti.orgID, ti.createProject(t, "org_"+uuid.NewString()))

	_, err := ti.keys.Create(t.Context(), ti.conn, fn.projectID, fn.deploymentID, []uuid.UUID{fn.functionID})
	require.ErrorIs(t, err, runnerkeys.ErrNoOrganization)
}

func TestWithoutOrganizationEncryption_FailsClosedForEnrolledOrganizations(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	fn := ti.createFunction(t, ti.orgID, ti.createProject(t, ti.orgID))
	id := ti.create(t, fn)
	ti.enroll(t)

	plain := runnerkeys.WithoutOrganizationEncryption(testenv.NewLogger(t), ti.conn, ti.platform)
	_, err := plain.Open(t.Context(), storedKey(ti.stored(t, id)))
	require.ErrorIs(t, err, orgencryption.ErrOrganizationEncryptionUnavailable)
}
