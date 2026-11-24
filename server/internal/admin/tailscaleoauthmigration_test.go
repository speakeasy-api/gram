package admin

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/k8s"
	"github.com/speakeasy-api/gram/server/internal/networkingress/ingresscredentials"
	networkingressrepo "github.com/speakeasy-api/gram/server/internal/networkingress/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/orgencryption/orgencryptiontest"
	orgencryptionrepo "github.com/speakeasy-api/gram/server/internal/orgencryption/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestTailscaleOAuthMigrationIsUnavailableWithoutOrganizationEncryption(t *testing.T) {
	t.Parallel()

	ctx, svc, _ := newTestAdminService(t)

	_, err := svc.ListTailscaleOAuthMigration(ctx, &gen.ListTailscaleOAuthMigrationPayload{})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnavailable, oopsErr.Code)
}

func TestTailscaleOAuthMigrationMovesAnOrganizationToItsKey(t *testing.T) {
	t.Parallel()

	ctx, svc, db := newTestAdminService(t)
	ctx = contextvalues.SetAdminAuthContext(ctx, &contextvalues.AdminAuthContext{OIDCSubject: "staff-test", Name: "Staff Test"})
	orgID := "org_" + uuid.NewString()
	seedOrg(t, ctx, db, orgFixture{id: orgID, name: "tailscale-oauth-migration", slug: "tailscale-oauth-migration-" + uuid.NewString()[:8], accountType: "enterprise"})

	fixture := orgencryptiontest.New(t, db)
	platform := testenv.NewEncryptionClient(t)
	transitions := orgencryption.NewTransitions(testenv.NewLogger(t), db, audit.NewLogger(), fixture.Provisioner, orgencryption.TransitionsConfig{ClearSettleDelay: time.Millisecond})
	cipher := orgencryption.NewCipher(testenv.NewLogger(t), platform, fixture.Service, transitions)
	svc.SetTailscaleOAuthMigration(&TailscaleOAuthMigration{
		Transitions: transitions,
		Runner:      orgencryption.NewRunner(testenv.NewLogger(t), db, cipher, testenv.NewMeterProvider(t)),
		Store:       ingresscredentials.NewCopyStore(db),
	})

	ingressID := uuid.New()
	names, err := k8s.NewNetworkIngressResourceNames(ingressID)
	require.NoError(t, err)
	resources, err := names.Marshal()
	require.NoError(t, err)
	_, err = networkingressrepo.New(db).CreateNetworkIngress(ctx, networkingressrepo.CreateNetworkIngressParams{
		ID: ingressID, OrganizationID: orgID, Provider: "tailscale", Hostname: "speakeasy",
		EndpointNamespaceKind: "platform", CustomDomainID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Enabled: true, IdentityRequired: false,
		AttestorNamespace: names.Namespace, AttestorServiceAccount: names.AttestorServiceAccount, ProviderResources: resources,
		Runtime: pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)
	credentials := ingresscredentials.New(testenv.NewLogger(t), cipher)
	_, err = credentials.Set(ctx, db, ingressID, orgID, []byte(`{"client_id":"id","client_secret":"secret"}`))
	require.NoError(t, err)

	list, err := svc.ListTailscaleOAuthMigration(ctx, &gen.ListTailscaleOAuthMigrationPayload{})
	require.NoError(t, err)
	require.Len(t, list.Organizations, 1)
	require.Equal(t, orgID, list.Organizations[0].OrganizationID)
	require.Equal(t, "platform_key", list.Organizations[0].State)
	require.Equal(t, int64(1), list.Organizations[0].Waiting)

	run := func(step string) (*gen.AdminTailscaleOAuthMigrationOrganization, error) {
		return svc.RunTailscaleOAuthMigrationStep(ctx, &gen.RunTailscaleOAuthMigrationStepPayload{OrganizationID: orgID, Step: step})
	}

	status, err := run("enroll")
	require.NoError(t, err)
	require.Equal(t, "dual_write", status.State)

	status, err = run("backfill")
	require.NoError(t, err)
	require.Equal(t, int64(0), status.Waiting)
	require.NotNil(t, status.LastVerifiedAt)
	require.NotNil(t, status.VerificationFailures)
	require.Equal(t, int64(0), *status.VerificationFailures)

	_, err = run("finalize")
	var blocked *oops.ShareableError
	require.ErrorAs(t, err, &blocked)
	require.Equal(t, oops.CodeConflict, blocked.Code, "finalize waits out the minimum time in dual_write")

	err = orgencryptionrepo.New(db).SetEncryptionTransitionStateChangedAt(ctx, orgencryptionrepo.SetEncryptionTransitionStateChangedAtParams{
		StateChangedAt: pgtype.Timestamptz{Time: time.Now().Add(-25 * time.Hour), InfinityModifier: pgtype.Finite, Valid: true},
		OrganizationID: orgID,
		UseCase:        string(ingresscredentials.UseCase),
	})
	require.NoError(t, err)
	_, err = run("backfill")
	require.NoError(t, err, "a clean verification after moving the clock back")

	status, err = run("finalize")
	require.NoError(t, err)
	require.Equal(t, "org_kms", status.State)

	status, err = run("clear_platform_copies")
	require.NoError(t, err)
	require.Equal(t, int64(0), status.PlatformCopies)
	require.NotNil(t, status.PlatformCopiesClearedAt)

	row, err := networkingressrepo.New(db).GetNetworkIngressByID(ctx, networkingressrepo.GetNetworkIngressByIDParams{ID: ingressID, OrganizationID: orgID})
	require.NoError(t, err)
	require.False(t, row.CredentialsEncrypted.Valid)
	opened, err := credentials.Open(ctx, db, orgID, ingresscredentials.Stored(row))
	require.NoError(t, err)
	require.JSONEq(t, `{"client_id":"id","client_secret":"secret"}`, string(opened))
}

func TestTailscaleOAuthMigrationBackfillReportsVerificationFailures(t *testing.T) {
	t.Parallel()

	ctx, svc, db := newTestAdminService(t)
	ctx = contextvalues.SetAdminAuthContext(ctx, &contextvalues.AdminAuthContext{OIDCSubject: "staff-test", Name: "Staff Test"})
	orgID := "org_" + uuid.NewString()
	seedOrg(t, ctx, db, orgFixture{id: orgID, name: "tailscale-oauth-mismatch", slug: "tailscale-oauth-mismatch-" + uuid.NewString()[:8], accountType: "enterprise"})

	fixture := orgencryptiontest.New(t, db)
	platform := testenv.NewEncryptionClient(t)
	transitions := orgencryption.NewTransitions(testenv.NewLogger(t), db, audit.NewLogger(), fixture.Provisioner, orgencryption.TransitionsConfig{ClearSettleDelay: time.Millisecond})
	cipher := orgencryption.NewCipher(testenv.NewLogger(t), platform, fixture.Service, transitions)
	svc.SetTailscaleOAuthMigration(&TailscaleOAuthMigration{
		Transitions: transitions,
		Runner:      orgencryption.NewRunner(testenv.NewLogger(t), db, cipher, testenv.NewMeterProvider(t)),
		Store:       ingresscredentials.NewCopyStore(db),
	})

	ingressID := uuid.New()
	names, err := k8s.NewNetworkIngressResourceNames(ingressID)
	require.NoError(t, err)
	resources, err := names.Marshal()
	require.NoError(t, err)
	_, err = networkingressrepo.New(db).CreateNetworkIngress(ctx, networkingressrepo.CreateNetworkIngressParams{
		ID: ingressID, OrganizationID: orgID, Provider: "tailscale", Hostname: "speakeasy",
		EndpointNamespaceKind: "platform", CustomDomainID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Enabled: true, IdentityRequired: false,
		AttestorNamespace: names.Namespace, AttestorServiceAccount: names.AttestorServiceAccount, ProviderResources: resources,
		Runtime: pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)
	platformCopy, err := platform.Encrypt([]byte(`{"client_id":"id","client_secret":"secret"}`))
	require.NoError(t, err)
	_, err = networkingressrepo.New(db).SetNetworkIngressCredentialCopiesFixture(ctx, networkingressrepo.SetNetworkIngressCredentialCopiesFixtureParams{
		PlatformCopy: pgtype.Text{String: platformCopy, Valid: true}, OrgCopy: pgtype.Text{String: "not an organization ciphertext", Valid: true},
		ID: ingressID, OrganizationID: orgID,
	})
	require.NoError(t, err)

	_, err = svc.RunTailscaleOAuthMigrationStep(ctx, &gen.RunTailscaleOAuthMigrationStepPayload{OrganizationID: orgID, Step: "enroll"})
	require.NoError(t, err)
	status, err := svc.RunTailscaleOAuthMigrationStep(ctx, &gen.RunTailscaleOAuthMigrationStepPayload{OrganizationID: orgID, Step: "backfill"})
	require.NoError(t, err, "a failed verification is not a failed step")
	require.NotNil(t, status.VerificationFailures)
	require.Equal(t, int64(1), *status.VerificationFailures)
	require.Nil(t, status.LastVerifiedAt, "no clean verification is recorded")
}
