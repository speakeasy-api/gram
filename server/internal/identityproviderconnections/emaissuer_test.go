package identityproviderconnections_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/conv"
	extkeysrepo "github.com/speakeasy-api/gram/server/internal/externalkeys/repo"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/identityproviderconnections"
	"github.com/speakeasy-api/gram/server/internal/identityproviderconnections/provisiontest"
	"github.com/speakeasy-api/gram/server/internal/identityproviderconnections/repo"
	"github.com/speakeasy-api/gram/server/internal/internalsessionissuers"
	jwksrepo "github.com/speakeasy-api/gram/server/internal/jsonwebkeysets/repo"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/gcp/gcpauth"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/gcp/gcpkms"
)

// pauseFlagFails is a flag provider whose kill-switch lookup errors.
type pauseFlagFails struct{ *feature.InMemory }

func (p pauseFlagFails) EvaluateFlag(ctx context.Context, flag feature.Flag, distinctID string, groups map[string]string) (feature.Evaluation, error) {
	if flag == feature.FlagInternalSessionIssuerSigningPaused {
		return feature.EvaluationIndeterminate, errors.New("flag provider unavailable")
	}
	return p.InMemory.EvaluateFlag(ctx, flag, distinctID, groups) //nolint:wrapcheck // Test double.
}

type issuerFixture struct {
	ti           *testInstance
	credentialID uuid.UUID
	flags        *feature.InMemory
}

func newIssuerFixture(t *testing.T) (context.Context, issuerFixture) {
	t.Helper()
	ctx, ti := newTestDB(t)
	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagInternalSessionIssuer, ti.orgID, true)
	flags.SetFlag(feature.FlagInternalSessionIssuerSigningPaused, ti.orgID, false)
	f := issuerFixture{ti: ti, credentialID: provisiontest.CreatePlatformSigningCredential(t, ctx, ti.conn), flags: flags}
	f.setVerifiedDomains(t, ctx, "example.com")
	return ctx, f
}

// setVerifiedDomains replaces the organization's verified domains.
func (f issuerFixture) setVerifiedDomains(t *testing.T, ctx context.Context, domains ...string) {
	t.Helper()
	require.NoError(t, testrepo.New(f.ti.conn).SetOrganizationVerifiedDomainsFixture(ctx, testrepo.SetOrganizationVerifiedDomainsFixtureParams{ID: f.ti.orgID, VerifiedDomains: append([]string{}, domains...)}))
}

// provisioner returns a provisioner minting from its own in-process key, so
// each provisioner's keys have a distinct kid.
func (f issuerFixture) provisioner(t *testing.T) (*identityproviderconnections.Provisioner, *hookedKMSClients) {
	t.Helper()
	kms := &hookedKMSClients{inner: provisiontest.NewKMSClients(t)}
	return provisiontest.NewProvisioner(t, f.ti.conn, kms.Factory, testServerURL, f.credentialID, ""), kms
}

func (f issuerFixture) lifecycle() *internalsessionissuers.Lifecycle {
	return internalsessionissuers.NewLifecycle(f.ti.conn, audit.NewLogger())
}

// signer signs through the same in-process key as kms.
func (f issuerFixture) signer(t *testing.T, kms *hookedKMSClients, paused bool) *internalsessionissuers.Signer {
	t.Helper()
	return f.signerWithFlags(t, kms, paused, f.flags)
}

func (f issuerFixture) signerWithFlags(t *testing.T, kms *hookedKMSClients, paused bool, flags feature.Provider) *internalsessionissuers.Signer {
	t.Helper()
	factory := func(ctx context.Context, ts oauth2.TokenSource) (gcpkms.SigningClient, error) {
		return kms.inner.Factory(ctx, ts)
	}
	gate := internalsessionissuers.NewGate(f.ti.conn, flags, internalsessionissuers.GateConfig{Provisioning: true, SigningPaused: paused})
	return internalsessionissuers.NewSigner(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), f.ti.conn, gate, gcpauth.NewIdentity(gcpauth.NewStubResolver()), factory, issuerSignerConfig(t, testServerURL, provisiontest.KeyRing, provisiontest.SigningServiceAccount()))
}

func issuerSignerConfig(t *testing.T, origin, ring, signer string) internalsessionissuers.SignerConfig {
	t.Helper()
	return internalsessionissuers.SignerConfig{Deployment: internalsessionissuers.Deployment{ServerURL: mustURL(t, origin), AllowLoopbackIssuer: false, KeyRing: ring}, ManagedSigner: signer}
}

func claims() internalsessionissuers.Claims {
	return internalsessionissuers.Claims{Subject: "user-1", Email: "user-1@example.com", Audience: "https://vendor.example.com", ClientID: "client-1", Resource: "https://vendor.example.com/mcp", Scope: "read", ID: uuid.NewString(), Expiry: time.Now().Add(time.Minute), Extra: nil}
}

func issuerURL(t *testing.T, orgID string) string {
	t.Helper()
	issuer, err := internalsessionissuers.URL(mustURL(t, testServerURL), orgID, false)
	require.NoError(t, err)
	return issuer.String()
}

func publishedIssuerKids(t *testing.T, ctx context.Context, f issuerFixture, orgID string) []string {
	t.Helper()
	_, set, err := internalsessionissuers.PublicKeys(ctx, f.ti.conn, orgID)
	require.NoError(t, err)
	kids := make([]string, 0, len(set.Keys))
	for _, key := range set.Keys {
		kids = append(kids, key.KeyID)
	}
	return kids
}

func activeIssuerKid(t *testing.T, ctx context.Context, f issuerFixture, orgID string) string {
	t.Helper()
	key, err := jwksrepo.New(f.ti.conn).GetActiveJsonWebKey(ctx, jwksrepo.GetActiveJsonWebKeyParams{JsonWebKeySetID: issuerKeySet(t, ctx, f, orgID), OrganizationID: orgID})
	require.NoError(t, err)
	return key.Kid
}

func issuerKeySet(t *testing.T, ctx context.Context, f issuerFixture, orgID string) uuid.UUID {
	t.Helper()
	issuer, err := jwksrepo.New(f.ti.conn).GetOrganizationInternalSessionIssuer(ctx, orgID)
	require.NoError(t, err)
	return issuer.JsonWebKeySetID
}

// verifyAssertion checks an assertion against the published keys, as a vendor does.
func verifyAssertion(t *testing.T, ctx context.Context, f issuerFixture, orgID, assertion string) map[string]any {
	t.Helper()
	parsed, err := jwt.ParseSigned(assertion, []jose.SignatureAlgorithm{jose.RS256, jose.ES256})
	require.NoError(t, err)
	require.Equal(t, internalsessionissuers.AssertionType, parsed.Headers[0].ExtraHeaders[jose.HeaderType])
	_, keys, err := internalsessionissuers.PublicKeys(ctx, f.ti.conn, orgID)
	require.NoError(t, err)
	var body map[string]any
	require.NoError(t, parsed.Claims(keys, &body))
	return body
}

func requireRefused(t *testing.T, err error, state internalsessionissuers.State) {
	t.Helper()
	refused, ok := errors.AsType[*internalsessionissuers.SigningRefusedError](err)
	require.True(t, ok, "want a refusal, got %v", err)
	require.Equal(t, state, refused.State)
}

func auditCount(t *testing.T, ctx context.Context, f issuerFixture, action audit.Action) int64 {
	t.Helper()
	count, err := audittest.AuditLogCountByAction(ctx, f.ti.conn, action)
	require.NoError(t, err)
	return count
}

