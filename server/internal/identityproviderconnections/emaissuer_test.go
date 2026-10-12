package identityproviderconnections_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"
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
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/gcp/gcpauth"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/gcp/gcpkms"
)

type countingPurger struct {
	mu   sync.Mutex
	orgs []string
}

func (p *countingPurger) PurgeEMAIssuerTokens(_ context.Context, organizationID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.orgs = append(p.orgs, organizationID)
	return nil
}

func (p *countingPurger) Purged() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.orgs...)
}

// entitlements grants or withholds the SSO entitlement for every organization.
type entitlements struct{ sso bool }

func (e *entitlements) IsFeatureEnabledUncached(context.Context, string, productfeatures.Feature) (bool, error) {
	return e.sso, nil
}

type issuerFixture struct {
	ti           *testInstance
	credentialID uuid.UUID
	flags        *feature.InMemory
	entitled     *entitlements
}

func newIssuerFixture(t *testing.T) (context.Context, issuerFixture) {
	t.Helper()
	ctx, ti := newTestDB(t)
	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagInternalSessionIssuer, ti.orgID, true)
	return ctx, issuerFixture{ti: ti, credentialID: provisiontest.CreatePlatformSigningCredential(t, ctx, ti.conn), flags: flags, entitled: &entitlements{sso: true}}
}

// provisioner returns a provisioner minting from its own in-process key, so
// each provisioner's keys have a distinct kid.
func (f issuerFixture) provisioner(t *testing.T) (*identityproviderconnections.Provisioner, *hookedKMSClients) {
	t.Helper()
	kms := &hookedKMSClients{inner: provisiontest.NewKMSClients(t)}
	return provisiontest.NewProvisioner(t, f.ti.conn, kms.Factory, testServerURL, f.credentialID, ""), kms
}

// signer signs through the same in-process key as kms.
func (f issuerFixture) signer(t *testing.T, kms *hookedKMSClients, paused bool) *internalsessionissuers.Signer {
	t.Helper()
	factory := func(ctx context.Context, ts oauth2.TokenSource) (gcpkms.SigningClient, error) {
		return kms.inner.Factory(ctx, ts)
	}
	gate := internalsessionissuers.NewGate(f.ti.conn, f.entitled, f.flags, internalsessionissuers.GateConfig{Provisioning: true, SigningPaused: paused})
	return internalsessionissuers.NewSigner(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), f.ti.conn, gate, gcpauth.NewIdentity(gcpauth.NewStubResolver()), factory, "")
}

func claims() internalsessionissuers.Claims {
	return internalsessionissuers.Claims{Subject: "user-1", Email: "user-1@example.com", Audience: "https://vendor.example.com", ClientID: "client-1", Resource: "https://vendor.example.com/mcp", Scope: "read", ID: uuid.NewString(), Expiry: time.Now().Add(time.Minute), Extra: nil}
}

func issuerURL(t *testing.T, orgID string) string {
	t.Helper()
	issuer, err := internalsessionissuers.URL(mustURL(t, testServerURL), orgID)
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

	require.NoError(t, rotator.SweepEMAIssuerKeys(ctx, f.ti.orgID), "nothing is due during the overlap")
	require.Empty(t, rotatingKMS.Disabled())

	require.NoError(t, q.BackdateRetiredJsonWebKeys(ctx, repo.BackdateRetiredJsonWebKeysParams{JsonWebKeySetID: setID, OrganizationID: f.ti.orgID, AgeSeconds: int32(internalsessionissuers.RetiredKeyOverlap/time.Second) + 1}))
	require.Equal(t, []string{after}, publishedIssuerKids(t, ctx, f, f.ti.orgID), "the retired key leaves the document after the overlap")
	swept, err := rotator.SweepAllEMAIssuerKeys(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, swept)
	require.Equal(t, kms.Created()[:1], rotatingKMS.Disabled(), "the retired KMS version is disabled")
	require.Equal(t, int64(1), auditCount(t, ctx, f, audit.ActionJsonWebKeyRevoke))
	swept, err = rotator.SweepAllEMAIssuerKeys(ctx)
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

	replacer, replacingKMS := f.provisioner(t)
	purger := &countingPurger{}
	replacer.SetEMAIssuerTokenPurger(purger)
	require.NoError(t, replacer.RotateEMAIssuerCompromised(ctx, f.ti.orgID))

	replacement := activeIssuerKid(t, ctx, f, f.ti.orgID)
	require.NotEqual(t, compromised, replacement)
	require.Equal(t, []string{replacement}, publishedIssuerKids(t, ctx, f, f.ti.orgID), "the active and pending keys are withdrawn at once")
	require.ElementsMatch(t, append(kms.Created(), rotatingKMS.Created()...), slices.Compact(slices.Sorted(slices.Values(replacingKMS.Disabled()))), "every compromised version is disabled in KMS")
	require.Equal(t, []string{f.ti.orgID}, purger.Purged())
	require.Equal(t, int64(2), auditCount(t, ctx, f, audit.ActionJsonWebKeyRevoke))

	assertion, err := f.signer(t, replacingKMS, false).Sign(ctx, f.ti.orgID, claims())
	require.NoError(t, err)
	require.Equal(t, issuerURL(t, f.ti.orgID), verifyAssertion(t, ctx, f, f.ti.orgID, assertion)["iss"])

	// A later routine rotation can't resurrect the withdrawn pending key.
	next, nextKMS := f.provisioner(t)
	require.ErrorAs(t, next.RotateEMAIssuer(ctx, f.ti.orgID, replacement), &pending)
	require.Len(t, nextKMS.Created(), 1, "it publishes a fresh key")
}

