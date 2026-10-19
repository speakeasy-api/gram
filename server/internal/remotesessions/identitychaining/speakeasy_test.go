package identitychaining

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/oauth2"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/conv"
	extcredrepo "github.com/speakeasy-api/gram/server/internal/externalcredentials/repo"
	extkeysrepo "github.com/speakeasy-api/gram/server/internal/externalkeys/repo"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/internalsessionissuers"
	jwksrepo "github.com/speakeasy-api/gram/server/internal/jsonwebkeysets/repo"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/signins"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/gcp/gcpauth"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/gcp/gcpkms"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// speakeasyLiveFixture is a Speakeasy-backed binding whose resource
// authorization server is a live test endpoint that verifies every signed
// assertion and counts redemptions.
type speakeasyLiveFixture struct {
	f                    chainStoreFixture
	logger               *slog.Logger
	tp                   trace.TracerProvider
	mp                   metric.MeterProvider
	flags                *feature.InMemory
	gate                 *internalsessionissuers.Gate
	signer               *internalsessionissuers.Signer
	keys                 gcpkms.SigningClient
	base                 *url.URL
	ring, account        string
	status               internalsessionissuers.Status
	publicJWK            []byte
	signIn               signins.Recorded
	remoteID             uuid.UUID
	clientID             uuid.UUID
	resource             string
	policy               *guardian.Policy
	sharedCache          cache.Cache
	challenges           *remotesessions.ChallengeManager
	chainer              *Chainer
	calls                *atomic.Int32
	metadataCalls        *atomic.Int32
	metadataUnavailable  *atomic.Bool
	revokeDuringExchange *atomic.Bool
	rejectRedemption     *atomic.Pointer[string]
	checkExchange        func()
}