func TestEMAIssuer_EnsureIsIdempotentAndConcurrent(t *testing.T) {
	t.Parallel()
	ctx, f := newIssuerFixture(t)
	provisioner, kms := f.provisioner(t)

	const callers = 5
	ids := make([]uuid.UUID, callers)
	var wg sync.WaitGroup
	for i := range callers {
		wg.Go(func() {
			id, err := provisioner.EnsureEMAIssuer(ctx, f.ti.orgID)
			require.NoError(t, err)
			ids[i] = id
		})
	}
	wg.Wait()
	for _, id := range ids {
		require.Equal(t, ids[0], id, "every caller adopts one issuer")
	}
	again, err := provisioner.EnsureEMAIssuer(ctx, f.ti.orgID)
	require.NoError(t, err)
	require.Equal(t, ids[0], again)
	require.Len(t, publishedIssuerKids(t, ctx, f, f.ti.orgID), 1)
	require.Len(t, kms.Disabled(), len(kms.Created())-1, "losing callers disable their unused keys")
	require.Equal(t, int64(1), auditCount(t, ctx, f, audit.ActionInternalSessionIssuerEnable))
	require.Equal(t, int64(1), auditCount(t, ctx, f, audit.ActionJsonWebKeySetCreate))
	require.Equal(t, int64(1), auditCount(t, ctx, f, audit.ActionJsonWebKeyPublish))
}

func TestEMAIssuer_EnsureRefusesDisabledOrganizationBeforeKMS(t *testing.T) {
	t.Parallel()
	ctx, f := newIssuerFixture(t)
	provisioner, kms := f.provisioner(t)
	require.NoError(t, testrepo.New(f.ti.conn).DisableDelegationOrganizationFixture(ctx, f.ti.orgID))

	_, err := provisioner.EnsureEMAIssuer(ctx, f.ti.orgID)
	require.ErrorIs(t, err, internalsessionissuers.ErrNotFound)
	require.Empty(t, kms.Created(), "no KMS key for a disabled organization")
}

func TestEMAIssuer_RotateKeepsURLAndOverlaps(t *testing.T) {
	t.Parallel()
	ctx, f := newIssuerFixture(t)
	provisioner, kms := f.provisioner(t)
	_, err := provisioner.EnsureEMAIssuer(ctx, f.ti.orgID)
	require.NoError(t, err)
	setID := issuerKeySet(t, ctx, f, f.ti.orgID)
	before := activeIssuerKid(t, ctx, f, f.ti.orgID)

	assertion, err := f.signer(t, kms, false).Sign(ctx, f.ti.orgID, claims())
	require.NoError(t, err)
	require.Equal(t, issuerURL(t, f.ti.orgID), verifyAssertion(t, ctx, f, f.ti.orgID, assertion)["iss"])

	rotator, rotatingKMS := f.provisioner(t)
	err = rotator.RotateEMAIssuer(ctx, f.ti.orgID, before)
	var pending *internalsessionissuers.RotationPendingError
	require.ErrorAs(t, err, &pending)
	require.Len(t, publishedIssuerKids(t, ctx, f, f.ti.orgID), 2, "pending key is published before use")
	require.Equal(t, before, activeIssuerKid(t, ctx, f, f.ti.orgID), "publication must not change the signer")

	err = rotator.RotateEMAIssuer(ctx, f.ti.orgID, before)
	require.ErrorAs(t, err, &pending, "activation waits for verifier caches")
	require.Len(t, rotatingKMS.Created(), 1, "retry reuses the pending key")

	q := repo.New(f.ti.conn)
	require.NoError(t, q.BackdatePendingRotationPublication(ctx, repo.BackdatePendingRotationPublicationParams{JsonWebKeySetID: setID, OrganizationID: f.ti.orgID, AgeSeconds: int32(internalsessionissuers.CacheTTL / time.Second)}))
	err = rotator.RotateEMAIssuer(ctx, f.ti.orgID, before)
	require.ErrorAs(t, err, &pending, "one cache window is not enough: a shared cache can sit in front of the verifier's")
	require.NoError(t, q.BackdatePendingRotationPublication(ctx, repo.BackdatePendingRotationPublicationParams{JsonWebKeySetID: setID, OrganizationID: f.ti.orgID, AgeSeconds: int32(internalsessionissuers.ActivationDelay / time.Second)}))
	require.NoError(t, rotator.RotateEMAIssuer(ctx, f.ti.orgID, before))
	after := activeIssuerKid(t, ctx, f, f.ti.orgID)
	require.NotEqual(t, before, after)
	require.ElementsMatch(t, []string{before, after}, publishedIssuerKids(t, ctx, f, f.ti.orgID), "retired key verifies during the overlap")
	require.Equal(t, int64(1), auditCount(t, ctx, f, audit.ActionJsonWebKeyRetire))
	require.Equal(t, int64(1), auditCount(t, ctx, f, audit.ActionJsonWebKeyActivate))

	require.Equal(t, setID, issuerKeySet(t, ctx, f, f.ti.orgID), "rotation keeps one key set")
	assertion, err = f.signer(t, rotatingKMS, false).Sign(ctx, f.ti.orgID, claims())
	require.NoError(t, err)
	require.Equal(t, issuerURL(t, f.ti.orgID), verifyAssertion(t, ctx, f, f.ti.orgID, assertion)["iss"], "issuer URL survives rotation")

	// A retry after activation is a no-op.
	require.NoError(t, rotator.RotateEMAIssuer(ctx, f.ti.orgID, before))
	require.Len(t, rotatingKMS.Created(), 1)

	err = rotator.RotateEMAIssuer(ctx, f.ti.orgID, "never-published")
	require.ErrorIs(t, err, internalsessionissuers.ErrUnknownKid)
}

func TestEMAIssuer_SweepRetiresKeysWithoutAnotherRotation(t *testing.T) {
	t.Parallel()
	ctx, f := newIssuerFixture(t)
	provisioner, kms := f.provisioner(t)
	_, err := provisioner.EnsureEMAIssuer(ctx, f.ti.orgID)
	require.NoError(t, err)
	setID := issuerKeySet(t, ctx, f, f.ti.orgID)
	before := activeIssuerKid(t, ctx, f, f.ti.orgID)
	rotator, rotatingKMS := f.provisioner(t)
	q := repo.New(f.ti.conn)
	var pending *internalsessionissuers.RotationPendingError
	require.ErrorAs(t, rotator.RotateEMAIssuer(ctx, f.ti.orgID, before), &pending)
	require.NoError(t, q.BackdatePendingRotationPublication(ctx, repo.BackdatePendingRotationPublicationParams{JsonWebKeySetID: setID, OrganizationID: f.ti.orgID, AgeSeconds: int32(internalsessionissuers.ActivationDelay / time.Second)}))
	require.NoError(t, rotator.RotateEMAIssuer(ctx, f.ti.orgID, before))
	after := activeIssuerKid(t, ctx, f, f.ti.orgID)

	pendingCleanup, err := rotator.SweepEMAIssuerKeys(ctx, f.ti.orgID)
	require.NoError(t, err, "nothing is due during the overlap")
	require.Zero(t, pendingCleanup)
	require.Empty(t, rotatingKMS.Disabled())

	require.NoError(t, q.BackdateRetiredJsonWebKeys(ctx, repo.BackdateRetiredJsonWebKeysParams{JsonWebKeySetID: setID, OrganizationID: f.ti.orgID, AgeSeconds: int32(internalsessionissuers.RetiredKeyOverlap/time.Second) + 1}))
	require.Equal(t, []string{after}, publishedIssuerKids(t, ctx, f, f.ti.orgID), "the retired key leaves the document after the overlap")
	swept, pendingCleanup, err := rotator.SweepAllEMAIssuerKeys(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, swept)
	require.Zero(t, pendingCleanup)
	require.Equal(t, kms.Created()[:1], rotatingKMS.Disabled(), "the retired KMS version is disabled")
	require.Equal(t, int64(1), auditCount(t, ctx, f, audit.ActionJsonWebKeyRevoke))
	swept, _, err = rotator.SweepAllEMAIssuerKeys(ctx)
	require.NoError(t, err)
	require.Zero(t, swept, "a swept organization is not swept again")
}

