package integrationcredentials_test

import (
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/integrationcredentials"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

const (
	column    = integrationcredentials.ColumnDeviceIntegrationConfigs
	plaintext = `{"api_key":"synthetic-secret"}`
)

var noCiphertext = pgtype.Text{String: "", Valid: false}

func TestCipher_SealsOnlyTheApplicationKeyCopyWhenNotEnrolled(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)

	sealed, err := ti.cipher.Seal(t.Context(), ti.orgID, column, []byte(plaintext))
	require.NoError(t, err)
	require.False(t, sealed.Org.Valid)
	require.NotContains(t, sealed.Legacy, "synthetic-secret")

	opened, err := ti.cipher.Open(t.Context(), ti.orgID, column, stored(sealed))
	require.NoError(t, err)
	require.JSONEq(t, plaintext, string(opened))
}

func TestCipher_SealsBothCopiesForAnEnrolledOrganization(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	ti.enroll(ti.orgID)

	sealed, err := ti.cipher.Seal(t.Context(), ti.orgID, column, []byte(plaintext))
	require.NoError(t, err)
	require.True(t, sealed.Org.Valid)
	require.True(t, strings.HasPrefix(sealed.Org.String, encryption.EnvelopePrefix))
	require.NotContains(t, sealed.Org.String, "synthetic-secret")

	legacy, err := ti.legacy.Decrypt(sealed.Legacy)
	require.NoError(t, err)
	require.JSONEq(t, plaintext, legacy, "the application-key copy is kept for rollback")

	opened, err := ti.cipher.Open(t.Context(), ti.orgID, column, integrationcredentials.Stored{Legacy: conv.ToPGText("not-a-ciphertext"), Org: sealed.Org})
	require.NoError(t, err)
	require.JSONEq(t, plaintext, string(opened), "an enrolled organization reads the organization copy")
}

func TestCipher_EnrolledReadsDoNotFallBackToTheApplicationKey(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	ti.enroll(ti.orgID)

	sealed, err := ti.cipher.Seal(t.Context(), ti.orgID, integrationcredentials.ColumnNetworkIngresses, []byte(plaintext))
	require.NoError(t, err)

	_, err = ti.cipher.Open(t.Context(), ti.orgID, column, stored(sealed))
	require.ErrorIs(t, err, integrationcredentials.ErrUnreadable, "a value moved to another column does not open, even with a valid application-key copy")
}

func TestCipher_TurningEnrollmentOffReadsTheApplicationKeyCopy(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	ti.enroll(ti.orgID)

	sealed, err := ti.cipher.Seal(t.Context(), ti.orgID, column, []byte(plaintext))
	require.NoError(t, err)
	ti.suspend(t, ti.orgID)
	ti.unenroll(ti.orgID)

	opened, err := ti.cipher.Open(t.Context(), ti.orgID, column, stored(sealed))
	require.NoError(t, err)
	require.JSONEq(t, plaintext, string(opened))

	resealed, err := ti.cipher.Seal(t.Context(), ti.orgID, column, []byte(plaintext))
	require.NoError(t, err)
	require.False(t, resealed.Org.Valid, "a write while not enrolled clears the organization copy")
}

func TestCipher_AnUnavailableOrganizationKeyIsNotUnreadable(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	ti.enroll(ti.orgID)

	sealed, err := ti.cipher.Seal(t.Context(), ti.orgID, column, []byte(plaintext))
	require.NoError(t, err)
	ti.suspend(t, ti.orgID)

	_, err = ti.cipher.Open(t.Context(), ti.orgID, column, stored(sealed))
	require.ErrorIs(t, err, orgencryption.ErrSuspended)
	require.NotErrorIs(t, err, integrationcredentials.ErrUnreadable, "callers retry rather than ask for new credentials")

	_, err = ti.cipher.Seal(t.Context(), ti.orgID, column, []byte(plaintext))
	require.ErrorIs(t, err, orgencryption.ErrSuspended, "an enrolled organization's writes fail closed")
}

func TestCipher_EnrolledRowWithoutAnOrganizationCopyReadsTheApplicationKey(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)

	sealed, err := ti.cipher.Seal(t.Context(), ti.orgID, column, []byte(plaintext))
	require.NoError(t, err)
	ti.enroll(ti.orgID)

	opened, err := ti.cipher.Open(t.Context(), ti.orgID, column, stored(sealed))
	require.NoError(t, err)
	require.JSONEq(t, plaintext, string(opened))
}

func TestCipher_WithoutOrganizationEncryptionIgnoresEnrollment(t *testing.T) {
	t.Parallel()

	features := &feature.InMemory{}
	features.SetFlag(feature.FlagOrgEncryptedIntegrationCredentials, feature.AnyDistinctID, true)
	cipher := integrationcredentials.New(testenv.NewLogger(t), testenv.NewEncryptionClient(t), nil, nil, features)

	require.False(t, cipher.Enrolled(t.Context(), "org_any"))
	sealed, err := cipher.Seal(t.Context(), "org_any", column, []byte(plaintext))
	require.NoError(t, err)
	require.False(t, sealed.Org.Valid)
}

func TestCipher_ReportsUnreadableAndMissingCredentials(t *testing.T) {
	t.Parallel()

	cipher := testenv.NewIntegrationCredentials(t)

	_, err := cipher.Open(t.Context(), "org_any", column, integrationcredentials.Stored{Legacy: conv.ToPGText("not-a-ciphertext"), Org: noCiphertext})
	require.ErrorIs(t, err, integrationcredentials.ErrUnreadable)

	_, err = cipher.Open(t.Context(), "org_any", column, integrationcredentials.Stored{Legacy: noCiphertext, Org: noCiphertext})
	require.ErrorIs(t, err, integrationcredentials.ErrMissing)
}
