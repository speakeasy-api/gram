package identitychaining

import (
	"bytes"
	"net/url"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/dev-idp/pkg/devidptest"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

const (
	// devIDPClientID is shared by Gram's trusted client at the dev-idp and its
	// client at the resource authorization server: the dev-idp names the
	// requesting app as the ID-JAG's client_id, which the resource
	// authorization server then requires the redeeming client to present.
	devIDPClientID = "gram-identity-chaining"

	devIDPResourceSlug = "docs"
	devIDPResource     = "https://mcp.docs.example/"
)

// devIDPFixture is a Gram tenant whose trusted identity provider and resource
// authorization server are both one real dev-idp instance.
type devIDPFixture struct {
	idp     *devidptest.Instance
	chainer *Chainer
	req     Request
}

// newDevIDPFixture wires a human with a retained dev-idp ID token, a ready
// binding for the dev-idp resource, and a chainer over the real egress, key
// resolution and delegation service. assigned controls whether the dev-idp
// lets Gram's app act for the human at the resource.
func newDevIDPFixture(t *testing.T, assigned bool) devIDPFixture {
	t.Helper()
	ctx := t.Context()
	idp := devidptest.Launch(t, devidptest.LaunchOpts{TLS: true})
	app := devidptest.CreateEmaApp(t, ctx, idp.Repo, devidptest.EmaAppOpts{ClientID: devIDPClientID, ClientSecret: "idp-secret"})
	resource := devidptest.CreateEmaResource(t, ctx, idp.Repo, devidptest.EmaResourceOpts{Slug: devIDPResourceSlug, ResourceIdentifier: devIDPResource})
	devidptest.TrustEmaIssuer(t, ctx, idp.Repo, resource.ID, idp.OAuth21URL, devidptest.EmaTrustRuleOpts{AllowedScopes: "read"})
	if assigned {
		devidptest.AssignEmaApp(t, ctx, idp.Repo, app, idp.DefaultUser.ID, resource.ID, "read")
	}

	db, err := infra.CloneTestDatabase(t, "identity_chaining_devidp")
	require.NoError(t, err)
	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	enc, err := encryption.NewWithBytes(bytes.Repeat([]byte{0x42}, 32))
	require.NoError(t, err)
	fixtures := testrepo.New(db)
	q := repo.New(db)

	org := "org-" + uuid.NewString()
	human := "user-" + uuid.NewString()
	require.NoError(t, fixtures.SeedDelegationLoaderOrganizationFixture(ctx, testrepo.SeedDelegationLoaderOrganizationFixtureParams{OrganizationID: org, Name: "Chaining organization", Slug: "chaining-" + uuid.NewString()[:8]}))
	project, err := fixtures.CreateProjectFixture(ctx, testrepo.CreateProjectFixtureParams{ID: uuid.New(), Name: "Chaining project", Slug: "chaining-project", OrganizationID: org})
	require.NoError(t, err)
	require.NoError(t, fixtures.InsertUserFixture(ctx, testrepo.InsertUserFixtureParams{ID: human, Email: human + "@example.test", DisplayName: "Chaining user"}))
	require.NoError(t, fixtures.CreateOrganizationUserRelationshipFixture(ctx, testrepo.CreateOrganizationUserRelationshipFixtureParams{OrganizationID: org, UserID: pgtype.Text{String: human, Valid: true}}))

	// Gram's trusted registration at the dev-idp, as an administrator records it.
	idpIssuer, err := q.CreateRemoteSessionIssuer(ctx, repo.CreateRemoteSessionIssuerParams{
		OrganizationID: pgtype.Text{String: org, Valid: true}, Slug: "dev-idp", Issuer: idp.OAuth21URL,
		AuthorizationEndpoint: pgtype.Text{String: idp.OAuth21URL + "/authorize", Valid: true},
		TokenEndpoint:         pgtype.Text{String: idp.OAuth21URL + "/token", Valid: true},
		JwksUri:               pgtype.Text{String: idp.OAuth21URL + "/.well-known/jwks.json", Valid: true},
		ScopesSupported:       []string{}, GrantTypesSupported: []string{}, AuthorizationGrantProfilesSupported: []string{}, ResponseTypesSupported: []string{},
		TokenEndpointAuthMethodsSupported: []string{oauthwire.AuthMethodClientSecretPost}, CodeChallengeMethodsSupported: []string{},
		IntrospectionEndpointAuthMethodsSupported: []string{}, IDTokenSigningAlgValuesSupported: []string{}, ClaimsSupported: []string{},
	})
	require.NoError(t, err)
	idpSecret, err := enc.Encrypt([]byte("idp-secret"))
	require.NoError(t, err)
	idpClient, err := q.CreateRemoteSessionClient(ctx, repo.CreateRemoteSessionClientParams{
		OrganizationID: pgtype.Text{String: org, Valid: true}, RemoteSessionIssuerID: idpIssuer.ID, ClientID: devIDPClientID,
		ClientSecretEncrypted:   pgtype.Text{String: idpSecret, Valid: true},
		TokenEndpointAuthMethod: pgtype.Text{String: oauthwire.AuthMethodClientSecretPost, Valid: true},
		Scope:                   []string{"openid", "email"},
	})
	require.NoError(t, err)
	usi, err := usersessionsrepo.New(db).CreateOrganizationUserSessionIssuer(ctx, usersessionsrepo.CreateOrganizationUserSessionIssuerParams{
		OrganizationID: pgtype.Text{String: org, Valid: true}, Slug: "chaining-usi", AuthnChallengeMode: "interactive",
		SessionDuration:              pgtype.Interval{Microseconds: time.Hour.Microseconds(), Days: 0, Months: 0, Valid: true},
		TrustedRemoteSessionIssuerID: uuid.NullUUID{UUID: idpIssuer.ID, Valid: true}, TrustedRemoteSessionClientID: uuid.NullUUID{UUID: idpClient.ID, Valid: true},
	})
	require.NoError(t, err)

	// The resource authorization server registration and its ready binding.
	resourceAS := idp.ResourceASURL(devIDPResourceSlug)
	resourceIssuer, resourceClient := prepareResourceClient(t, db, enc, org, resourceAS, resourceAS+"/token", devIDPClientID, "unused-secret")
	req := Request{OrganizationID: org, ProjectID: project, UserSessionIssuerID: usi.ID, UserID: human, UpstreamResource: "https://mcp.docs.example"}
	bindResource(t, db, req, resourceIssuer, resourceClient, devIDPResource, remotesessions.PreparationStateReady)

	// The human's delegation, as a verified federated login would retain it.
	trusted, err := q.GetTrustedRemoteSessionClientForOrganization(ctx, repo.GetTrustedRemoteSessionClientForOrganizationParams{OrganizationID: org, IssuerID: idpIssuer.ID, ClientID: idpClient.ID})
	require.NoError(t, err)
	upstreamSubject := idp.DefaultUser.ID.String()
	idToken := mintDevIDPIDToken(t, idp, upstreamSubject)
	encryptedToken, err := enc.Encrypt([]byte(idToken))
	require.NoError(t, err)
	encryptedSubject, err := enc.Encrypt([]byte(upstreamSubject))
	require.NoError(t, err)
	_, err = q.UpsertTrustedDelegationCredential(ctx, repo.UpsertTrustedDelegationCredentialParams{
		OrganizationID: org, ClientID: idpClient.ID, IssuerID: idpIssuer.ID, SubjectUrn: urn.NewUserSubject(human).String(), ExpectedGeneration: 0,
		IdentityAssertionEncrypted: pgtype.Text{String: encryptedToken, Valid: true},
		IdentityAssertionExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true, InfinityModifier: pgtype.Finite},
		UpstreamSubjectEncrypted:   pgtype.Text{String: encryptedSubject, Valid: true},
		CredentialConfigHash:       pgtype.Text{String: remotesessions.FederatedDelegationConfigurationHash(org, trusted.RemoteSessionIssuer, trusted.RemoteSessionClient), Valid: true},
		ObservationStatus:          pgtype.Text{String: "assertion_only", Valid: true},
	})
	require.NoError(t, err)

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	meterProvider := testenv.NewMeterProvider(t)
	policy, err := guardian.NewUnsafePolicy(tracerProvider, []string{}, guardian.WithTLSRootCAs(idp.RootCAs()))
	require.NoError(t, err)
	locks := cache.NewRedisCacheAdapter(redisClient)
	keys, err := remotesessions.NewIDTokenKeyResolver(logger, policy, meterProvider, ratelimit.NewRedisStore(redisClient))
	require.NoError(t, err)
	serverURL, err := url.Parse("https://gram.example.test")
	require.NoError(t, err)
	challenges := remotesessions.NewChallengeManager(logger, tracerProvider, meterProvider, db, enc, policy, nil, locks, serverURL, remotesessions.WithIDTokenVerifier(remotesessions.NewIDTokenVerifier(keys)))
	return devIDPFixture{
		idp:     idp,
		chainer: New(logger, db, enc, challenges, remotesessions.NewDelegationService(db, enc, challenges), keys, locks),
		req:     req,
	}
}