func TestEMAIssuer_FailedRotationDisablesNoKMSKey(t *testing.T) {
	t.Parallel()
	ctx, f := newIssuerFixture(t)
	provisioner, kms := f.provisioner(t)
	_, err := provisioner.EnsureEMAIssuer(ctx, f.ti.orgID)
	require.NoError(t, err)
	setID := issuerKeySet(t, ctx, f, f.ti.orgID)
	before := activeIssuerKid(t, ctx, f, f.ti.orgID)
	rotator, rotatingKMS := f.provisioner(t)
	q := repo.New(f.ti.conn)
	var pending *internalsessionissuers.RotationPendingError
	require.ErrorAs(t, rotator.RotateEMAIssuer(ctx, f.ti.orgID, before), &pending)
	require.NoError(t, q.BackdatePendingRotationPublication(ctx, repo.BackdatePendingRotationPublicationParams{JsonWebKeySetID: setID, OrganizationID: f.ti.orgID, AgeSeconds: int32(internalsessionissuers.ActivationDelay / time.Second)}))
	require.NoError(t, rotator.RotateEMAIssuer(ctx, f.ti.orgID, before))
	require.NoError(t, q.BackdateRetiredJsonWebKeys(ctx, repo.BackdateRetiredJsonWebKeysParams{JsonWebKeySetID: setID, OrganizationID: f.ti.orgID, AgeSeconds: int32(internalsessionissuers.RetiredKeyOverlap/time.Second) + 1}))

	// An unknown kid still commits the expiry it found, then retires it.
	require.ErrorIs(t, rotator.RotateEMAIssuer(ctx, f.ti.orgID, "never-published"), internalsessionissuers.ErrUnknownKid)
	require.Equal(t, kms.Created()[:1], rotatingKMS.Disabled())
	require.Equal(t, int64(1), auditCount(t, ctx, f, audit.ActionJsonWebKeyRevoke), "KMS and the audit trail agree")
}

func TestEMAIssuer_CompromisedRotationRevokesEveryKey(t *testing.T) {
	t.Parallel()
	ctx, f := newIssuerFixture(t)
	provisioner, kms := f.provisioner(t)
	_, err := provisioner.EnsureEMAIssuer(ctx, f.ti.orgID)
	require.NoError(t, err)
	compromised := activeIssuerKid(t, ctx, f, f.ti.orgID)
	rotator, rotatingKMS := f.provisioner(t)
	var pending *internalsessionissuers.RotationPendingError
	require.ErrorAs(t, rotator.RotateEMAIssuer(ctx, f.ti.orgID, compromised), &pending, "a rotation is in flight")
	require.Len(t, publishedIssuerKids(t, ctx, f, f.ti.orgID), 2)

	_, err = f.lifecycle().Contain(ctx, f.ti.orgID, nil)
	require.ErrorIs(t, err, internalsessionissuers.ErrActiveKidRequired)
	containment, err := f.lifecycle().Contain(ctx, f.ti.orgID, &compromised)
	require.NoError(t, err)
	require.Len(t, containment.Revoked, 2)
	require.Empty(t, publishedIssuerKids(t, ctx, f, f.ti.orgID), "the active and pending keys are withdrawn at once")
	_, err = f.signer(t, kms, false).Sign(ctx, f.ti.orgID, claims())
	requireRefused(t, err, internalsessionissuers.StateKeyless)
	require.Equal(t, int64(2), auditCount(t, ctx, f, audit.ActionJsonWebKeyRevoke))

	replacer, replacingKMS := f.provisioner(t)
	cleaned, pendingCleanup, err := replacer.CleanupEMAIssuerKMS(ctx, f.ti.orgID)
	require.NoError(t, err)
	require.Equal(t, 2, cleaned)
	require.Zero(t, pendingCleanup)
	require.ElementsMatch(t, append(kms.Created(), rotatingKMS.Created()...), slices.Compact(slices.Sorted(slices.Values(replacingKMS.Disabled()))), "every compromised version is disabled in KMS")

	require.NoError(t, replacer.ProvisionEMAIssuerKey(ctx, f.ti.orgID))
	replacement := activeIssuerKid(t, ctx, f, f.ti.orgID)
	require.NotEqual(t, compromised, replacement)
	require.Equal(t, []string{replacement}, publishedIssuerKids(t, ctx, f, f.ti.orgID))
	assertion, err := f.signer(t, replacingKMS, false).Sign(ctx, f.ti.orgID, claims())
	require.NoError(t, err)
	require.Equal(t, issuerURL(t, f.ti.orgID), verifyAssertion(t, ctx, f, f.ti.orgID, assertion)["iss"])

	// Repeats naming the replaced key coalesce: nothing is revoked or minted.
	containment, err = f.lifecycle().Contain(ctx, f.ti.orgID, &compromised)
	require.NoError(t, err)
	require.True(t, containment.Coalesced)
	require.NoError(t, replacer.ProvisionEMAIssuerKey(ctx, f.ti.orgID))
	require.Len(t, replacingKMS.Created(), 1, "a live issuer gets no further key")
	require.Equal(t, replacement, activeIssuerKid(t, ctx, f, f.ti.orgID))
	unknown := "never-published"
	_, err = f.lifecycle().Contain(ctx, f.ti.orgID, &unknown)
	require.ErrorIs(t, err, internalsessionissuers.ErrUnknownKid)

	// A later routine rotation can't resurrect the withdrawn pending key.
	next, nextKMS := f.provisioner(t)
	require.ErrorAs(t, next.RotateEMAIssuer(ctx, f.ti.orgID, replacement), &pending)
	require.Len(t, nextKMS.Created(), 1, "it publishes a fresh key")
}

func TestEMAIssuer_DisableStopsSigningAndReenableResumes(t *testing.T) {
	t.Parallel()
	ctx, f := newIssuerFixture(t)
	provisioner, kms := f.provisioner(t)
	first, err := provisioner.EnsureEMAIssuer(ctx, f.ti.orgID)
	require.NoError(t, err)
	kid := activeIssuerKid(t, ctx, f, f.ti.orgID)
	signer := f.signer(t, kms, false)

	require.NoError(t, f.lifecycle().SetDisabled(ctx, f.ti.orgID, true))
	require.Equal(t, []string{kid}, publishedIssuerKids(t, ctx, f, f.ti.orgID), "discovery keeps serving while disabled")
	_, err = signer.Sign(ctx, f.ti.orgID, claims())
	requireRefused(t, err, internalsessionissuers.StateDisabled)
	require.Empty(t, kms.Disabled(), "disabling keeps the key")
	require.NoError(t, f.lifecycle().SetDisabled(ctx, f.ti.orgID, true), "disabling twice is a no-op")
	require.Equal(t, int64(1), auditCount(t, ctx, f, audit.ActionInternalSessionIssuerDisable))

	// A copied database cannot re-enable an issuer owned by the source deployment.
	moved := provisiontest.NewProvisioner(t, f.ti.conn, kms.Factory, "https://moved.example.com", f.credentialID, "")
	second, err := moved.EnsureEMAIssuer(ctx, f.ti.orgID)
	require.ErrorIs(t, err, internalsessionissuers.ErrMisconfigured)
	require.Equal(t, uuid.Nil, second)
	second, err = provisioner.EnsureEMAIssuer(ctx, f.ti.orgID)
	require.NoError(t, err)
	require.Equal(t, first, second, "re-enabling keeps the issuer")
	require.Len(t, kms.Created(), 1, "re-enabling creates no key")
	require.Equal(t, int64(2), auditCount(t, ctx, f, audit.ActionInternalSessionIssuerEnable))
	assertion, err := signer.Sign(ctx, f.ti.orgID, claims())
	require.NoError(t, err)
	require.Equal(t, issuerURL(t, f.ti.orgID), verifyAssertion(t, ctx, f, f.ti.orgID, assertion)["iss"])
}

