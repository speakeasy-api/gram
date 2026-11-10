package integrationcredentials_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	devicerepo "github.com/speakeasy-api/gram/server/internal/deviceintegrations/repo"
	"github.com/speakeasy-api/gram/server/internal/integrationcredentials"
	"github.com/speakeasy-api/gram/server/internal/integrationcredentials/repo"
	ingressrepo "github.com/speakeasy-api/gram/server/internal/networkingress/repo"
	slackrepo "github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

const deviceProvider = "jamf"

type credentialRows struct {
	organizationID string
	slack, ingress uuid.UUID
}

// insertCredentialRows writes one live row per table holding only an
// application-key ciphertext, as rows written before enrolment do.
func (ti *testInstance) insertCredentialRows(t *testing.T, organizationID string, legacy string) credentialRows {
	t.Helper()

	slack, err := slackrepo.New(ti.conn).CreateSlackDirectoryConnection(t.Context(), slackrepo.CreateSlackDirectoryConnectionParams{
		OrganizationID:          organizationID,
		SlackTeamID:             "T" + uuid.NewString(),
		SlackTeamName:           conv.ToPGText("Example workspace"),
		CredentialsEncrypted:    conv.ToPGText(legacy),
		CredentialsOrgEncrypted: noCiphertext,
		GrantedScopes:           []string{},
		Generation:              uuid.New(),
	})
	require.NoError(t, err)

	_, err = devicerepo.New(ti.conn).InsertConfig(t.Context(), devicerepo.InsertConfigParams{
		OrganizationID:          organizationID,
		Provider:                deviceProvider,
		CredentialsEncrypted:    legacy,
		CredentialsOrgEncrypted: noCiphertext,
		Settings:                []byte("{}"),
		Enabled:                 true,
	})
	require.NoError(t, err)

	ingress, err := ingressrepo.New(ti.conn).CreateNetworkIngress(t.Context(), ingressrepo.CreateNetworkIngressParams{
		ID:                      uuid.New(),
		OrganizationID:          organizationID,
		Provider:                "tailscale",
		Hostname:                "private",
		EndpointNamespaceKind:   "platform",
		CustomDomainID:          uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Enabled:                 true,
		IdentityRequired:        false,
		CredentialsEncrypted:    conv.ToPGText(legacy),
		CredentialsOrgEncrypted: noCiphertext,
		AttestorNamespace:       "attestor-" + uuid.NewString(),
		AttestorServiceAccount:  "attestor-sa",
		ProviderResources:       []byte("{}"),
	})
	require.NoError(t, err)

	return credentialRows{organizationID: organizationID, slack: slack.ID, ingress: ingress.ID}
}

type storedRow struct {
	column    integrationcredentials.Column
	stored    integrationcredentials.Stored
	updatedAt pgtype.Timestamptz
}

// read returns the stored credentials of the rows, in table order.
func (ti *testInstance) read(t *testing.T, rows credentialRows) []storedRow {
	t.Helper()

	slack, err := slackrepo.New(ti.conn).GetSlackDirectoryConnection(t.Context(), slackrepo.GetSlackDirectoryConnectionParams{OrganizationID: rows.organizationID, ID: rows.slack})
	require.NoError(t, err)
	device, err := devicerepo.New(ti.conn).GetConfigByOrgAndProvider(t.Context(), devicerepo.GetConfigByOrgAndProviderParams{OrganizationID: rows.organizationID, Provider: deviceProvider})
	require.NoError(t, err)
	ingress, err := ingressrepo.New(ti.conn).GetNetworkIngressByID(t.Context(), ingressrepo.GetNetworkIngressByIDParams{ID: rows.ingress, OrganizationID: rows.organizationID})
	require.NoError(t, err)

	return []storedRow{
		{column: integrationcredentials.ColumnSlackDirectoryConnections, stored: integrationcredentials.Stored{Legacy: slack.CredentialsEncrypted, Org: slack.CredentialsOrgEncrypted}, updatedAt: slack.UpdatedAt},
		{column: integrationcredentials.ColumnDeviceIntegrationConfigs, stored: integrationcredentials.Stored{Legacy: conv.ToPGText(device.CredentialsEncrypted), Org: device.CredentialsOrgEncrypted}, updatedAt: device.UpdatedAt},
		{column: integrationcredentials.ColumnNetworkIngresses, stored: integrationcredentials.Stored{Legacy: ingress.CredentialsEncrypted, Org: ingress.CredentialsOrgEncrypted}, updatedAt: ingress.UpdatedAt},
	}
}

func (ti *testInstance) legacyCiphertext(t *testing.T) string {
	t.Helper()

	ciphertext, err := ti.legacy.Encrypt([]byte(plaintext))
	require.NoError(t, err)

	return ciphertext
}

