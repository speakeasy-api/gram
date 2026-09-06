package externalcredentials_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/external_credentials"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// Speakeasy staff dogfood the feature against a service account in Gram's own
// project, which is exactly what the screening refuses for everyone else.
func TestCreateGcpIamCredential_PlatformAdminExemptsGramProjectTarget(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	cred, err := ti.service.CreateGcpIamCredential(withAdmin(t, orgAdmin(t, ctx)), &gen.CreateGcpIamCredentialPayload{
		SessionToken:              nil,
		Name:                      "gcp-staff-dogfood",
		ImpersonateServiceAccount: gramProjectServiceAccount("internal"),
	})
	require.NoError(t, err)
	require.NotNil(t, cred)
	require.Equal(t, gramProjectServiceAccount("internal"), *cred.ImpersonateServiceAccount)

	requireSkipProjectVerification(t, ctx, ti, cred.ID, true)
}

// The exemption is not a blanket bypass of the screening: it forgives one
// refusal. An address that cannot be placed in a project was never compared
// against Gram's, so there is nothing to forgive.
func TestCreateGcpIamCredential_PlatformAdminStillRefusedMalformedTarget(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.CreateGcpIamCredential(withAdmin(t, orgAdmin(t, ctx)), &gen.CreateGcpIamCredentialPayload{
		SessionToken:              nil,
		Name:                      "gcp-staff-malformed",
		ImpersonateServiceAccount: "123456789012-compute@developer.gserviceaccount.com",
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

// An ordinary target needs no exemption, so a platform administrator's write
// must not stamp one on it. Recording the flag for every staff write would
// grant a standing bypass to rows that never asked for one.
func TestCreateGcpIamCredential_PlatformAdminRecordsNoExemptionForCustomerTarget(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	cred, err := ti.service.CreateGcpIamCredential(withAdmin(t, orgAdmin(t, ctx)), &gen.CreateGcpIamCredentialPayload{
		SessionToken:              nil,
		Name:                      "gcp-staff-ordinary-target",
		ImpersonateServiceAccount: "signer@customer-project.iam.gserviceaccount.com",
	})
	require.NoError(t, err)

	requireSkipProjectVerification(t, ctx, ti, cred.ID, false)
}

// The payload requires the impersonation target, so the dashboard resubmits it
// even when the operator only edited the name. Without carrying the exemption
// forward, an organization could never rename a credential staff had exempted.
func TestUpdateGcpIamCredential_OrgAdminRenamesExemptedCredential(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	target := gramProjectServiceAccount("internal")
	cred, err := ti.service.CreateGcpIamCredential(withAdmin(t, orgAdmin(t, ctx)), &gen.CreateGcpIamCredentialPayload{
		SessionToken:              nil,
		Name:                      "gcp-exempt-before-rename",
		ImpersonateServiceAccount: target,
	})
	require.NoError(t, err)

	renamed, err := ti.service.UpdateGcpIamCredential(orgAdmin(t, ctx), &gen.UpdateGcpIamCredentialPayload{
		ID:                        cred.ID,
		SessionToken:              nil,
		Name:                      "gcp-exempt-after-rename",
		ImpersonateServiceAccount: target,
	})
	require.NoError(t, err)
	require.Equal(t, "gcp-exempt-after-rename", renamed.Name)

	requireSkipProjectVerification(t, ctx, ti, cred.ID, true)
}

// Capitalization is not a new identity. Re-submitting the same account cased
// differently must not read as naming another one, which would re-impose the
// refusal on an edit that changed nothing.
func TestUpdateGcpIamCredential_ExemptionSurvivesCaseDifference(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	cred, err := ti.service.CreateGcpIamCredential(withAdmin(t, orgAdmin(t, ctx)), &gen.CreateGcpIamCredentialPayload{
		SessionToken:              nil,
		Name:                      "gcp-exempt-case",
		ImpersonateServiceAccount: gramProjectServiceAccount("internal"),
	})
	require.NoError(t, err)

	updated, err := ti.service.UpdateGcpIamCredential(orgAdmin(t, ctx), &gen.UpdateGcpIamCredentialPayload{
		ID:                        cred.ID,
		SessionToken:              nil,
		Name:                      "gcp-exempt-case",
		ImpersonateServiceAccount: gramProjectServiceAccount("INTERNAL"),
	})
	require.NoError(t, err)

	requireSkipProjectVerification(t, ctx, ti, cred.ID, true)

	// The caller may not change this target at all, so the approved spelling is
	// what persists. Writing their capitalization back would let an edit that is
	// required to be a no-op rewrite the identity the credential authenticates
	// as, and GCP resolves a service account by an id that is lowercase-only.
	require.Equal(t, gramProjectServiceAccount("internal"), *updated.ImpersonateServiceAccount)
}

// The exemption is pinned to the service account staff approved, so the edit
// path cannot be turned into a probe for other internal service accounts.
func TestUpdateGcpIamCredential_ExemptionDoesNotTransferToAnotherGramProjectTarget(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	cred, err := ti.service.CreateGcpIamCredential(withAdmin(t, orgAdmin(t, ctx)), &gen.CreateGcpIamCredentialPayload{
		SessionToken:              nil,
		Name:                      "gcp-exempt-pinned",
		ImpersonateServiceAccount: gramProjectServiceAccount("internal"),
	})
	require.NoError(t, err)

	_, err = ti.service.UpdateGcpIamCredential(orgAdmin(t, ctx), &gen.UpdateGcpIamCredentialPayload{
		ID:                        cred.ID,
		SessionToken:              nil,
		Name:                      "gcp-exempt-pinned",
		ImpersonateServiceAccount: gramProjectServiceAccount("someone-else"),
	})
	requireOopsCode(t, err, oops.CodeBadRequest)

	requireSkipProjectVerification(t, ctx, ti, cred.ID, true)
}

// Moving the target away clears the exemption, so moving it back is refused.
// The flag cannot be parked on a credential and recovered later.
func TestUpdateGcpIamCredential_ExemptionLostByRoundTrip(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	target := gramProjectServiceAccount("internal")
	cred, err := ti.service.CreateGcpIamCredential(withAdmin(t, orgAdmin(t, ctx)), &gen.CreateGcpIamCredentialPayload{
		SessionToken:              nil,
		Name:                      "gcp-exempt-round-trip",
		ImpersonateServiceAccount: target,
	})
	require.NoError(t, err)

	_, err = ti.service.UpdateGcpIamCredential(orgAdmin(t, ctx), &gen.UpdateGcpIamCredentialPayload{
		ID:                        cred.ID,
		SessionToken:              nil,
		Name:                      "gcp-exempt-round-trip",
		ImpersonateServiceAccount: "signer@customer-project.iam.gserviceaccount.com",
	})
	require.NoError(t, err)
	requireSkipProjectVerification(t, ctx, ti, cred.ID, false)

	_, err = ti.service.UpdateGcpIamCredential(orgAdmin(t, ctx), &gen.UpdateGcpIamCredentialPayload{
		ID:                        cred.ID,
		SessionToken:              nil,
		Name:                      "gcp-exempt-round-trip",
		ImpersonateServiceAccount: target,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

// A row that predates the screening carries no exemption, so re-saving it stays
// refused. The carry-forward rule must not launder those rows into exempt ones.
func TestUpdateGcpIamCredential_UnscreenedRowIsNotCarriedForward(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	created := createGCPUnscreenedCredentialDirect(t, ctx, ti, "gcp-legacy-unscreened")

	_, err := ti.service.UpdateGcpIamCredential(orgAdmin(t, ctx), &gen.UpdateGcpIamCredentialPayload{
		ID:                        created.ID,
		SessionToken:              nil,
		Name:                      "gcp-legacy-renamed",
		ImpersonateServiceAccount: gramProjectServiceAccount("internal"),
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

// Verify reports what an exempted credential can actually do. Re-screening it
// without the exemption would report a refusal no edit through this API could
// clear.
func TestVerifyGcpIamCredential_HonorsStoredExemption(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	cred, err := ti.service.CreateGcpIamCredential(withAdmin(t, orgAdmin(t, ctx)), &gen.CreateGcpIamCredentialPayload{
		SessionToken:              nil,
		Name:                      "gcp-exempt-verify",
		ImpersonateServiceAccount: gramProjectServiceAccount("internal"),
	})
	require.NoError(t, err)

	result, err := ti.service.VerifyGcpIamCredential(orgAdmin(t, ctx), &gen.VerifyGcpIamCredentialPayload{
		ID:           cred.ID,
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.True(t, result.Verified)
	require.Equal(t, gramProjectServiceAccount("internal"), *result.Principal)
}

// A platform administrator can also grant the exemption by editing an existing
// credential, including lifting one written before the screening existed. The
// log is the only durable record of that grant, so it has to fire here too.
func TestUpdateGcpIamCredential_PlatformAdminGrantsExemptionOnUpdate(t *testing.T) {
	t.Parallel()
	ctx, ti, logs := newTestServiceWithLogs(t)

	created := createGCPUnscreenedCredentialDirect(t, ctx, ti, "gcp-legacy-to-lift")
	requireSkipProjectVerification(t, ctx, ti, created.ID, false)

	_, err := ti.service.UpdateGcpIamCredential(withAdmin(t, orgAdmin(t, ctx)), &gen.UpdateGcpIamCredentialPayload{
		ID:                        created.ID,
		SessionToken:              nil,
		Name:                      "gcp-legacy-lifted",
		ImpersonateServiceAccount: gramProjectServiceAccount("internal"),
	})
	require.NoError(t, err)

	requireSkipProjectVerification(t, ctx, ti, created.ID, true)
	require.Contains(t, logs.String(), "exempted a gcp iam credential from own-project screening")
}

// Re-pointing an exempted credential at a second internal service account is a
// new grant of a new pair, even though the stored column does not change. The
// exemption is pinned to the pair, so a record keyed on the column alone would
// let the second grant happen with nothing written down anywhere.
func TestUpdateGcpIamCredential_RepointingExemptedCredentialIsANewGrant(t *testing.T) {
	t.Parallel()
	ctx, ti, logs := newTestServiceWithLogs(t)

	cred, err := ti.service.CreateGcpIamCredential(withAdmin(t, orgAdmin(t, ctx)), &gen.CreateGcpIamCredentialPayload{
		SessionToken:              nil,
		Name:                      "gcp-exempt-repointed",
		ImpersonateServiceAccount: gramProjectServiceAccount("internal"),
	})
	require.NoError(t, err)
	require.Equal(t, 1, strings.Count(logs.String(), "exempted a gcp iam credential from own-project screening"))

	updated, err := ti.service.UpdateGcpIamCredential(withAdmin(t, orgAdmin(t, ctx)), &gen.UpdateGcpIamCredentialPayload{
		ID:                        cred.ID,
		SessionToken:              nil,
		Name:                      "gcp-exempt-repointed",
		ImpersonateServiceAccount: gramProjectServiceAccount("second-internal"),
	})
	require.NoError(t, err)
	require.Equal(t, gramProjectServiceAccount("second-internal"), *updated.ImpersonateServiceAccount)

	requireSkipProjectVerification(t, ctx, ti, cred.ID, true)
	require.Equal(t, 2, strings.Count(logs.String(), "exempted a gcp iam credential from own-project screening"),
		"re-pointing an exempted credential at another service account is a second grant")
}

// Carrying an existing exemption forward is not a grant, so a re-save that
// changes nothing must stay silent. Logging every write by a platform
// administrator would bury the grants among the re-saves.
func TestUpdateGcpIamCredential_CarryingExemptionForwardIsNotLogged(t *testing.T) {
	t.Parallel()
	ctx, ti, logs := newTestServiceWithLogs(t)

	target := gramProjectServiceAccount("internal")
	cred, err := ti.service.CreateGcpIamCredential(withAdmin(t, orgAdmin(t, ctx)), &gen.CreateGcpIamCredentialPayload{
		SessionToken:              nil,
		Name:                      "gcp-exempt-resaved",
		ImpersonateServiceAccount: target,
	})
	require.NoError(t, err)

	_, err = ti.service.UpdateGcpIamCredential(orgAdmin(t, ctx), &gen.UpdateGcpIamCredentialPayload{
		ID:                        cred.ID,
		SessionToken:              nil,
		Name:                      "gcp-exempt-resaved-again",
		ImpersonateServiceAccount: target,
	})
	require.NoError(t, err)

	require.Equal(t, 1, strings.Count(logs.String(), "exempted a gcp iam credential from own-project screening"),
		"only the original grant is a grant")
}

// The support-contact suffix belongs to the one refusal staff can lift. A
// malformed address is self-serve, so pointing the operator at a human queue
// would be wrong.
func TestCreateGcpIamCredential_MalformedTargetRefusalOffersNoSupportContact(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.CreateGcpIamCredential(orgAdmin(t, ctx), &gen.CreateGcpIamCredentialPayload{
		SessionToken:              nil,
		Name:                      "gcp-malformed-no-support",
		ImpersonateServiceAccount: "person@example.com",
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
	require.NotContains(t, err.Error(), "Speakeasy support")
}