func TestEMAIssuer_CrossOrgIsolation(t *testing.T) {
	t.Parallel()
	ctx, f := newIssuerFixture(t)
	provisioner, kms := f.provisioner(t)
	_, err := provisioner.EnsureEMAIssuer(ctx, f.ti.orgID)
	require.NoError(t, err)
	kid := activeIssuerKid(t, ctx, f, f.ti.orgID)
	other := createOrganization(t, ctx, f.ti.conn)
	f.flags.SetFlag(feature.FlagInternalSessionIssuer, other, true)
	require.NoError(t, testrepo.New(f.ti.conn).SetOrganizationVerifiedDomainsFixture(ctx, testrepo.SetOrganizationVerifiedDomainsFixtureParams{ID: other, VerifiedDomains: []string{"other.example"}}))

	_, _, err = internalsessionissuers.PublicKeys(ctx, f.ti.conn, other)
	require.ErrorIs(t, err, internalsessionissuers.ErrNotFound, "another organization resolves no issuer")
	_, err = f.signer(t, kms, false).Sign(ctx, other, claims())
	requireRefused(t, err, internalsessionissuers.StateSetupRequired)
	require.ErrorIs(t, provisioner.RotateEMAIssuer(ctx, other, kid), internalsessionissuers.ErrNotFound)
	_, err = f.lifecycle().Contain(ctx, other, &kid)
	require.ErrorIs(t, err, internalsessionissuers.ErrNotFound)
	require.ErrorIs(t, f.lifecycle().SetDisabled(ctx, other, true), internalsessionissuers.ErrNotFound)
	require.ErrorIs(t, provisioner.ProvisionEMAIssuerKey(ctx, other), internalsessionissuers.ErrNotFound)
	pendingCleanup, err := provisioner.SweepEMAIssuerKeys(ctx, other)
	require.NoError(t, err)
	require.Zero(t, pendingCleanup)
	require.Equal(t, []string{kid}, publishedIssuerKids(t, ctx, f, f.ti.orgID), "org A's issuer is untouched")
}

func TestEMAIssuer_SigningGates(t *testing.T) {
	t.Parallel()
	ctx, f := newIssuerFixture(t)
	provisioner, kms := f.provisioner(t)
	_, err := provisioner.EnsureEMAIssuer(ctx, f.ti.orgID)
	require.NoError(t, err)

	_, err = f.signer(t, kms, true).Sign(ctx, f.ti.orgID, claims())
	requireRefused(t, err, internalsessionissuers.StateSigningPaused)

	f.flags.SetFlag(feature.FlagInternalSessionIssuerSigningPaused, f.ti.orgID, true)
	_, err = f.signer(t, kms, false).Sign(ctx, f.ti.orgID, claims())
	requireRefused(t, err, internalsessionissuers.StateSigningPaused)
	f.flags.SetFlag(feature.FlagInternalSessionIssuerSigningPaused, f.ti.orgID, false)

	missing := &feature.InMemory{}
	missing.SetFlag(feature.FlagInternalSessionIssuer, f.ti.orgID, true)
	_, err = f.signerWithFlags(t, kms, false, missing).Sign(ctx, f.ti.orgID, claims())
	requireRefused(t, err, internalsessionissuers.StateSigningPaused)
	_, err = f.signerWithFlags(t, kms, false, pauseFlagFails{InMemory: f.flags}).Sign(ctx, f.ti.orgID, claims())
	requireRefused(t, err, internalsessionissuers.StateSigningPaused)
	_, err = f.signer(t, kms, false).Sign(ctx, f.ti.orgID, claims())
	require.NoError(t, err, "only an authoritative off lets signing run")

	f.setVerifiedDomains(t, ctx)
	_, err = f.signer(t, kms, false).Sign(ctx, f.ti.orgID, claims())
	requireRefused(t, err, internalsessionissuers.StateVerifiedDomainRequired)
	f.setVerifiedDomains(t, ctx, "example.com")

	f.flags.SetFlag(feature.FlagInternalSessionIssuer, f.ti.orgID, false)
	_, err = f.signer(t, kms, false).Sign(ctx, f.ti.orgID, claims())
	requireRefused(t, err, internalsessionissuers.StateUnavailable)
	f.flags.SetFlag(feature.FlagInternalSessionIssuer, f.ti.orgID, true)

	factory := func(ctx context.Context, ts oauth2.TokenSource) (gcpkms.SigningClient, error) {
		return kms.inner.Factory(ctx, ts)
	}
	gate := internalsessionissuers.NewGate(f.ti.conn, f.flags, internalsessionissuers.GateConfig{Provisioning: true, SigningPaused: false})
	for _, pin := range []string{"", "someone-else@example.iam.gserviceaccount.com"} {
		unpinned := internalsessionissuers.NewSigner(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), f.ti.conn, gate, gcpauth.NewIdentity(gcpauth.NewStubResolver()), factory, issuerSignerConfig(t, testServerURL, provisiontest.KeyRing, pin))
		_, err = unpinned.Sign(ctx, f.ti.orgID, claims())
		require.ErrorIs(t, err, internalsessionissuers.ErrMisconfigured, "signing needs the key's credential to match the configured signer %q", pin)
	}

	require.Len(t, publishedIssuerKids(t, ctx, f, f.ti.orgID), 1, "discovery keeps serving")
}

func TestEMAIssuer_SignEnforcesClaimInvariants(t *testing.T) {
	t.Parallel()
	ctx, f := newIssuerFixture(t)
	provisioner, kms := f.provisioner(t)
	_, err := provisioner.EnsureEMAIssuer(ctx, f.ti.orgID)
	require.NoError(t, err)
	signer := f.signer(t, kms, false)

	for name, mutate := range map[string]func(*internalsessionissuers.Claims){
		"no subject": func(c *internalsessionissuers.Claims) { c.Subject = "" },
		"no email":   func(c *internalsessionissuers.Claims) { c.Email = "" },
		"foreign email domain": func(c *internalsessionissuers.Claims) {
			c.Email = "user-1@attacker.example"
		},
		"subdomain of verified domain": func(c *internalsessionissuers.Claims) {
			c.Email = "user-1@mail.example.com"
		},
		"second @ ending on verified domain": func(c *internalsessionissuers.Claims) {
			c.Email = "user-1@attacker.example@example.com"
		},
		"reserved tenant":         func(c *internalsessionissuers.Claims) { c.Extra = map[string]any{"tenant": "tenant-1"} },
		"reserved email_verified": func(c *internalsessionissuers.Claims) { c.Extra = map[string]any{"email_verified": true} },
		"reserved act":            func(c *internalsessionissuers.Claims) { c.Extra = map[string]any{"act": map[string]any{"sub": "x"}} },
		"no audience":             func(c *internalsessionissuers.Claims) { c.Audience = "" },
		"no client":               func(c *internalsessionissuers.Claims) { c.ClientID = "" },
		"no jti":                  func(c *internalsessionissuers.Claims) { c.ID = "" },
		"no expiry":               func(c *internalsessionissuers.Claims) { c.Expiry = time.Time{} },
		"expired":                 func(c *internalsessionissuers.Claims) { c.Expiry = time.Now().Add(-time.Second) },
		"outlives overlap": func(c *internalsessionissuers.Claims) {
			c.Expiry = time.Now().Add(internalsessionissuers.MaxAssertionLifetime + time.Minute)
		},
		"overrides iss": func(c *internalsessionissuers.Claims) {
			c.Extra = map[string]any{"iss": "https://attacker.example.com"}
		},
		"overrides iat":   func(c *internalsessionissuers.Claims) { c.Extra = map[string]any{"iat": 1} },
		"overrides sub":   func(c *internalsessionissuers.Claims) { c.Extra = map[string]any{"sub": "someone-else"} },
		"overrides email": func(c *internalsessionissuers.Claims) { c.Extra = map[string]any{"email": "someone-else@example.com"} },
	} {
		c := claims()
		mutate(&c)
		_, err := signer.Sign(ctx, f.ti.orgID, c)
		require.ErrorIs(t, err, internalsessionissuers.ErrInvalidClaims, name)
	}

	c := claims()
	c.Email = "User-1@Example.COM"
	c.Extra = map[string]any{"vendor_hint": "hint-1"}
	issuedAt := time.Now().Add(-time.Second)
	assertion, err := signer.Sign(ctx, f.ti.orgID, c)
	require.NoError(t, err)
	body := verifyAssertion(t, ctx, f, f.ti.orgID, assertion)
	require.Equal(t, "hint-1", body["vendor_hint"])
	require.Equal(t, c.Email, body["email"])
	require.Equal(t, c.ClientID, body["client_id"])
	require.GreaterOrEqual(t, body["iat"], float64(issuedAt.Unix()), "the signer sets iat")
}