func TestBackfiller_SealsOnlyEnrolledOrganizations(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	enrolled := ti.insertCredentialRows(t, ti.orgID, ti.legacyCiphertext(t))
	notEnrolled := ti.insertCredentialRows(t, createOrganization(t, ti.conn), ti.legacyCiphertext(t))
	ti.enroll(ti.orgID)
	before := ti.read(t, enrolled)

	result, err := integrationcredentials.NewBackfiller(testenv.NewLogger(t), ti.conn, ti.cipher).Run(t.Context())
	require.NoError(t, err)
	require.Equal(t, integrationcredentials.BackfillResult{Organizations: 1, Sealed: 3, Unreadable: 0, Failed: 0}, result)

	for i, after := range ti.read(t, enrolled) {
		require.True(t, after.stored.Org.Valid, after.column)
		require.Equal(t, before[i].stored.Legacy, after.stored.Legacy, "%s keeps its application-key copy", after.column)
		require.Equal(t, before[i].updatedAt, after.updatedAt, "%s is not presented as a changed row", after.column)

		opened, err := ti.cipher.Open(t.Context(), ti.orgID, after.column, integrationcredentials.Stored{Legacy: conv.ToPGText("not-a-ciphertext"), Org: after.stored.Org})
		require.NoError(t, err, after.column)
		require.JSONEq(t, plaintext, string(opened), after.column)
	}

	for _, row := range ti.read(t, notEnrolled) {
		require.False(t, row.stored.Org.Valid, row.column)
	}

	again, err := integrationcredentials.NewBackfiller(testenv.NewLogger(t), ti.conn, ti.cipher).Run(t.Context())
	require.NoError(t, err)
	require.Equal(t, 0, again.Organizations+again.Sealed, "a second run has nothing left to seal")
}

func TestBackfiller_SkipsUnreadableAndDeletedRows(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	rows := ti.insertCredentialRows(t, ti.orgID, ti.legacyCiphertext(t))
	slack, err := slackrepo.New(ti.conn).GetSlackDirectoryConnection(t.Context(), slackrepo.GetSlackDirectoryConnectionParams{OrganizationID: ti.orgID, ID: rows.slack})
	require.NoError(t, err)
	_, err = slackrepo.New(ti.conn).UpdateSlackDirectoryCredentials(t.Context(), slackrepo.UpdateSlackDirectoryCredentialsParams{
		CredentialsEncrypted: conv.ToPGText("not-a-ciphertext"), CredentialsOrgEncrypted: noCiphertext, OrganizationID: ti.orgID, ID: rows.slack, Generation: slack.Generation,
	})
	require.NoError(t, err)
	require.NoError(t, devicerepo.New(ti.conn).SoftDeleteConfig(t.Context(), devicerepo.SoftDeleteConfigParams{OrganizationID: ti.orgID, Provider: deviceProvider}))
	ti.enroll(ti.orgID)

	result, err := integrationcredentials.NewBackfiller(testenv.NewLogger(t), ti.conn, ti.cipher).Run(t.Context())
	require.NoError(t, err)
	require.Equal(t, integrationcredentials.BackfillResult{Organizations: 1, Sealed: 1, Unreadable: 1, Failed: 0}, result, "only the network ingress row is sealed")

	ingress, err := ingressrepo.New(ti.conn).GetNetworkIngressByID(t.Context(), ingressrepo.GetNetworkIngressByIDParams{ID: rows.ingress, OrganizationID: ti.orgID})
	require.NoError(t, err)
	require.True(t, ingress.CredentialsOrgEncrypted.Valid)
}

func TestBackfiller_ContinuesPastAnOrganizationWhoseKeyIsUnavailable(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	otherOrg := createOrganization(t, ti.conn)
	ti.insertCredentialRows(t, ti.orgID, ti.legacyCiphertext(t))
	healthy := ti.insertCredentialRows(t, otherOrg, ti.legacyCiphertext(t))
	ti.enroll(ti.orgID)
	ti.enroll(otherOrg)

	sealed, err := ti.cipher.Seal(t.Context(), ti.orgID, column, []byte(plaintext))
	require.NoError(t, err)
	require.True(t, sealed.Org.Valid)
	ti.suspend(t, ti.orgID)

	result, err := integrationcredentials.NewBackfiller(testenv.NewLogger(t), ti.conn, ti.cipher).Run(t.Context())
	require.NoError(t, err)
	require.Equal(t, integrationcredentials.BackfillResult{Organizations: 2, Sealed: 3, Unreadable: 0, Failed: 1}, result)
	for _, row := range ti.read(t, healthy) {
		require.True(t, row.stored.Org.Valid, row.column)
	}
}

func TestBackfiller_DoesNotOverwriteARotatedCredential(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	read := ti.legacyCiphertext(t)
	rows := ti.insertCredentialRows(t, ti.orgID, read)
	_, err := ingressrepo.New(ti.conn).RotateNetworkIngressCredentials(t.Context(), ingressrepo.RotateNetworkIngressCredentialsParams{
		CredentialsEncrypted: conv.ToPGText(ti.legacyCiphertext(t)), CredentialsOrgEncrypted: noCiphertext, OrganizationID: ti.orgID,
	})
	require.NoError(t, err)

	affected, err := repo.New(ti.conn).SealNetworkIngressCredentials(t.Context(), repo.SealNetworkIngressCredentialsParams{
		CredentialsOrgEncrypted: conv.ToPGText("stale"),
		OrganizationID:          ti.orgID,
		ID:                      rows.ingress,
		CredentialsEncrypted:    conv.ToPGText(read),
	})
	require.NoError(t, err)
	require.Zero(t, affected, "a seal computed from a superseded credential is discarded")
}

func TestBackfiller_DoesNothingWithoutOrganizationEncryption(t *testing.T) {
	t.Parallel()

	ti := newTestInstance(t)
	rows := ti.insertCredentialRows(t, ti.orgID, ti.legacyCiphertext(t))
	ti.enroll(ti.orgID)

	result, err := integrationcredentials.NewBackfiller(testenv.NewLogger(t), ti.conn, testenv.NewIntegrationCredentials(t)).Run(t.Context())
	require.NoError(t, err)
	require.Equal(t, integrationcredentials.BackfillResult{Organizations: 0, Sealed: 0, Unreadable: 0, Failed: 0}, result)
	for _, row := range ti.read(t, rows) {
		require.False(t, row.stored.Org.Valid, row.column)
	}
}