// mintDevIDPIDToken signs an ID token as the dev-idp would issue at login.
func mintDevIDPIDToken(t *testing.T, idp *devidptest.Instance, subject string) string {
	t.Helper()
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: idp.SigningKey()}, (&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", idp.KeyID()))
	require.NoError(t, err)
	now := time.Now()
	raw, err := jwt.Signed(signer).Claims(map[string]any{
		"iss": idp.OAuth21URL, "sub": subject, "aud": devIDPClientID,
		"iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
	}).Serialize()
	require.NoError(t, err)
	return raw
}

func TestDevIDP_AcquiresAndReusesDownstreamToken(t *testing.T) {
	t.Parallel()
	f := newDevIDPFixture(t, true)

	token, outcome := f.chainer.Acquire(t.Context(), f.req)
	require.True(t, outcome.Succeeded(), "outcome: %+v", outcome)
	require.NotEmpty(t, token.Value())
	require.WithinDuration(t, time.Now().Add(time.Hour-accessExpirySkew), token.ExpiresAt(), time.Minute)

	// The downstream token is the resource authorization server's own access
	// token for the human, audience-bound to the resource.
	var claims struct {
		jwt.Claims

		ClientID string `json:"client_id"`
		Scope    string `json:"scope"`
	}
	parsed, err := jwt.ParseSigned(token.Value(), []jose.SignatureAlgorithm{jose.RS256})
	require.NoError(t, err)
	require.NoError(t, parsed.UnsafeClaimsWithoutVerification(&claims))
	require.Equal(t, f.idp.ResourceASURL(devIDPResourceSlug), claims.Issuer)
	require.Equal(t, f.idp.DefaultUser.ID.String(), claims.Subject)
	require.Equal(t, devIDPClientID, claims.ClientID)
	require.Equal(t, "read", claims.Scope)

	requests := f.idp.Requests()
	again, outcome := f.chainer.Acquire(t.Context(), f.req)
	require.True(t, outcome.Succeeded(), "outcome: %+v", outcome)
	require.Equal(t, token.Value(), again.Value(), "a live credential is reused")
	require.Equal(t, requests, f.idp.Requests(), "reuse contacts neither the identity provider nor the resource authorization server")
}

func TestDevIDP_UnassignedHumanIsDeniedAndCached(t *testing.T) {
	t.Parallel()
	f := newDevIDPFixture(t, false)

	_, outcome := f.chainer.Acquire(t.Context(), f.req)
	require.Equal(t, Outcome{Stage: StageExchange, Reason: ReasonAccessDenied, Confidence: ConfidenceInferred, Retryable: false, Cached: false}, outcome)

	requests := f.idp.Requests()
	_, outcome = f.chainer.Acquire(t.Context(), f.req)
	require.True(t, outcome.Cached, "a provider rejection is not repeated immediately")
	require.Equal(t, ReasonAccessDenied, outcome.Reason)
	require.Equal(t, requests, f.idp.Requests())
}