func TestEMAIssuer_HiddenFromCustomerKeyAPIs(t *testing.T) {
	t.Parallel()
	ctx, f := newIssuerFixture(t)
	provisioner, _ := f.provisioner(t)
	_, err := provisioner.EnsureEMAIssuer(ctx, f.ti.orgID)
	require.NoError(t, err)
	jq := jwksrepo.New(f.ti.conn)
	setID := issuerKeySet(t, ctx, f, f.ti.orgID)
	set, err := jq.LockOrganizationEMAIssuerForKeyWrite(ctx, f.ti.orgID)
	require.NoError(t, err)

	_, err = jq.GetJsonWebKeySet(ctx, jwksrepo.GetJsonWebKeySetParams{ID: setID, OrganizationID: f.ti.orgID})
	require.Error(t, err, "customer set reads never return the issuer")
	sets, err := jq.ListJsonWebKeySets(ctx, f.ti.orgID)
	require.NoError(t, err)
	require.Empty(t, sets)
	_, err = remotesessionsrepo.New(f.ti.conn).LockJsonWebKeySetForClientAttach(ctx, remotesessionsrepo.LockJsonWebKeySetForClientAttachParams{ID: setID, OrganizationID: f.ti.orgID})
	require.Error(t, err, "an issuer key set can never back OAuth client authentication")

	eq := extkeysrepo.New(f.ti.conn)
	keys, err := eq.ListExternalKeys(ctx, extkeysrepo.ListExternalKeysParams{OrganizationID: conv.ToPGText(f.ti.orgID), Provider: conv.PtrToPGTextEmpty(nil)})
	require.NoError(t, err)
	require.Empty(t, keys, "customer key listings never show the issuer key")
	_, err = eq.LockExternalKeyForDelete(ctx, extkeysrepo.LockExternalKeyForDeleteParams{ID: set.ExternalKeyID, OrganizationID: conv.ToPGText(f.ti.orgID), Provider: "gcp_kms"})
	require.Error(t, err, "customers cannot delete the issuer key")
	mint, err := jq.GetExternalKeyForMint(ctx, jwksrepo.GetExternalKeyForMintParams{ID: set.ExternalKeyID, OrganizationID: conv.ToPGText(f.ti.orgID)})
	require.NoError(t, err)
	require.True(t, mint.EmaIssuerManaged)
	require.True(t, mint.CredentialID.Valid, "the issuer key reaches the platform signing credential")
}

func TestEMAIssuer_UncertainCommitReconcilesWithoutAConnection(t *testing.T) {
	t.Parallel()
	ctx, f := newIssuerFixture(t)
	provisioner, kms := f.provisioner(t)
	_, err := provisioner.EnsureEMAIssuer(ctx, f.ti.orgID)
	require.NoError(t, err)
	published := kms.Created()[0]

	absent, err := provisioner.PublicationAbsent(ctx, f.ti.orgID, uuid.Nil, published)
	require.NoError(t, err)
	require.False(t, absent, "a committed issuer key is preserved")
	absent, err = provisioner.PublicationAbsent(ctx, f.ti.orgID, uuid.Nil, published+"-lost")
	require.NoError(t, err)
	require.True(t, absent, "a key the lost commit never published is disabled")
}

func TestEMAIssuer_CompromisedRotationContainsEvenIfKMSCannotDisable(t *testing.T) {
	t.Parallel()
	ctx, f := newIssuerFixture(t)
	provisioner, kms := f.provisioner(t)
	_, err := provisioner.EnsureEMAIssuer(ctx, f.ti.orgID)
	require.NoError(t, err)
	compromised := activeIssuerKid(t, ctx, f, f.ti.orgID)

	_, err = f.lifecycle().Contain(ctx, f.ti.orgID, &compromised)
	require.NoError(t, err)
	replacer, replacingKMS := f.provisioner(t)
	replacingKMS.failDisable(errors.New("kms unavailable"))
	cleaned, pendingCleanup, err := replacer.CleanupEMAIssuerKMS(ctx, f.ti.orgID)
	require.Error(t, err)
	require.Zero(t, cleaned)
	require.Equal(t, 1, pendingCleanup, "the cleanup stays recorded as pending")
	require.Len(t, replacingKMS.RevokedGrants(), 1, "a failed disable still removes the signer grant")
	require.Empty(t, publishedIssuerKids(t, ctx, f, f.ti.orgID), "a KMS failure never republishes the compromised key")
	_, err = f.signer(t, kms, false).Sign(ctx, f.ti.orgID, claims())
	requireRefused(t, err, internalsessionissuers.StateKeyless)

	require.NoError(t, replacer.ProvisionEMAIssuerKey(ctx, f.ti.orgID), "replacement does not wait for KMS cleanup")
	replacement := activeIssuerKid(t, ctx, f, f.ti.orgID)
	require.Equal(t, []string{replacement}, publishedIssuerKids(t, ctx, f, f.ti.orgID))

	replacingKMS.failDisable(nil)
	swept, pendingCleanup, err := replacer.SweepAllEMAIssuerKeys(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, swept, "the sweep retries the pending cleanup")
	require.Zero(t, pendingCleanup)
	require.Equal(t, kms.Created(), replacingKMS.Disabled())
	swept, _, err = replacer.SweepAllEMAIssuerKeys(ctx)
	require.NoError(t, err)
	require.Zero(t, swept, "completed cleanup is not retried")
}

func TestEMAIssuer_CompromisedReplacementFailureLeavesIssuerKeyless(t *testing.T) {
	t.Parallel()
	ctx, f := newIssuerFixture(t)
	provisioner, kms := f.provisioner(t)
	_, err := provisioner.EnsureEMAIssuer(ctx, f.ti.orgID)
	require.NoError(t, err)
	compromised := activeIssuerKid(t, ctx, f, f.ti.orgID)

	_, err = f.lifecycle().Contain(ctx, f.ti.orgID, &compromised)
	require.NoError(t, err)
	replacer, replacingKMS := f.provisioner(t)
	replacingKMS.failCreate(errors.New("kms unavailable"))
	require.Error(t, replacer.ProvisionEMAIssuerKey(ctx, f.ti.orgID))
	require.Empty(t, publishedIssuerKids(t, ctx, f, f.ti.orgID), "the compromised key stays withdrawn")
	_, err = f.signer(t, kms, false).Sign(ctx, f.ti.orgID, claims())
	requireRefused(t, err, internalsessionissuers.StateKeyless)

	// A keyless issuer needs no kid to retry.
	containment, err := f.lifecycle().Contain(ctx, f.ti.orgID, nil)
	require.NoError(t, err)
	require.Empty(t, containment.Revoked)
	replacingKMS.failCreate(nil)
	require.NoError(t, replacer.ProvisionEMAIssuerKey(ctx, f.ti.orgID))
	require.NotEqual(t, compromised, activeIssuerKid(t, ctx, f, f.ti.orgID))
}

// rotateFully runs a routine rotation through activation and returns the new active kid.
func rotateFully(t *testing.T, ctx context.Context, f issuerFixture, rotator *identityproviderconnections.Provisioner, orgID, active string) string {
	t.Helper()
	var pending *internalsessionissuers.RotationPendingError
	require.ErrorAs(t, rotator.RotateEMAIssuer(ctx, orgID, active), &pending)
	setID := issuerKeySet(t, ctx, f, orgID)
	require.NoError(t, repo.New(f.ti.conn).BackdatePendingRotationPublication(ctx, repo.BackdatePendingRotationPublicationParams{JsonWebKeySetID: setID, OrganizationID: orgID, AgeSeconds: int32(internalsessionissuers.ActivationDelay / time.Second)}))
	require.NoError(t, rotator.RotateEMAIssuer(ctx, orgID, active))
	return activeIssuerKid(t, ctx, f, orgID)
}