func newSpeakeasyLiveFixture(t *testing.T) *speakeasyLiveFixture {
	t.Helper()
	f := newChainStoreFixture(t)
	useSpeakeasySignIn(t, f.db, f.req)
	ctx := t.Context()
	logger := testenv.NewLogger(t)
	tp := testenv.NewTracerProvider(t)
	mp := testenv.NewMeterProvider(t)
	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagInternalSessionIssuer, f.req.OrganizationID, true)
	flags.SetFlag(feature.FlagInternalSessionIssuerSigningPaused, f.req.OrganizationID, false)
	signins.EnableForTests(flags, f.req.OrganizationID)
	gate := internalsessionissuers.NewGate(f.db, flags, internalsessionissuers.GateConfig{Provisioning: true, SigningPaused: false})
	require.NoError(t, orgrepo.New(f.db).SetVerifiedDomains(ctx, orgrepo.SetVerifiedDomainsParams{ID: f.req.OrganizationID, VerifiedDomains: []string{"example.test"}}))
	const origin = "https://issuer.example.test"
	const ring = "projects/gram-stub/locations/global/keyRings/ema-test"
	const resource = ring + "/cryptoKeys/" + internalsessionissuers.KMSKeyPrefix + "-test/cryptoKeyVersions/1"
	const account = "ema-signer@gram-stub.iam.gserviceaccount.com"
	keys, err := gcpkms.NewLocalSigningClient(jose.RS256)
	require.NoError(t, err)
	public, err := keys.GetPublicKey(ctx, resource)
	require.NoError(t, err)
	publicJSON, err := json.Marshal(jose.JSONWebKey{Key: public.Key, KeyID: "test-key", Algorithm: string(jose.RS256), Use: "sig"})
	require.NoError(t, err)
	ec, err := extcredrepo.New(f.db).CreateExternalCredential(ctx, extcredrepo.CreateExternalCredentialParams{Provider: "gcp_iam", Name: "EMA signing"})
	require.NoError(t, err)
	_, err = extcredrepo.New(f.db).CreateGcpIamCredential(ctx, extcredrepo.CreateGcpIamCredentialParams{ExternalCredentialID: ec.ID, ImpersonateServiceAccount: conv.ToPGText(account), SkipProjectVerification: true})
	require.NoError(t, err)
	ek, err := extkeysrepo.New(f.db).CreateExternalKey(ctx, extkeysrepo.CreateExternalKeyParams{OrganizationID: conv.ToPGText(f.req.OrganizationID), ExternalCredentialID: ec.ID, Provider: "gcp_kms", Algorithm: string(jose.RS256), Name: "EMA signing"})
	require.NoError(t, err)
	_, err = extkeysrepo.New(f.db).CreateGcpKmsKey(ctx, extkeysrepo.CreateGcpKmsKeyParams{ExternalKeyID: ek.ID, ResourceName: resource})
	require.NoError(t, err)
	set, err := jwksrepo.New(f.db).CreateJsonWebKeySet(ctx, jwksrepo.CreateJsonWebKeySetParams{OrganizationID: f.req.OrganizationID, ExternalKeyID: ek.ID, Name: "EMA issuer"})
	require.NoError(t, err)
	base, err := url.Parse(origin)
	require.NoError(t, err)
	issuerURL, err := internalsessionissuers.URL(base, f.req.OrganizationID, false)
	require.NoError(t, err)
	issuer, err := jwksrepo.New(f.db).CreateInternalSessionIssuer(ctx, jwksrepo.CreateInternalSessionIssuerParams{OrganizationID: f.req.OrganizationID, JsonWebKeySetID: set.ID, Issuer: issuerURL.String()})
	require.NoError(t, err)
	_, err = jwksrepo.New(f.db).CreateJsonWebKey(ctx, jwksrepo.CreateJsonWebKeyParams{OrganizationID: f.req.OrganizationID, JsonWebKeySetID: set.ID, ExternalKeyID: ek.ID, State: "active", Kid: "test-key", PublicJwk: publicJSON})
	require.NoError(t, err)
	issuerID := issuer.ID
	signer := internalsessionissuers.NewSigner(logger, tp, mp, f.db, gate, gcpauth.NewIdentity(gcpauth.NewStubResolver()), func(context.Context, oauth2.TokenSource) (gcpkms.SigningClient, error) { return keys, nil }, internalsessionissuers.SignerConfig{Deployment: internalsessionissuers.Deployment{ServerURL: base, KeyRing: ring}, ManagedSigner: account})
	gate.SetSigningAuthority(signer.CheckAuthority)
	status, err := gate.Status(ctx, f.req.OrganizationID)
	require.NoError(t, err)
	require.Equal(t, internalsessionissuers.StateEnabled, status.State)
	var key jose.JSONWebKey
	require.NoError(t, key.UnmarshalJSON(status.ActiveKey.PublicJwk))
	signIn, err := signins.Record(ctx, f.db, signins.RecordParams{OrganizationID: f.req.OrganizationID, UserID: f.req.UserID, WorkOSSessionID: uuid.NewString(), AuthenticationMethod: signins.MethodSSO, AuthenticatedAt: time.Now().Add(-time.Minute), Evidence: signins.Evidence{VerifiedEmail: f.req.UserID + "@example.test", IDPAuthenticatedAt: time.Now().Add(-time.Minute)}})
	require.NoError(t, err)
	f.req.UserSessionJTI = uuid.NewString()

	calls := new(atomic.Int32)
	metadataCalls := new(atomic.Int32)
	metadataUnavailable := new(atomic.Bool)
	revokeDuringExchange := new(atomic.Bool)
	rejectRedemption := new(atomic.Pointer[string])
	exchangeErrors := make(chan error, 4)
	checkExchange := func() {
		t.Helper()
		select {
		case err := <-exchangeErrors:
			require.NoError(t, err)
		case <-time.After(5 * time.Second):
			t.Fatal("expected assertion exchange did not complete")
		}
	}
	var expectedAudience string
	endpoint := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/oauth-protected-resource/mcp" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Path == "/.well-known/oauth-protected-resource" {
			metadataCalls.Add(1)
			if metadataUnavailable.Load() {
				http.Error(w, "unavailable", http.StatusServiceUnavailable)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"resource": expectedAudience, "authorization_servers": []string{expectedAudience}})
			return
		}
		calls.Add(1)
		fail := func(err error) { exchangeErrors <- err; http.Error(w, "invalid test exchange", http.StatusBadRequest) }
		if code := rejectRedemption.Load(); code != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": *code})
			exchangeErrors <- nil
			return
		}
		if revokeDuringExchange.Load() {
			if _, err := signins.RevokeUser(ctx, f.db, []string{f.req.OrganizationID}, f.req.UserID, signins.RevokeReasonAdmin); err != nil {
				fail(fmt.Errorf("revoke during exchange: %w", err))
				return
			}
		}
		if err := r.ParseForm(); err != nil {
			fail(fmt.Errorf("parse exchange form: %w", err))
			return
		}
		if r.Form.Get(oauthwire.ParamGrantType) != oauthwire.GrantTypeJWTBearer {
			fail(fmt.Errorf("unexpected exchange grant type"))
			return
		}
		assertion, err := jwt.ParseSigned(r.Form.Get(oauthwire.ParamAssertion), []jose.SignatureAlgorithm{jose.RS256})
		if err != nil {
			fail(fmt.Errorf("parse assertion: %w", err))
			return
		}
		var claims struct {
			jwt.Claims
			Email    string `json:"email"`
			ClientID string `json:"client_id"`
			Resource string `json:"resource"`
			Scope    string `json:"scope"`
		}
		if err := assertion.Claims(key.Key, &claims); err != nil {
			fail(fmt.Errorf("verify assertion: %w", err))
			return
		}
		if err := claims.Validate(jwt.Expected{Issuer: status.Issuer.Issuer, Subject: f.req.UserID, AnyAudience: jwt.Audience{expectedAudience}, Time: time.Now()}); err != nil {
			fail(fmt.Errorf("validate assertion claims: %w", err))
			return
		}
		if claims.ID == "" || claims.Expiry == nil || claims.Expiry.Time().After(time.Now().Add(2*time.Minute)) {
			fail(fmt.Errorf("missing assertion ID or invalid lifetime"))
			return
		}
		if claims.Email != f.req.UserID+"@example.test" || claims.ClientID != testResourceClient || claims.Resource != expectedAudience || claims.Scope != "read" {
			fail(fmt.Errorf("unexpected assertion identity or authorization claims"))
			return
		}
		if typ, ok := assertion.Headers[0].ExtraHeaders[jose.HeaderType].(string); !ok || typ != internalsessionissuers.AssertionType {
			fail(fmt.Errorf("unexpected assertion type"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		err = json.NewEncoder(w).Encode(map[string]any{"access_token": "live-downstream", "token_type": "Bearer", "expires_in": 3600, "scope": "read"})
		if err != nil {
			exchangeErrors <- fmt.Errorf("encode exchange response: %w", err)
			return
		}
		exchangeErrors <- nil
	}))
	t.Cleanup(endpoint.Close)
	expectedAudience = endpoint.URL
	// Replace the fixture binding's resource client with a live HTTP endpoint.
	remoteID, clientID := prepareResourceClient(t, f.db, f.chainer.enc, f.req.OrganizationID, endpoint.URL, endpoint.URL, testResourceClient, "resource-secret")
	bindResource(t, f.db, f.req, remoteID, clientID, endpoint.URL, remotesessions.PreparationStateReady)
	// Retire the original fixture binding before testing the live resource.
	f.setBinding(t, remotesessions.PreparationStateUnlinked)
	f.req.UpstreamResource = endpoint.URL + "/mcp"
	require.NoError(t, repo.New(f.db).InsertEMAProvider(ctx, repo.InsertEMAProviderParams{OrganizationID: f.req.OrganizationID, VendorIssuer: endpoint.URL, InternalSessionIssuerID: uuid.NullUUID{UUID: issuerID, Valid: true}}))
	roots := x509.NewCertPool()
	roots.AddCert(endpoint.Certificate())
	policy, err := guardian.NewUnsafePolicy(tp, nil, guardian.WithTLSRootCAs(roots))
	require.NoError(t, err)
	sharedCache := cache.NewRedisCacheAdapter(redisClient)
	challenges := remotesessions.NewChallengeManager(logger, tp, mp, f.db, f.chainer.enc, policy, nil, sharedCache, base)
	chainer := New(logger, f.db, f.chainer.enc, challenges, nil, nil, sharedCache)
	chainer.SetSpeakeasyIssuer(gate, signer, flags)
	chainer.SetResourceResolver(challenges.ResolveEMAResource)
	return &speakeasyLiveFixture{
		f: f, logger: logger, tp: tp, mp: mp, flags: flags, gate: gate, signer: signer, keys: keys, base: base, ring: ring, account: account,
		status: status, publicJWK: publicJSON, signIn: signIn, remoteID: remoteID, clientID: clientID, resource: endpoint.URL, policy: policy, sharedCache: sharedCache, challenges: challenges, chainer: chainer,
		calls: calls, metadataCalls: metadataCalls, metadataUnavailable: metadataUnavailable, revokeDuringExchange: revokeDuringExchange,
		rejectRedemption: rejectRedemption, checkExchange: checkExchange,
	}
}

