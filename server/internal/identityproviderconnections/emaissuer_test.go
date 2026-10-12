package identityproviderconnections_test

import (
	"context"
	"sync"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/emaissuer"
	extkeysrepo "github.com/speakeasy-api/gram/server/internal/externalkeys/repo"
	"github.com/speakeasy-api/gram/server/internal/identityproviderconnections"
	"github.com/speakeasy-api/gram/server/internal/identityproviderconnections/provisiontest"
	"github.com/speakeasy-api/gram/server/internal/identityproviderconnections/repo"
	jwksrepo "github.com/speakeasy-api/gram/server/internal/jsonwebkeysets/repo"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
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

type issuerFixture struct {
	ti           *testInstance
	credentialID uuid.UUID
}

func newIssuerFixture(t *testing.T) (context.Context, issuerFixture) {
	t.Helper()
	ctx, ti := newTestDB(t)
	return ctx, issuerFixture{ti: ti, credentialID: provisiontest.CreatePlatformSigningCredential(t, ctx, ti.conn)}
}

// provisioner returns a provisioner minting from its own in-process key, so
// each provisioner's keys have a distinct kid.
func (f issuerFixture) provisioner(t *testing.T) (*identityproviderconnections.Provisioner, *hookedKMSClients) {
	t.Helper()
	kms := &hookedKMSClients{inner: provisiontest.NewKMSClients(t)}
	return provisiontest.NewProvisioner(t, f.ti.conn, kms.Factory, testServerURL, f.credentialID, ""), kms
}

// signer signs through the same in-process key as kms.
func (f issuerFixture) signer(t *testing.T, kms *hookedKMSClients, cfg emaissuer.SignerConfig) *emaissuer.Signer {
	t.Helper()
	factory := func(ctx context.Context, ts oauth2.TokenSource) (gcpkms.SigningClient, error) {
		return kms.inner.Factory(ctx, ts)
	}
	return emaissuer.NewSigner(testenv.NewLogger(t), f.ti.conn, mustURL(t, testServerURL), gcpauth.NewIdentity(gcpauth.NewStubResolver()), factory, cfg)
}

func issuerURL(t *testing.T, orgID string) string {
	t.Helper()
	issuer, err := emaissuer.URL(mustURL(t, testServerURL), orgID)
	require.NoError(t, err)
	return issuer.String()
}

func publishedIssuerKids(t *testing.T, ctx context.Context, f issuerFixture, orgID string) []string {
	t.Helper()
	set, err := emaissuer.PublicKeys(ctx, f.ti.conn, orgID)
	require.NoError(t, err)
	kids := make([]string, 0, len(set.Keys))
	for _, key := range set.Keys {
		kids = append(kids, key.KeyID)
	}
	return kids
}

func activeIssuerKid(t *testing.T, ctx context.Context, f issuerFixture, orgID string) string {
	t.Helper()
	q := jwksrepo.New(f.ti.conn)
	set, err := q.GetOrganizationEMAIssuer(ctx, orgID)
	require.NoError(t, err)
	key, err := q.GetActiveJsonWebKey(ctx, jwksrepo.GetActiveJsonWebKeyParams{JsonWebKeySetID: set.ID, OrganizationID: orgID})
	require.NoError(t, err)
	return key.Kid
}

// verifyAssertion checks an assertion against the published keys, as a vendor does.
func verifyAssertion(t *testing.T, ctx context.Context, f issuerFixture, orgID, assertion string) map[string]any {
	t.Helper()
	parsed, err := jwt.ParseSigned(assertion, []jose.SignatureAlgorithm{jose.RS256, jose.ES256})
	require.NoError(t, err)
	require.Equal(t, emaissuer.AssertionType, parsed.Headers[0].ExtraHeaders[jose.HeaderType])
	keys, err := emaissuer.PublicKeys(ctx, f.ti.conn, orgID)
	require.NoError(t, err)
	var claims map[string]any
	require.NoError(t, parsed.Claims(keys, &claims))
	return claims
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
}

func TestEMAIssuer_RotateKeepsURLAndOverlaps(t *testing.T) {
	t.Parallel()
	ctx, f := newIssuerFixture(t)
	provisioner, kms := f.provisioner(t)
	setID, err := provisioner.EnsureEMAIssuer(ctx, f.ti.orgID)
	require.NoError(t, err)
	before := activeIssuerKid(t, ctx, f, f.ti.orgID)

	assertion, err := f.signer(t, kms, emaissuer.SignerConfig{}).Sign(ctx, f.ti.orgID, map[string]any{"sub": "user-1"})
	require.NoError(t, err)
	require.Equal(t, issuerURL(t, f.ti.orgID), verifyAssertion(t, ctx, f, f.ti.orgID, assertion)["iss"])

	rotator, rotatingKMS := f.provisioner(t)
	err = rotator.RotateEMAIssuer(ctx, f.ti.orgID, before)
	var pending *emaissuer.RotationPendingError
	require.ErrorAs(t, err, &pending)
	require.Len(t, publishedIssuerKids(t, ctx, f, f.ti.orgID), 2, "pending key is published before use")
	require.Equal(t, before, activeIssuerKid(t, ctx, f, f.ti.orgID), "publication must not change the signer")

	err = rotator.RotateEMAIssuer(ctx, f.ti.orgID, before)
	require.ErrorAs(t, err, &pending, "activation waits for verifier caches")
	require.Len(t, rotatingKMS.Created(), 1, "retry reuses the pending key")

	q := repo.New(f.ti.conn)
	require.NoError(t, q.BackdatePendingRotationPublication(ctx, repo.BackdatePendingRotationPublicationParams{JsonWebKeySetID: setID, OrganizationID: f.ti.orgID, AgeSeconds: int32(emaissuer.CacheTTL / time.Second)}))
	require.NoError(t, rotator.RotateEMAIssuer(ctx, f.ti.orgID, before))
	after := activeIssuerKid(t, ctx, f, f.ti.orgID)
	require.NotEqual(t, before, after)
	require.ElementsMatch(t, []string{before, after}, publishedIssuerKids(t, ctx, f, f.ti.orgID), "retired key verifies during the overlap")

	set, err := jwksrepo.New(f.ti.conn).GetOrganizationEMAIssuer(ctx, f.ti.orgID)
	require.NoError(t, err)
	require.Equal(t, setID, set.ID, "rotation keeps one issuer")
	assertion, err = f.signer(t, rotatingKMS, emaissuer.SignerConfig{}).Sign(ctx, f.ti.orgID, map[string]any{"sub": "user-1"})
	require.NoError(t, err)
	require.Equal(t, issuerURL(t, f.ti.orgID), verifyAssertion(t, ctx, f, f.ti.orgID, assertion)["iss"], "issuer URL survives rotation")

	// A retry after activation is a no-op.
	require.NoError(t, rotator.RotateEMAIssuer(ctx, f.ti.orgID, before))
	require.Len(t, rotatingKMS.Created(), 1)

	// After the overlap the retired key leaves the document, then is revoked
	// and disabled in KMS on the next rotation call.
	require.NoError(t, q.BackdateRetiredJsonWebKeys(ctx, repo.BackdateRetiredJsonWebKeysParams{JsonWebKeySetID: setID, OrganizationID: f.ti.orgID, AgeSeconds: int32(emaissuer.RetiredKeyOverlap/time.Second) + 1}))
	require.Equal(t, []string{after}, publishedIssuerKids(t, ctx, f, f.ti.orgID))
	require.NoError(t, rotator.RotateEMAIssuer(ctx, f.ti.orgID, before))
	require.Equal(t, kms.Created()[:1], rotatingKMS.Disabled())

	err = rotator.RotateEMAIssuer(ctx, f.ti.orgID, "never-published")
	require.ErrorIs(t, err, emaissuer.ErrUnknownKid)
}

func TestEMAIssuer_ReplaceRevokesImmediately(t *testing.T) {
	t.Parallel()
	ctx, f := newIssuerFixture(t)
	provisioner, kms := f.provisioner(t)
	_, err := provisioner.EnsureEMAIssuer(ctx, f.ti.orgID)
	require.NoError(t, err)
	compromised := activeIssuerKid(t, ctx, f, f.ti.orgID)

	replacer, replacingKMS := f.provisioner(t)
	purger := &countingPurger{}
	replacer.SetEMAIssuerTokenPurger(purger)
	require.NoError(t, replacer.ReplaceEMAIssuerKey(ctx, f.ti.orgID, compromised))

	replacement := activeIssuerKid(t, ctx, f, f.ti.orgID)
	require.NotEqual(t, compromised, replacement)
	require.Equal(t, []string{replacement}, publishedIssuerKids(t, ctx, f, f.ti.orgID), "the revoked key is withdrawn at once")
	require.Equal(t, kms.Created(), replacingKMS.Disabled(), "the compromised version is disabled in KMS")
	require.Equal(t, []string{f.ti.orgID}, purger.Purged())

	assertion, err := f.signer(t, replacingKMS, emaissuer.SignerConfig{}).Sign(ctx, f.ti.orgID, map[string]any{"sub": "user-1"})
	require.NoError(t, err)
	require.Equal(t, issuerURL(t, f.ti.orgID), verifyAssertion(t, ctx, f, f.ti.orgID, assertion)["iss"])

	require.ErrorIs(t, replacer.ReplaceEMAIssuerKey(ctx, f.ti.orgID, compromised), emaissuer.ErrKeyNotFound)
}

func TestEMAIssuer_DisableWithdrawsAndReenableKeepsURL(t *testing.T) {
	t.Parallel()
	ctx, f := newIssuerFixture(t)
	provisioner, kms := f.provisioner(t)
	purger := &countingPurger{}
	provisioner.SetEMAIssuerTokenPurger(purger)
	first, err := provisioner.EnsureEMAIssuer(ctx, f.ti.orgID)
	require.NoError(t, err)

	require.NoError(t, provisioner.DisableEMAIssuer(ctx, f.ti.orgID))
	_, err = emaissuer.PublicKeys(ctx, f.ti.conn, f.ti.orgID)
	require.ErrorIs(t, err, emaissuer.ErrNotFound)
	_, err = f.signer(t, kms, emaissuer.SignerConfig{}).Sign(ctx, f.ti.orgID, map[string]any{})
	require.ErrorIs(t, err, emaissuer.ErrNotFound)
	require.Equal(t, kms.Created(), kms.Disabled())
	require.Equal(t, []string{f.ti.orgID}, purger.Purged())
	require.ErrorIs(t, provisioner.DisableEMAIssuer(ctx, f.ti.orgID), emaissuer.ErrNotFound)

	reenabler, reenablingKMS := f.provisioner(t)
	second, err := reenabler.EnsureEMAIssuer(ctx, f.ti.orgID)
	require.NoError(t, err)
	require.NotEqual(t, first, second)
	assertion, err := f.signer(t, reenablingKMS, emaissuer.SignerConfig{}).Sign(ctx, f.ti.orgID, map[string]any{})
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

	_, err = emaissuer.PublicKeys(ctx, f.ti.conn, other)
	require.ErrorIs(t, err, emaissuer.ErrNotFound, "another organization resolves no issuer")
	_, err = f.signer(t, kms, emaissuer.SignerConfig{}).Sign(ctx, other, map[string]any{})
	require.ErrorIs(t, err, emaissuer.ErrNotFound, "another organization cannot sign with this key")
	require.ErrorIs(t, provisioner.RotateEMAIssuer(ctx, other, kid), emaissuer.ErrNotFound)
	require.ErrorIs(t, provisioner.ReplaceEMAIssuerKey(ctx, other, kid), emaissuer.ErrNotFound)
	require.ErrorIs(t, provisioner.DisableEMAIssuer(ctx, other), emaissuer.ErrNotFound)
	require.Equal(t, []string{kid}, publishedIssuerKids(t, ctx, f, f.ti.orgID), "org A's issuer is untouched")
}

func TestEMAIssuer_KillSwitchRefusesSigning(t *testing.T) {
	t.Parallel()
	ctx, f := newIssuerFixture(t)
	provisioner, kms := f.provisioner(t)
	_, err := provisioner.EnsureEMAIssuer(ctx, f.ti.orgID)
	require.NoError(t, err)

	signer := f.signer(t, kms, emaissuer.SignerConfig{Disabled: true})
	require.False(t, signer.Enabled())
	_, err = signer.Sign(ctx, f.ti.orgID, map[string]any{})
	require.ErrorIs(t, err, emaissuer.ErrSigningDisabled)
	require.Len(t, publishedIssuerKids(t, ctx, f, f.ti.orgID), 1, "discovery keeps serving")
}

func TestEMAIssuer_HiddenFromCustomerKeyAPIs(t *testing.T) {
	t.Parallel()
	ctx, f := newIssuerFixture(t)
	provisioner, _ := f.provisioner(t)
	setID, err := provisioner.EnsureEMAIssuer(ctx, f.ti.orgID)
	require.NoError(t, err)
	jq := jwksrepo.New(f.ti.conn)
	set, err := jq.GetOrganizationEMAIssuer(ctx, f.ti.orgID)
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