func TestEMAIssuer_CompromiseOfARetiredKeyStillContains(t *testing.T) {
	t.Parallel()
	ctx, f := newIssuerFixture(t)
	provisioner, _ := f.provisioner(t)
	_, err := provisioner.EnsureEMAIssuer(ctx, f.ti.orgID)
	require.NoError(t, err)
	compromised := activeIssuerKid(t, ctx, f, f.ti.orgID)
	rotator, _ := f.provisioner(t)
	current := rotateFully(t, ctx, f, rotator, f.ti.orgID, compromised)
	require.ElementsMatch(t, []string{compromised, current}, publishedIssuerKids(t, ctx, f, f.ti.orgID), "the retired key is still published")

	containment, err := f.lifecycle().Contain(ctx, f.ti.orgID, &compromised)
	require.NoError(t, err)
	require.False(t, containment.Coalesced, "a retired but published key is not already contained")
	require.Len(t, containment.Revoked, 2)
	require.Empty(t, publishedIssuerKids(t, ctx, f, f.ti.orgID))

	containment, err = f.lifecycle().Contain(ctx, f.ti.orgID, &compromised)
	require.NoError(t, err)
	require.Empty(t, containment.Revoked, "a keyless issuer has nothing left to revoke")
	replacer, _ := f.provisioner(t)
	require.NoError(t, replacer.ProvisionEMAIssuerKey(ctx, f.ti.orgID))
	containment, err = f.lifecycle().Contain(ctx, f.ti.orgID, &compromised)
	require.NoError(t, err)
	require.True(t, containment.Coalesced, "once revoked, repeats naming it coalesce")
}

// The sweep revokes a retired key after a routine rotation on its own. A
// compromise call naming that key must still contain its successor, which
// was active before the compromise was reported.
func TestEMAIssuer_CompromiseOfASweptKeyStillContainsItsSuccessor(t *testing.T) {
	t.Parallel()
	ctx, f := newIssuerFixture(t)
	provisioner, _ := f.provisioner(t)
	_, err := provisioner.EnsureEMAIssuer(ctx, f.ti.orgID)
	require.NoError(t, err)
	setID := issuerKeySet(t, ctx, f, f.ti.orgID)
	compromised := activeIssuerKid(t, ctx, f, f.ti.orgID)
	rotator, _ := f.provisioner(t)
	successor := rotateFully(t, ctx, f, rotator, f.ti.orgID, compromised)
	require.NoError(t, repo.New(f.ti.conn).BackdateRetiredJsonWebKeys(ctx, repo.BackdateRetiredJsonWebKeysParams{JsonWebKeySetID: setID, OrganizationID: f.ti.orgID, AgeSeconds: int32(internalsessionissuers.RetiredKeyOverlap/time.Second) + 1}))
	_, err = rotator.SweepEMAIssuerKeys(ctx, f.ti.orgID)
	require.NoError(t, err)
	require.Equal(t, []string{successor}, publishedIssuerKids(t, ctx, f, f.ti.orgID), "the sweep revoked the old key")

	containment, err := f.lifecycle().Contain(ctx, f.ti.orgID, &compromised)
	require.NoError(t, err)
	require.False(t, containment.Coalesced, "a key revoked by the sweep was never contained")
	require.Len(t, containment.Revoked, 1)
	require.Empty(t, publishedIssuerKids(t, ctx, f, f.ti.orgID))
}

func TestEMAIssuer_SweepSkipsTombstonedIssuersButFinishesTheirCleanup(t *testing.T) {
	t.Parallel()
	ctx, f := newIssuerFixture(t)
	provisioner, kms := f.provisioner(t)
	_, err := provisioner.EnsureEMAIssuer(ctx, f.ti.orgID)
	require.NoError(t, err)
	first := activeIssuerKid(t, ctx, f, f.ti.orgID)
	rotator, _ := f.provisioner(t)
	second := rotateFully(t, ctx, f, rotator, f.ti.orgID, first)
	setID := issuerKeySet(t, ctx, f, f.ti.orgID)
	q := repo.New(f.ti.conn)
	expire := func() {
		require.NoError(t, q.BackdateRetiredJsonWebKeys(ctx, repo.BackdateRetiredJsonWebKeysParams{JsonWebKeySetID: setID, OrganizationID: f.ti.orgID, AgeSeconds: int32(internalsessionissuers.RetiredKeyOverlap/time.Second) + 1}))
	}
	expire()
	// The first key is revoked with its KMS cleanup left pending.
	sweeper, sweeperKMS := f.provisioner(t)
	sweeperKMS.failDisable(errors.New("kms unavailable"))
	_, _, err = sweeper.SweepAllEMAIssuerKeys(ctx)
	require.Error(t, err)
	// The second key retires and expires too, then the issuer is tombstoned.
	nextRotator, nextKMS := f.provisioner(t)
	nextKMS.failDisable(errors.New("kms unavailable"))
	rotateFully(t, ctx, f, nextRotator, f.ti.orgID, second)
	expire()
	require.NoError(t, testrepo.New(f.ti.conn).TombstoneInternalSessionIssuerFixture(ctx, f.ti.orgID))

	sweeperKMS.failDisable(nil)
	swept, pending, err := sweeper.SweepAllEMAIssuerKeys(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, swept, "pending cleanup is finished even for a tombstoned issuer")
	require.Zero(t, pending)
	require.Equal(t, kms.Created(), sweeperKMS.Disabled(), "only the revoked key's version is disabled")
	swept, _, err = sweeper.SweepAllEMAIssuerKeys(ctx)
	require.NoError(t, err)
	require.Zero(t, swept, "a tombstoned issuer's retired keys are never listed, so it is not swept forever")
}

func TestEMAIssuer_SweepPagesThroughEveryOrganization(t *testing.T) {
	t.Parallel()
	ctx, f := newIssuerFixture(t)
	organizations := []string{f.ti.orgID, createOrganization(t, ctx, f.ti.conn), createOrganization(t, ctx, f.ti.conn)}
	for _, orgID := range organizations {
		provisioner, _ := f.provisioner(t)
		_, err := provisioner.EnsureEMAIssuer(ctx, orgID)
		require.NoError(t, err)
		kid := activeIssuerKid(t, ctx, f, orgID)
		_, err = f.lifecycle().Contain(ctx, orgID, &kid)
		require.NoError(t, err)
	}
	sweeper, kms := f.provisioner(t)
	sweeper.SetSweepPage(1)

	// A spent budget stops after one organization and says where to resume.
	first, err := sweeper.SweepEMAIssuerKeysFrom(ctx, "", time.Nanosecond)
	require.NoError(t, err)
	require.Equal(t, 1, first.Swept)
	require.False(t, first.Done)
	require.NotEmpty(t, first.Next)
	require.Len(t, kms.Disabled(), 1)

	rest, err := sweeper.SweepEMAIssuerKeysFrom(ctx, first.Next, 0)
	require.NoError(t, err)
	require.True(t, rest.Done)
	require.Equal(t, len(organizations)-1, rest.Swept, "resuming covers the remaining pages")
	require.Zero(t, rest.Pending)
	require.Len(t, kms.Disabled(), len(organizations))
}