func TestSpeakeasy_SignedExchangeAndRevocation(t *testing.T) {
	t.Parallel()
	lf := newSpeakeasyLiveFixture(t)
	f, ctx, logger, tp, mp := lf.f, t.Context(), lf.logger, lf.tp, lf.mp
	flags, gate, signer, signIn := lf.flags, lf.gate, lf.signer, lf.signIn
	base, ring, account, keys := lf.base, lf.ring, lf.account, lf.keys
	remoteID, policy, sharedCache, challenges, chainer := lf.remoteID, lf.policy, lf.sharedCache, lf.challenges, lf.chainer
	calls, metadataCalls, metadataUnavailable, revokeDuringExchange, checkExchange := lf.calls, lf.metadataCalls, lf.metadataUnavailable, lf.revokeDuringExchange, lf.checkExchange
	// First consent can resolve a parent resource without a prior MCP request,
	// and runs the live exchange for the consent sign-in.
	preGrant := f.req
	preGrant.UserSessionJTI = ""
	consentOutcome := chainer.CheckConsent(ctx, preGrant, signIn.ID.UUID)
	checkExchange()
	require.True(t, consentOutcome.Succeeded(), "%+v", consentOutcome)
	require.EqualValues(t, 1, calls.Load(), "consent redeems once for its sign-in")
	governor := NewGovernor(logger, f.db, f.chainer.enc, nil)
	governor.SetSpeakeasyIssuer(gate, signer, flags)
	governor.SetResourceResolver(challenges.ResolveEMAResource)
	governor.SetCachedResourceResolver(challenges.CachedEMAResource)
	served, ready := governor.Serves(ctx, f.req)
	require.True(t, ready)
	require.Equal(t, remoteID, served)
	require.EqualValues(t, 1, metadataCalls.Load())
	// A second replica reads the shared result while discovery is unavailable.
	metadataUnavailable.Store(true)
	otherChallenges := remotesessions.NewChallengeManager(logger, tp, mp, f.db, f.chainer.enc, policy, nil, sharedCache, base)
	chainer.SetResourceResolver(otherChallenges.ResolveEMAResource)
	require.True(t, chainer.CheckConsent(ctx, preGrant, signIn.ID.UUID).Succeeded())
	require.EqualValues(t, 1, calls.Load(), "a repeat consent check reuses the consent credential")
	require.EqualValues(t, 1, metadataCalls.Load(), "another replica reuses verified metadata")
	_, preGrantOutcome := chainer.Acquire(ctx, preGrant)
	require.Equal(t, ReasonReauthenticationRequired, preGrantOutcome.Reason, "readiness never bypasses runtime grant validation")
	wrongConsent := preGrant
	wrongConsent.OrganizationID = "another-organization"
	require.False(t, chainer.CheckConsent(ctx, wrongConsent, signIn.ID.UUID).Succeeded())
	wrongOrg, err := signins.CheckConsentIdentity(ctx, f.db, "another-organization", f.req.UserID, signIn.ID.UUID, time.Now())
	require.NoError(t, err)
	require.False(t, wrongOrg.Eligible())
	wrongConsent = preGrant
	wrongConsent.UserID = "another-user"
	require.Equal(t, ReasonReauthenticationRequired, chainer.CheckConsent(ctx, wrongConsent, signIn.ID.UUID).Reason)
	require.Equal(t, ReasonReauthenticationRequired, chainer.CheckConsent(ctx, preGrant, uuid.Nil).Reason)
	require.Equal(t, RefreshReauthenticate, governor.RefreshDecision(ctx, f.req).Decision, "no grant carries the session id yet")
	_, err = usersessionsrepo.New(f.db).CreateUserSession(ctx, usersessionsrepo.CreateUserSessionParams{UserSessionIssuerID: f.req.UserSessionIssuerID, SubjectUrn: urn.NewUserSubject(f.req.UserID), Jti: f.req.UserSessionJTI, RefreshExpiresAt: conv.ToPGTimestamptz(time.Now().Add(time.Hour)), ExpiresAt: conv.ToPGTimestamptz(time.Now().Add(time.Hour)), SignInID: signIn.ID, SignInUserID: conv.ToPGText(f.req.UserID)})
	require.NoError(t, err)
	require.Equal(t, RefreshVerdict{Decision: RefreshServed, RemoteSessionIssuerID: remoteID, Provider: remotesessions.EMAProviderSpeakeasy, Reason: ReasonSuccess}, governor.RefreshDecision(ctx, f.req), "a live grant with an eligible sign-in chains without a new sign-in, judged from cached discovery")
	require.Equal(t, RefreshReauthenticate, governor.RefreshDecision(ctx, preGrant).Decision, "without the grant's session id the human must sign in again")
	token, outcome := chainer.Acquire(ctx, f.req)
	require.True(t, outcome.Succeeded(), "%+v", outcome)
	require.Equal(t, "live-downstream", token.Value())
	require.EqualValues(t, 1, calls.Load(), "the grant adopts its consent credential instead of signing again")
	require.True(t, chainer.HasUsableCredential(ctx, f.req))
	require.True(t, chainer.HasUsableCredential(ctx, preGrant), "status follows the stored grant without inventing current session authority")
	_, outcome = chainer.Acquire(ctx, f.req)
	require.True(t, outcome.Succeeded(), "%+v", outcome)
	require.EqualValues(t, 1, calls.Load(), "cached token is reused")
	// A second live grant for the same human. While the legacy one-slot
	// index exists, its publish replaces the first grant's slot.
	second := f.req
	second.UserSessionJTI = uuid.NewString()
	_, err = usersessionsrepo.New(f.db).CreateUserSession(ctx, usersessionsrepo.CreateUserSessionParams{UserSessionIssuerID: f.req.UserSessionIssuerID, SubjectUrn: urn.NewUserSubject(f.req.UserID), Jti: second.UserSessionJTI, RefreshExpiresAt: conv.ToPGTimestamptz(time.Now().Add(time.Hour)), ExpiresAt: conv.ToPGTimestamptz(time.Now().Add(time.Hour)), SignInID: signIn.ID, SignInUserID: conv.ToPGText(f.req.UserID)})
	require.NoError(t, err)
	_, outcome = chainer.Acquire(ctx, second)
	checkExchange()
	require.True(t, outcome.Succeeded(), "%+v", outcome)
	require.EqualValues(t, 2, calls.Load())
	require.False(t, chainer.HasUsableCredential(ctx, f.req), "the legacy index keeps one slot per subject")
	// Once the legacy index is dropped, each grant keeps its own slot.
	_, err = f.db.Exec(ctx, "DROP INDEX remote_session_ema_credentials_subject_key") //nolint:glint // notestingrawsql: DDL simulating the contract migration in this cloned database; sqlc does not generate DDL.
	require.NoError(t, err)
	_, outcome = chainer.Acquire(ctx, f.req)
	checkExchange()
	require.True(t, outcome.Succeeded(), "%+v", outcome)
	require.EqualValues(t, 3, calls.Load())
	require.True(t, chainer.HasUsableCredential(ctx, f.req))
	require.True(t, chainer.HasUsableCredential(ctx, second), "another grant's publish never evicts this one")
	for _, r := range []Request{f.req, second} {
		_, outcome = chainer.Acquire(ctx, r)
		require.True(t, outcome.Succeeded(), "%+v", outcome)
	}
	require.EqualValues(t, 3, calls.Load(), "each grant reuses its own cached token")
	wrongOrigin, err := url.Parse("https://other-deployment.example.test")
	require.NoError(t, err)
	for _, cfg := range []internalsessionissuers.SignerConfig{
		{Deployment: internalsessionissuers.Deployment{ServerURL: wrongOrigin, KeyRing: ring}, ManagedSigner: account},
		{Deployment: internalsessionissuers.Deployment{ServerURL: base, KeyRing: ring + "-other"}, ManagedSigner: account},
		{Deployment: internalsessionissuers.Deployment{ServerURL: base, KeyRing: ring}, ManagedSigner: "other@gram-stub.iam.gserviceaccount.com"},
	} {
		otherSigner := internalsessionissuers.NewSigner(logger, tp, mp, f.db, gate, gcpauth.NewIdentity(gcpauth.NewStubResolver()), func(context.Context, oauth2.TokenSource) (gcpkms.SigningClient, error) { return keys, nil }, cfg)
		gate.SetSigningAuthority(otherSigner.CheckAuthority)
		unavailableStatus, err := gate.Status(ctx, f.req.OrganizationID)
		require.NoError(t, err)
		require.Equal(t, internalsessionissuers.StateUnavailable, unavailableStatus.State, "management readiness rejects copied issuer authority")
		require.NotNil(t, unavailableStatus.Issuer, "issuer remains available for safe containment")
		gate.SetSigningAuthority(signer.CheckAuthority)
		chainer.SetSpeakeasyIssuer(gate, otherSigner, flags)
		require.False(t, chainer.HasUsableCredential(ctx, f.req), "copied credentials never confer authority on another deployment")
		_, outcome = chainer.Acquire(ctx, f.req)
		require.Equal(t, ReasonConfigurationRequired, outcome.Reason)
		governor.SetSpeakeasyIssuer(gate, otherSigner, flags)
		_, serves := governor.Serves(ctx, f.req)
		require.False(t, serves, "foreign deployment must not advertise issuer readiness")
		order, resolved, _ := governor.ProviderOrder(ctx, f.req)
		require.True(t, resolved)
		require.Equal(t, remotesessions.EMAProviderSigningUnavailable, order.Providers[0].Unavailable)
		require.EqualValues(t, 3, calls.Load())
	}
	chainer.SetSpeakeasyIssuer(gate, signer, flags)
	governor.SetSpeakeasyIssuer(gate, signer, flags)
	flags.SetFlag(feature.FlagInternalSessionIssuerSigningPaused, f.req.OrganizationID, true)
	_, serves := governor.Serves(ctx, f.req)
	require.False(t, serves, "a paused issuer must not advertise readiness")
	flags.SetFlag(feature.FlagInternalSessionIssuerSigningPaused, f.req.OrganizationID, false)
	flags.SetFlag(feature.FlagInternalSessionIssuer, f.req.OrganizationID, false)
	_, serves = governor.Serves(ctx, f.req)
	require.False(t, serves, "rollout removal must stop advertising readiness")
	flags.SetFlag(feature.FlagInternalSessionIssuer, f.req.OrganizationID, true)
	_, serves = governor.Serves(ctx, f.req)
	require.True(t, serves, "restored issuer readiness is read live")
	flags.SetFlag(feature.FlagSignInEnforcement, f.req.OrganizationID, false)
	_, serves = governor.Serves(ctx, f.req)
	require.False(t, serves, "Speakeasy needs sign-in enforcement")
	_, outcome = chainer.Acquire(ctx, f.req)
	require.Equal(t, ReasonProviderUnavailable, outcome.Reason, "cached tokens are not released without sign-in enforcement")
	require.Equal(t, remotesessions.EMAProviderSignInEnforcement, outcome.Unavailable)
	signins.EnableForTests(flags, f.req.OrganizationID)
	_, serves = governor.Serves(ctx, f.req)
	require.True(t, serves)
	var missingSigner *internalsessionissuers.Signer
	gate.SetSigningAuthority(missingSigner.CheckAuthority)
	missingStatus, err := gate.Status(ctx, f.req.OrganizationID)
	require.NoError(t, err)
	require.Equal(t, internalsessionissuers.StateUnavailable, missingStatus.State)
	require.NotNil(t, missingStatus.Issuer)
	gate.SetSigningAuthority(func(context.Context, string, internalsessionissuers.Status) error {
		return fmt.Errorf("authority lookup failed")
	})
	failedStatus, err := gate.Status(ctx, f.req.OrganizationID)
	require.ErrorContains(t, err, "authority lookup failed")
	require.Equal(t, internalsessionissuers.StateUnavailable, failedStatus.State)
	gate.SetSigningAuthority(signer.CheckAuthority)

	otherUser := "user-" + uuid.NewString()
	require.NoError(t, testrepo.New(f.db).InsertUserFixture(ctx, testrepo.InsertUserFixtureParams{ID: otherUser, Email: otherUser + "@example.test", DisplayName: "Other member"}))
	require.NoError(t, testrepo.New(f.db).CreateOrganizationUserRelationshipFixture(ctx, testrepo.CreateOrganizationUserRelationshipFixtureParams{OrganizationID: f.req.OrganizationID, UserID: conv.ToPGText(otherUser)}))
	wrong := f.req
	wrong.UserID = otherUser
	_, outcome = chainer.Acquire(ctx, wrong)
	require.Equal(t, ReasonReauthenticationRequired, outcome.Reason)
	require.Equal(t, RefreshReauthenticate, governor.RefreshDecision(ctx, wrong).Decision, "another member's request never borrows this grant")
	wrong = f.req
	wrong.ProjectID = uuid.New()
	require.False(t, chainer.HasUsableCredential(ctx, wrong))
	_, err = jwksrepo.New(f.db).SetInternalSessionIssuerDisabled(ctx, jwksrepo.SetInternalSessionIssuerDisabledParams{OrganizationID: f.req.OrganizationID, Disabled: true})
	require.NoError(t, err)
	require.False(t, chainer.HasUsableCredential(ctx, f.req))
	gate.SetSigningAuthority(missingSigner.CheckAuthority)
	disabledStatus, err := gate.Status(ctx, f.req.OrganizationID)
	require.NoError(t, err)
	require.Equal(t, internalsessionissuers.StateDisabled, disabledStatus.State, "containment remains visible without signing authority")
	require.NotNil(t, disabledStatus.Issuer)
	gate.SetSigningAuthority(signer.CheckAuthority)
	_, err = jwksrepo.New(f.db).SetInternalSessionIssuerDisabled(ctx, jwksrepo.SetInternalSessionIssuerDisabledParams{OrganizationID: f.req.OrganizationID, Disabled: false})
	require.NoError(t, err)
	require.False(t, chainer.HasUsableCredential(ctx, f.req), "re-enabling cannot resurrect the previous token")
	revokeDuringExchange.Store(true)
	_, outcome = chainer.Acquire(ctx, f.req)
	checkExchange()
	require.Equal(t, ReasonStaleConfiguration, outcome.Reason, "revocation during redemption discards the token")
	require.False(t, chainer.HasUsableCredential(ctx, f.req))
	require.False(t, chainer.HasUsableCredential(ctx, preGrant), "status cannot revive a revoked retained grant")
	require.EqualValues(t, 4, calls.Load())
	require.Equal(t, ReasonReauthenticationRequired, chainer.CheckConsent(ctx, preGrant, signIn.ID.UUID).Reason, "revoked consent is not eligible")
	require.EqualValues(t, 4, calls.Load(), "a revoked consent sign-in never redeems")
	require.Equal(t, RefreshReauthenticate, governor.RefreshDecision(ctx, f.req).Decision, "a revoked sign-in needs a fresh sign-in before refresh skips reconnect")

}
