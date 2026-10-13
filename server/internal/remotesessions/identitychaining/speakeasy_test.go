package identitychaining

import (
	"context"
	"encoding/json"
	"fmt"
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
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/productfeatures/productfeaturestest"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/ssosignins"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/gcp/gcpauth"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/gcp/gcpkms"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func TestSpeakeasy_SignedExchangeAndRevocation(t *testing.T) {
	t.Parallel()
	f := newChainStoreFixture(t)
	ctx := t.Context()
	logger := testenv.NewLogger(t)
	tp := testenv.NewTracerProvider(t)
	mp := testenv.NewMeterProvider(t)
	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	features := productfeatures.NewClient(logger, tp, f.db, redisClient)
	productfeaturestest.Enable(t, ctx, f.db, features, f.req.OrganizationID, productfeatures.FeatureSSO)
	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagInternalSessionIssuer, f.req.OrganizationID, true)
	flags.SetFlag(feature.FlagInternalSessionIssuerSigningPaused, f.req.OrganizationID, false)
	gate := internalsessionissuers.NewGate(f.db, features, flags, internalsessionissuers.GateConfig{Provisioning: true})
	const origin = "https://issuer.example.test"
	const ring = "projects/gram-stub/locations/global/keyRings/ema-test"
	const resource = ring + "/cryptoKeys/test/cryptoKeyVersions/1"
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
	require.NoError(t, orgrepo.New(f.db).SetVerifiedDomains(ctx, orgrepo.SetVerifiedDomainsParams{ID: f.req.OrganizationID, VerifiedDomains: []string{"example.test"}}))
	signIn, err := ssosignins.Record(ctx, f.db, ssosignins.RecordParams{OrganizationID: f.req.OrganizationID, UserID: f.req.UserID, WorkOSSessionID: uuid.NewString(), AuthenticationMethod: ssosignins.MethodSSO, AuthenticatedAt: time.Now().Add(-time.Minute)})
	require.NoError(t, err)
	f.req.UserSessionJTI = uuid.NewString()

	var calls atomic.Int32
	var revokeDuringExchange atomic.Bool
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
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		fail := func(err error) { exchangeErrors <- err; http.Error(w, "invalid test exchange", http.StatusBadRequest) }
		if revokeDuringExchange.Load() {
			if _, err := ssosignins.RevokeUser(ctx, f.db, []string{f.req.OrganizationID}, f.req.UserID, ssosignins.RevokeReasonAdmin); err != nil {
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
		if claims.Email != f.req.UserID+"@example.test" || claims.ClientID != testResourceClient || claims.Resource != testResource || claims.Scope != "read" {
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
	defer endpoint.Close()
	expectedAudience = endpoint.URL
	// Replace the fixture binding's resource client with a live HTTP endpoint.
	remoteID, clientID := prepareResourceClient(t, f.db, f.chainer.enc, f.req.OrganizationID, endpoint.URL, endpoint.URL, testResourceClient, "resource-secret")
	bindResource(t, f.db, f.req, remoteID, clientID, testResource, remotesessions.PreparationStateReady)
	// The original binding names the same resource; retire it to keep selection unique.
	f.setBinding(t, remotesessions.PreparationStateUnlinked)
	require.NoError(t, repo.New(f.db).InsertEMAProvider(ctx, repo.InsertEMAProviderParams{OrganizationID: f.req.OrganizationID, VendorIssuer: endpoint.URL, InternalSessionIssuerID: uuid.NullUUID{UUID: issuerID, Valid: true}}))
	policy, err := guardian.NewUnsafePolicy(tp, nil)
	require.NoError(t, err)
	challenges := remotesessions.NewChallengeManager(logger, tp, mp, f.db, f.chainer.enc, policy, nil, nil, base)
	chainer := New(logger, f.db, f.chainer.enc, challenges, nil, nil, cache.NewRedisCacheAdapter(redisClient))
	chainer.SetSpeakeasyIssuer(gate, signer)
	preGrant := f.req
	preGrant.UserSessionJTI = ""
	require.Equal(t, ReasonConsentRequired, chainer.CheckConsent(ctx, preGrant, signIn.ID.UUID).Reason)
	require.EqualValues(t, 0, calls.Load(), "pre-grant eligibility never redeems a token")
	_, preGrantOutcome := chainer.Acquire(ctx, preGrant)
	require.Equal(t, ReasonReauthenticationRequired, preGrantOutcome.Reason, "readiness never bypasses runtime grant validation")
	wrongConsent := preGrant
	wrongConsent.OrganizationID = "another-organization"
	require.NotEqual(t, ReasonConsentRequired, chainer.CheckConsent(ctx, wrongConsent, signIn.ID.UUID).Reason)
	wrongOrg, err := ssosignins.CheckConsentIdentity(ctx, f.db, "another-organization", f.req.UserID, signIn.ID.UUID, time.Now())
	require.NoError(t, err)
	require.False(t, wrongOrg.Eligible())
	wrongConsent = preGrant
	wrongConsent.UserID = "another-user"
	require.Equal(t, ReasonReauthenticationRequired, chainer.CheckConsent(ctx, wrongConsent, signIn.ID.UUID).Reason)
	require.Equal(t, ReasonReauthenticationRequired, chainer.CheckConsent(ctx, preGrant, uuid.Nil).Reason)
	_, err = usersessionsrepo.New(f.db).CreateUserSession(ctx, usersessionsrepo.CreateUserSessionParams{UserSessionIssuerID: f.req.UserSessionIssuerID, SubjectUrn: urn.NewUserSubject(f.req.UserID), Jti: f.req.UserSessionJTI, RefreshExpiresAt: conv.ToPGTimestamptz(time.Now().Add(time.Hour)), ExpiresAt: conv.ToPGTimestamptz(time.Now().Add(time.Hour)), SsoSignInID: signIn.ID, SsoSignInUserID: conv.ToPGText(f.req.UserID)})
	require.NoError(t, err)
	token, outcome := chainer.Acquire(ctx, f.req)
	checkExchange()
	require.True(t, outcome.Succeeded(), "%+v", outcome)
	require.Equal(t, "live-downstream", token.Value())
	require.EqualValues(t, 1, calls.Load())
	require.True(t, chainer.HasUsableCredential(ctx, f.req))
	_, outcome = chainer.Acquire(ctx, f.req)
	require.True(t, outcome.Succeeded(), "%+v", outcome)
	require.EqualValues(t, 1, calls.Load(), "cached token is reused")
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
		chainer.SetSpeakeasyIssuer(gate, otherSigner)
		require.False(t, chainer.HasUsableCredential(ctx, f.req), "copied credentials never confer authority on another deployment")
		_, outcome = chainer.Acquire(ctx, f.req)
		require.Equal(t, ReasonConfigurationRequired, outcome.Reason)
		require.EqualValues(t, 1, calls.Load())
	}
	chainer.SetSpeakeasyIssuer(gate, signer)
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
	require.EqualValues(t, 2, calls.Load())
	require.Equal(t, ReasonReauthenticationRequired, chainer.CheckConsent(ctx, preGrant, signIn.ID.UUID).Reason, "revoked consent is not eligible")

}