func TestEMAIssuer_SweepKeepsItsCursorWhenListingFails(t *testing.T) {
	t.Parallel()
	ctx, f := newIssuerFixture(t)
	for _, orgID := range []string{f.ti.orgID, createOrganization(t, ctx, f.ti.conn)} {
		provisioner, _ := f.provisioner(t)
		_, err := provisioner.EnsureEMAIssuer(ctx, orgID)
		require.NoError(t, err)
		kid := activeIssuerKid(t, ctx, f, orgID)
		_, err = f.lifecycle().Contain(ctx, orgID, &kid)
		require.NoError(t, err)
	}
	sweeper, kms := f.provisioner(t)
	sweeper.SetSweepPage(1)
	sweepCtx, cancel := context.WithCancel(ctx)
	// The first organization's cleanup detaches from the caller; the next
	// page's listing then fails on the cancelled context.
	kms.mu.Lock()
	kms.onDisable = cancel
	kms.mu.Unlock()

	progress, err := sweeper.SweepEMAIssuerKeysFrom(sweepCtx, "", 0)
	require.Error(t, err)
	require.Equal(t, 1, progress.Swept)
	require.False(t, progress.Done)
	require.NotEmpty(t, progress.Next, "a failed listing keeps the cursor instead of restarting the traversal")
}

func TestEMAIssuer_SweepMovesPastAnOrganizationThatHangs(t *testing.T) {
	t.Parallel()
	ctx, f := newIssuerFixture(t)
	var created []string
	for _, orgID := range []string{f.ti.orgID, createOrganization(t, ctx, f.ti.conn)} {
		provisioner, kms := f.provisioner(t)
		_, err := provisioner.EnsureEMAIssuer(ctx, orgID)
		require.NoError(t, err)
		created = append(created, kms.Created()...)
		kid := activeIssuerKid(t, ctx, f, orgID)
		_, err = f.lifecycle().Contain(ctx, orgID, &kid)
		require.NoError(t, err)
	}
	sweeper, kms := f.provisioner(t)
	sweeper.SetIssuerKeyCleanupTimeout(time.Minute)
	sweeper.SetSweepOrganizationTimeout(300 * time.Millisecond)
	kms.mu.Lock()
	kms.stallDisable = created[0]
	kms.mu.Unlock()

	started := time.Now()
	progress, err := sweeper.SweepEMAIssuerKeysFrom(ctx, "", 0)
	require.Error(t, err)
	require.Less(t, time.Since(started), 30*time.Second, "the hanging organization's keys are bounded by its share, not each key's own deadline")
	require.Equal(t, 2, progress.Swept, "the cursor moves past the hanging organization")
	require.True(t, progress.Done)
	require.Equal(t, 1, progress.Pending)
	require.Equal(t, created[1:], kms.Disabled())
}

// A key that keeps failing goes to the back of the line, so keys after it are
// reached on the next run even when the first one spends the whole budget.
func TestEMAIssuer_CleanupRetriesFailingKeysLast(t *testing.T) {
	t.Parallel()
	ctx, f := newIssuerFixture(t)
	provisioner, kms := f.provisioner(t)
	_, err := provisioner.EnsureEMAIssuer(ctx, f.ti.orgID)
	require.NoError(t, err)
	active := activeIssuerKid(t, ctx, f, f.ti.orgID)
	rotator, rotatingKMS := f.provisioner(t)
	var pending *internalsessionissuers.RotationPendingError
	require.ErrorAs(t, rotator.RotateEMAIssuer(ctx, f.ti.orgID, active), &pending)
	_, err = f.lifecycle().Contain(ctx, f.ti.orgID, &active)
	require.NoError(t, err)

	queue, err := jwksrepo.New(f.ti.conn).ListPendingEMAIssuerKMSCleanup(ctx, f.ti.orgID)
	require.NoError(t, err)
	require.Len(t, queue, 2)
	first, err := repo.New(f.ti.conn).GetManagedKeyResource(ctx, repo.GetManagedKeyResourceParams{ID: queue[0].ExternalKeyID, OrganizationID: conv.ToPGText(f.ti.orgID)})
	require.NoError(t, err)
	healthy := slices.DeleteFunc(append(kms.Created(), rotatingKMS.Created()...), func(v string) bool { return v == first.ResourceName })
	require.Len(t, healthy, 1)

	cleaner, cleaningKMS := f.provisioner(t)
	cleaner.SetIssuerKeyCleanupTimeout(time.Minute)
	cleaningKMS.mu.Lock()
	cleaningKMS.stallDisable = first.ResourceName
	cleaningKMS.mu.Unlock()
	run := func() (int, int) {
		runCtx, cancel := context.WithTimeout(ctx, 400*time.Millisecond)
		defer cancel()
		cleaned, stillPending, _ := cleaner.CleanupEMAIssuerKMS(runCtx, f.ti.orgID)
		return cleaned, stillPending
	}

	cleaned, stillPending := run()
	require.Zero(t, cleaned, "the stalled key spends the whole run")
	require.Equal(t, 2, stillPending)
	cleaned, stillPending = run()
	require.Equal(t, 1, cleaned, "the next run starts with the key not yet tried")
	require.Equal(t, 1, stillPending)
	require.Equal(t, healthy, cleaningKMS.Disabled())
}

func TestEMAIssuer_CleanupGivesEachKeyItsOwnDeadline(t *testing.T) {
	t.Parallel()
	ctx, f := newIssuerFixture(t)
	provisioner, kms := f.provisioner(t)
	_, err := provisioner.EnsureEMAIssuer(ctx, f.ti.orgID)
	require.NoError(t, err)
	active := activeIssuerKid(t, ctx, f, f.ti.orgID)
	rotator, rotatingKMS := f.provisioner(t)
	err = rotator.RotateEMAIssuer(ctx, f.ti.orgID, active)
	var pending *internalsessionissuers.RotationPendingError
	require.ErrorAs(t, err, &pending)
	_, err = f.lifecycle().Contain(ctx, f.ti.orgID, &active)
	require.NoError(t, err)

	cleaner, cleaningKMS := f.provisioner(t)
	cleaner.SetIssuerKeyCleanupTimeout(500 * time.Millisecond)
	cleaningKMS.mu.Lock()
	cleaningKMS.stallDisable = kms.Created()[0]
	cleaningKMS.mu.Unlock()
	cleaned, stillPending, err := cleaner.CleanupEMAIssuerKMS(ctx, f.ti.orgID)
	require.Error(t, err)
	require.Equal(t, 1, cleaned, "the healthy key after a stalled one is still cleaned up")
	require.Equal(t, 1, stillPending)
	require.Equal(t, rotatingKMS.Created(), cleaningKMS.Disabled())
	require.Len(t, cleaningKMS.RevokedGrants(), 2, "a stalled disable doesn't stop removing that key's grant")
}

func TestEMAIssuer_CleanupOpeningKMSStopsAtTheCallerDeadline(t *testing.T) {
	t.Parallel()
	ctx, f := newIssuerFixture(t)
	provisioner, _ := f.provisioner(t)
	_, err := provisioner.EnsureEMAIssuer(ctx, f.ti.orgID)
	require.NoError(t, err)
	kid := activeIssuerKid(t, ctx, f, f.ti.orgID)
	_, err = f.lifecycle().Contain(ctx, f.ti.orgID, &kid)
	require.NoError(t, err)

	cleaner, cleaningKMS := f.provisioner(t)
	cleaner.SetIssuerKeyCleanupTimeout(time.Minute)
	cleaningKMS.mu.Lock()
	cleaningKMS.stallOpen = true
	cleaningKMS.mu.Unlock()
	callCtx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()
	started := time.Now()
	cleaned, pending, err := cleaner.CleanupEMAIssuerKMS(callCtx, f.ti.orgID)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Less(t, time.Since(started), 30*time.Second, "opening the kms client stops at the caller deadline, not the cleanup timeout")
	require.Zero(t, cleaned)
	require.Equal(t, 1, pending)
}