func TestEMAIssuer_DisableStopsSigningAndReenableResumes(t *testing.T) {
	t.Parallel()
	ctx, f := newIssuerFixture(t)
	provisioner, kms := f.provisioner(t)
	purger := &countingPurger{}
	provisioner.SetEMAIssuerTokenPurger(purger)
	first, err := provisioner.EnsureEMAIssuer(ctx, f.ti.orgID)
	require.NoError(t, err)
	kid := activeIssuerKid(t, ctx, f, f.ti.orgID)
	signer := f.signer(t, kms, false)

	require.NoError(t, provisioner.DisableEMAIssuer(ctx, f.ti.orgID))
	require.Equal(t, []string{kid}, publishedIssuerKids(t, ctx, f, f.ti.orgID), "discovery keeps serving while disabled")
	_, err = signer.Sign(ctx, f.ti.orgID, claims())
	requireRefused(t, err, internalsessionissuers.StateDisabled)
	require.Empty(t, kms.Disabled(), "disabling keeps the key")
	require.Equal(t, []string{f.ti.orgID}, purger.Purged())
	require.NoError(t, provisioner.DisableEMAIssuer(ctx, f.ti.orgID), "disabling twice is a no-op")
	require.Equal(t, int64(1), auditCount(t, ctx, f, audit.ActionInternalSessionIssuerDisable))

	// A deployment whose origin moved re-enables the same pinned issuer.
	moved := provisiontest.NewProvisioner(t, f.ti.conn, kms.Factory, "https://moved.example.com", f.credentialID, "")
	second, err := moved.EnsureEMAIssuer(ctx, f.ti.orgID)
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

	_, _, err = internalsessionissuers.PublicKeys(ctx, f.ti.conn, other)
	require.ErrorIs(t, err, internalsessionissuers.ErrNotFound, "another organization resolves no issuer")
	_, err = f.signer(t, kms, false).Sign(ctx, other, claims())
	requireRefused(t, err, internalsessionissuers.StateSetupRequired)
	require.ErrorIs(t, provisioner.RotateEMAIssuer(ctx, other, kid), internalsessionissuers.ErrNotFound)
	require.ErrorIs(t, provisioner.RotateEMAIssuerCompromised(ctx, other), internalsessionissuers.ErrNotFound)
	require.ErrorIs(t, provisioner.DisableEMAIssuer(ctx, other), internalsessionissuers.ErrNotFound)
	require.NoError(t, provisioner.SweepEMAIssuerKeys(ctx, other))
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

	f.entitled.sso = false
	_, err = f.signer(t, kms, false).Sign(ctx, f.ti.orgID, claims())
	requireRefused(t, err, internalsessionissuers.StateEntitlementRequired)
	f.entitled.sso = true

	f.flags.SetFlag(feature.FlagInternalSessionIssuer, f.ti.orgID, false)
	_, err = f.signer(t, kms, false).Sign(ctx, f.ti.orgID, claims())
	requireRefused(t, err, internalsessionissuers.StateUnavailable)
	f.flags.SetFlag(feature.FlagInternalSessionIssuer, f.ti.orgID, true)

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
		"no subject":  func(c *internalsessionissuers.Claims) { c.Subject = "" },
		"no audience": func(c *internalsessionissuers.Claims) { c.Audience = "" },
		"no client":   func(c *internalsessionissuers.Claims) { c.ClientID = "" },
		"no jti":      func(c *internalsessionissuers.Claims) { c.ID = "" },
		"no expiry":   func(c *internalsessionissuers.Claims) { c.Expiry = time.Time{} },
		"expired":     func(c *internalsessionissuers.Claims) { c.Expiry = time.Now().Add(-time.Second) },
		"outlives overlap": func(c *internalsessionissuers.Claims) {
			c.Expiry = time.Now().Add(internalsessionissuers.MaxAssertionLifetime + time.Minute)
		},
		"overrides iss": func(c *internalsessionissuers.Claims) {
			c.Extra = map[string]any{"iss": "https://attacker.example.com"}
		},
		"overrides iat":     func(c *internalsessionissuers.Claims) { c.Extra = map[string]any{"iat": 1} },
		"overrides subject": func(c *internalsessionissuers.Claims) { c.Extra = map[string]any{"email": "someone-else@example.com"} },
	} {
		c := claims()
		mutate(&c)
		_, err := signer.Sign(ctx, f.ti.orgID, c)
		require.ErrorIs(t, err, internalsessionissuers.ErrInvalidClaims, name)
	}

	c := claims()
	c.Extra = map[string]any{"tenant": "tenant-1"}
	issuedAt := time.Now().Add(-time.Second)
	assertion, err := signer.Sign(ctx, f.ti.orgID, c)
	require.NoError(t, err)
	body := verifyAssertion(t, ctx, f, f.ti.orgID, assertion)
	require.Equal(t, "tenant-1", body["tenant"])
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

func TestEMAIssuer_CompromisedRotationFailsIfKMSCannotDisable(t *testing.T) {
	t.Parallel()
	ctx, f := newIssuerFixture(t)
	provisioner, _ := f.provisioner(t)
	_, err := provisioner.EnsureEMAIssuer(ctx, f.ti.orgID)
	require.NoError(t, err)
	compromised := activeIssuerKid(t, ctx, f, f.ti.orgID)

	replacer, replacingKMS := f.provisioner(t)
	replacingKMS.disableErr = errors.New("kms unavailable")
	require.Error(t, replacer.RotateEMAIssuerCompromised(ctx, f.ti.orgID), "success must mean the compromised versions are disabled")
	require.Equal(t, compromised, activeIssuerKid(t, ctx, f, f.ti.orgID), "nothing is replaced until KMS confirms")
	require.Equal(t, []string{compromised}, publishedIssuerKids(t, ctx, f, f.ti.orgID))
}