func TestEMAIssuer_CleanupRevokesTheOriginallyGrantedAccountOrStaysPending(t *testing.T) {
	t.Parallel()
	ctx, f := newIssuerFixture(t)
	provisioner, _ := f.provisioner(t)
	_, err := provisioner.EnsureEMAIssuer(ctx, f.ti.orgID)
	require.NoError(t, err)
	kid := activeIssuerKid(t, ctx, f, f.ti.orgID)
	_, err = f.lifecycle().Contain(ctx, f.ti.orgID, &kid)
	require.NoError(t, err)
	require.NoError(t, testrepo.New(f.ti.conn).SoftDeleteExternalCredentialFixture(ctx, testrepo.SoftDeleteExternalCredentialFixtureParams{ID: f.credentialID, OrganizationID: pgtype.Text{String: "", Valid: false}}))

	require.NoError(t, testrepo.New(f.ti.conn).SetGcpIamCredentialServiceAccountFixture(ctx, testrepo.SetGcpIamCredentialServiceAccountFixtureParams{ImpersonateServiceAccount: pgtype.Text{String: "", Valid: false}, ExternalCredentialID: f.credentialID, OrganizationID: pgtype.Text{String: "", Valid: false}}))
	reconfigured := provisiontest.NewProvisioner(t, f.ti.conn, provisiontest.NewKMSClients(t).Factory, testServerURL, f.credentialID, "other-signer@example.iam.gserviceaccount.com")
	cleaned, pending, err := reconfigured.CleanupEMAIssuerKMS(ctx, f.ti.orgID)
	require.Error(t, err, "with the granted account unknown, the configured signer never stands in")
	require.Zero(t, cleaned)
	require.Equal(t, 1, pending)

	require.NoError(t, testrepo.New(f.ti.conn).SetGcpIamCredentialServiceAccountFixture(ctx, testrepo.SetGcpIamCredentialServiceAccountFixtureParams{ImpersonateServiceAccount: conv.ToPGText(provisiontest.SigningServiceAccount()), ExternalCredentialID: f.credentialID, OrganizationID: pgtype.Text{String: "", Valid: false}}))
	kms := &hookedKMSClients{inner: provisiontest.NewKMSClients(t)}
	reconfigured = provisiontest.NewProvisioner(t, f.ti.conn, kms.Factory, testServerURL, f.credentialID, "other-signer@example.iam.gserviceaccount.com")
	cleaned, pending, err = reconfigured.CleanupEMAIssuerKMS(ctx, f.ti.orgID)
	require.NoError(t, err)
	require.Equal(t, 1, cleaned)
	require.Zero(t, pending)
	require.Len(t, kms.RevokedGrants(), 1)
	require.Contains(t, kms.RevokedGrants()[0], provisiontest.SigningServiceAccount(), "the deleted credential's account, which holds the grant, loses it")
}

func TestEMAIssuer_CopiedDeploymentCannotSignOrManageSourceKeys(t *testing.T) {
	t.Parallel()
	ctx, f := newIssuerFixture(t)
	owner, kms := f.provisioner(t)
	_, err := owner.EnsureEMAIssuer(ctx, f.ti.orgID)
	require.NoError(t, err)
	kid := activeIssuerKid(t, ctx, f, f.ti.orgID)
	preview := provisiontest.NewProvisioner(t, f.ti.conn, kms.Factory, "https://preview.example.com", f.credentialID, "")
	_, err = preview.EnsureEMAIssuer(ctx, f.ti.orgID)
	require.ErrorIs(t, err, internalsessionissuers.ErrMisconfigured)
	require.ErrorIs(t, preview.RotateEMAIssuer(ctx, f.ti.orgID, kid), internalsessionissuers.ErrMisconfigured)
	require.ErrorIs(t, preview.ProvisionEMAIssuerKey(ctx, f.ti.orgID), internalsessionissuers.ErrMisconfigured)
	gate := internalsessionissuers.NewGate(f.ti.conn, f.flags, internalsessionissuers.GateConfig{Provisioning: true, SigningPaused: false})
	factory := func(ctx context.Context, ts oauth2.TokenSource) (gcpkms.SigningClient, error) {
		return kms.inner.Factory(ctx, ts)
	}
	for _, cfg := range []internalsessionissuers.SignerConfig{
		issuerSignerConfig(t, "https://preview.example.com", provisiontest.KeyRing, provisiontest.SigningServiceAccount()),
		issuerSignerConfig(t, testServerURL, "projects/preview/locations/global/keyRings/issuer", provisiontest.SigningServiceAccount()),
		{},
	} {
		signer := internalsessionissuers.NewSigner(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), f.ti.conn, gate, gcpauth.NewIdentity(gcpauth.NewStubResolver()), factory, cfg)
		assertion, err := signer.Sign(ctx, f.ti.orgID, claims())
		require.ErrorIs(t, err, internalsessionissuers.ErrMisconfigured)
		require.Empty(t, assertion)
	}
	// Containment remains database-only on any deployment, but a copied
	// deployment must leave the source deployment's KMS material untouched.
	_, err = f.lifecycle().Contain(ctx, f.ti.orgID, &kid)
	require.NoError(t, err)
	cleaned, pending, err := preview.CleanupEMAIssuerKMS(ctx, f.ti.orgID)
	require.ErrorIs(t, err, internalsessionissuers.ErrMisconfigured)
	require.Zero(t, cleaned)
	require.Equal(t, 1, pending)
	_, err = preview.SweepEMAIssuerKeys(ctx, f.ti.orgID)
	require.ErrorIs(t, err, internalsessionissuers.ErrMisconfigured)
	require.Empty(t, kms.Disabled())
	require.Empty(t, publishedIssuerKids(t, ctx, f, f.ti.orgID))
	require.NoError(t, f.lifecycle().SetDisabled(ctx, f.ti.orgID, true))
	require.Len(t, kms.Created(), 1)
}

// afterSigning runs a state change after the KMS response, before Sign returns.
type afterSigning struct {
	gcpkms.SigningClient
	after func()
}

func (s afterSigning) AsymmetricSign(ctx context.Context, resource string, alg jose.SignatureAlgorithm, digest []byte) ([]byte, error) {
	result, err := s.SigningClient.AsymmetricSign(ctx, resource, alg, digest)
	if err != nil {
		return nil, fmt.Errorf("sign before state change: %w", err)
	}
	s.after()
	return result, nil
}

func TestEMAIssuer_KillSwitchDuringSigningWins(t *testing.T) {
	t.Parallel()
	ctx, f := newIssuerFixture(t)
	owner, kms := f.provisioner(t)
	_, err := owner.EnsureEMAIssuer(ctx, f.ti.orgID)
	require.NoError(t, err)
	gate := internalsessionissuers.NewGate(f.ti.conn, f.flags, internalsessionissuers.GateConfig{Provisioning: true, SigningPaused: false})
	factory := func(ctx context.Context, ts oauth2.TokenSource) (gcpkms.SigningClient, error) {
		client, err := kms.inner.Factory(ctx, ts)
		if err != nil {
			return nil, fmt.Errorf("create test signing client: %w", err)
		}
		return afterSigning{SigningClient: client, after: func() { f.flags.SetFlag(feature.FlagInternalSessionIssuerSigningPaused, f.ti.orgID, true) }}, nil
	}
	signer := internalsessionissuers.NewSigner(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), f.ti.conn, gate, gcpauth.NewIdentity(gcpauth.NewStubResolver()), factory, issuerSignerConfig(t, testServerURL, provisiontest.KeyRing, provisiontest.SigningServiceAccount()))
	assertion, err := signer.Sign(ctx, f.ti.orgID, claims())
	requireRefused(t, err, internalsessionissuers.StateSigningPaused)
	require.Empty(t, assertion)
}

func TestEMAIssuer_SignRefusesEmailsOffTheCurrentVerifiedDomains(t *testing.T) {
	t.Parallel()
	ctx, f := newIssuerFixture(t)
	provisioner, kms := f.provisioner(t)
	_, err := provisioner.EnsureEMAIssuer(ctx, f.ti.orgID)
	require.NoError(t, err)
	signer := f.signer(t, kms, false)

	f.setVerifiedDomains(t, ctx, "example.com", "example.org")
	_, err = signer.Sign(ctx, f.ti.orgID, claims())
	require.NoError(t, err)

	f.setVerifiedDomains(t, ctx, "example.org")
	_, err = signer.Sign(ctx, f.ti.orgID, claims())
	require.ErrorIs(t, err, internalsessionissuers.ErrInvalidClaims, "a removed domain stops signing even while another remains")
}
