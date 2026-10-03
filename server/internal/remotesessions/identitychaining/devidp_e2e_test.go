package identitychaining

import (
	"bytes"
	"context"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/dev-idp/pkg/devidptest"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	identityproviderconnectionsrepo "github.com/speakeasy-api/gram/server/internal/identityproviderconnections/repo"
	"github.com/speakeasy-api/gram/server/internal/mcp/tunnelrouting"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	oktaresourceconnectionsrepo "github.com/speakeasy-api/gram/server/internal/oktaresourceconnections/repo"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
	"github.com/speakeasy-api/gram/tunnel/route"
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
	observed    *recordingObserver
	idp         *devidptest.Instance
	chainer     *Chainer
	req         Request
	db          *pgxpool.Pool
	enc         *encryption.Client
	idpClientID uuid.UUID
}

// recordingObserver keeps every observation the chainer reports.
type recordingObserver struct {
	mu           sync.Mutex
	observations []Observation
}

func (r *recordingObserver) ObserveAttempt(_ context.Context, o Observation) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.observations = append(r.observations, o)
	return nil
}

func (r *recordingObserver) all() []Observation {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.observations)
}

// devIDPOptions varies the dev-idp tenant a fixture builds.
type devIDPOptions struct {
	// unassigned withholds the dev-idp assignment that lets Gram's app act for
	// the human at the resource.
	unassigned bool

	// remoteIssuer, when set, is the issuer Gram records for the downstream
	// authorization server instead of the dev-idp resource server's own, as
	// with Linear, whose MCP authorization server and Okta resource app differ.
	remoteIssuer string

	// confirmAudience records an administrator-confirmed Okta resource
	// connection naming the dev-idp resource server's issuer as audience.
	confirmAudience bool
}

// newDevIDPFixture wires a human with a retained dev-idp ID token, a ready
// binding for the dev-idp resource, and a chainer over the real egress, key
// resolution and delegation service.
func newDevIDPFixture(t *testing.T, opts devIDPOptions) devIDPFixture {
	t.Helper()
	ctx := t.Context()
	idp := devidptest.Launch(t, devidptest.LaunchOpts{TLS: true})
	app := devidptest.CreateEmaApp(t, ctx, idp.Repo, devidptest.EmaAppOpts{ClientID: devIDPClientID, ClientSecret: "idp-secret"})
	resource := devidptest.CreateEmaResource(t, ctx, idp.Repo, devidptest.EmaResourceOpts{Slug: devIDPResourceSlug, ResourceIdentifier: devIDPResource})
	devidptest.TrustEmaIssuer(t, ctx, idp.Repo, resource.ID, idp.OAuth21URL, devidptest.EmaTrustRuleOpts{AllowedScopes: "read"})
	if !opts.unassigned {
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
	remoteIssuer := resourceAS
	if opts.remoteIssuer != "" {
		remoteIssuer = opts.remoteIssuer
	}
	resourceIssuer, resourceClient := prepareResourceClient(t, db, enc, org, remoteIssuer, resourceAS+"/token", devIDPClientID, "unused-secret")
	req := Request{OrganizationID: org, ProjectID: project, UserSessionIssuerID: usi.ID, UserID: human, UpstreamResource: "https://mcp.docs.example"}
	bindResource(t, db, req, resourceIssuer, resourceClient, devIDPResource, remotesessions.PreparationStateReady)

	if opts.confirmAudience {
		connection, err := identityproviderconnectionsrepo.New(db).CreateIdentityProviderConnection(ctx, identityproviderconnectionsrepo.CreateIdentityProviderConnectionParams{OrganizationID: org, Provider: "okta"})
		require.NoError(t, err)
		_, err = identityproviderconnectionsrepo.New(db).CreateOktaIdentityProviderConnection(ctx, identityproviderconnectionsrepo.CreateOktaIdentityProviderConnectionParams{
			IdentityProviderConnectionID: connection.ID, OrganizationID: org, OrgUrl: idp.Issuer, IssuerUrl: idp.OAuth21URL,
			RemoteSessionIssuerID: idpIssuer.ID, RemoteSessionClientID: idpClient.ID, ListingMode: "custom_app",
		})
		require.NoError(t, err)
		_, err = oktaresourceconnectionsrepo.New(db).UpsertResourceConnection(ctx, oktaresourceconnectionsrepo.UpsertResourceConnectionParams{
			OrganizationID: org, IdentityProviderConnectionID: connection.ID, RemoteSessionIssuerID: resourceIssuer,
			Resource: strings.TrimRight(devIDPResource, "/"), Audience: resourceAS, OktaApplicationID: pgtype.Text{},
		})
		require.NoError(t, err)
	}

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
	keys := remotesessions.NewIDTokenKeyResolver(logger, policy, meterProvider, ratelimit.NewRedisStore(redisClient))
	serverURL, err := url.Parse("https://gram.example.test")
	require.NoError(t, err)
	challenges := remotesessions.NewChallengeManager(logger, tracerProvider, meterProvider, db, enc, policy, tunnelrouting.NewHTTPClient(route.NewRouteTable(), "forward-token", policy, nil), locks, serverURL, remotesessions.WithIDTokenVerifier(remotesessions.NewIDTokenVerifier(keys)))
	chainer := New(logger, db, enc, challenges, remotesessions.NewDelegationService(db, enc, challenges), keys, locks)
	observed := &recordingObserver{}
	chainer.SetObserver(observed)
	return devIDPFixture{
		observed:    observed,
		idp:         idp,
		chainer:     chainer,
		req:         req,
		db:          db,
		enc:         enc,
		idpClientID: idpClient.ID,
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
	f := newDevIDPFixture(t, devIDPOptions{})

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

	observed := f.observed.all()
	require.Len(t, observed, 1)
	require.True(t, observed[0].Outcome.Succeeded())
	require.True(t, observed[0].GrantValidated)
	require.Equal(t, f.req.OrganizationID, observed[0].OrganizationID)
	require.Equal(t, devIDPResource, observed[0].Resource)
	require.Equal(t, f.idp.ResourceASURL(devIDPResourceSlug), observed[0].RemoteIssuer)
	require.Equal(t, f.idp.ResourceASURL(devIDPResourceSlug), observed[0].Audience)
	require.NotEqual(t, uuid.Nil, observed[0].TrustedIssuerID)
	require.NotEqual(t, uuid.Nil, observed[0].RemoteIssuerID)

	requests := f.idp.Requests()
	again, outcome := f.chainer.Acquire(t.Context(), f.req)
	require.True(t, outcome.Succeeded(), "outcome: %+v", outcome)
	require.Equal(t, token.Value(), again.Value(), "a live credential is reused")
	require.Equal(t, requests, f.idp.Requests(), "reuse contacts neither the identity provider nor the resource authorization server")
	require.Len(t, f.observed.all(), 1, "a reused credential is not an attempt")
}

func TestDevIDP_UnassignedHumanIsDeniedAndCached(t *testing.T) {
	t.Parallel()
	f := newDevIDPFixture(t, devIDPOptions{unassigned: true})

	_, outcome := f.chainer.Acquire(t.Context(), f.req)
	require.Equal(t, Outcome{Stage: StageExchange, Reason: ReasonAccessDenied, Confidence: ConfidenceInferred, Retryable: false, Cached: false}, outcome)
	observed := f.observed.all()
	require.Len(t, observed, 1)
	require.Equal(t, outcome, observed[0].Outcome)
	require.False(t, observed[0].GrantValidated)

	requests := f.idp.Requests()
	_, outcome = f.chainer.Acquire(t.Context(), f.req)
	require.True(t, outcome.Cached, "a provider rejection is not repeated immediately")
	require.Equal(t, ReasonAccessDenied, outcome.Reason)
	require.Equal(t, requests, f.idp.Requests())
	require.Len(t, f.observed.all(), 1, "a cached failure is not an attempt")
}

func TestDevIDP_RegistrationChangeRetiresStoredToken(t *testing.T) {
	t.Parallel()
	f := newDevIDPFixture(t, devIDPOptions{})
	_, outcome := f.chainer.Acquire(t.Context(), f.req)
	require.True(t, outcome.Succeeded(), "outcome: %+v", outcome)

	rotated, err := f.enc.Encrypt([]byte("rotated-secret"))
	require.NoError(t, err)
	_, err = repo.New(f.db).UpdateOrganizationRemoteSessionClient(t.Context(), repo.UpdateOrganizationRemoteSessionClientParams{
		ClientSecretEncrypted: pgtype.Text{String: rotated, Valid: true}, ID: f.idpClientID, OrganizationID: pgtype.Text{String: f.req.OrganizationID, Valid: true},
	})
	require.NoError(t, err)

	requests := f.idp.Requests()
	_, outcome = f.chainer.Acquire(t.Context(), f.req)
	require.Equal(t, Outcome{Stage: StageDelegation, Reason: ReasonConfigurationRequired, Confidence: ConfidenceVerified, Retryable: false, Cached: false}, outcome,
		"a token acquired under the previous trusted registration is never released")
	require.Equal(t, requests, f.idp.Requests())
}

// Okta mints ID-JAGs only for a resource app's Issuer URL, which can differ
// from the downstream authorization server's issuer (Linear's resource app is
// auth.linear.com, its MCP authorization server mcp.linear.app).
func TestDevIDP_ConfirmedAudienceDiffersFromRemoteIssuer(t *testing.T) {
	t.Parallel()
	const remoteIssuer = "https://mcp.docs.example"

	unconfirmed := newDevIDPFixture(t, devIDPOptions{remoteIssuer: remoteIssuer})
	_, outcome := unconfirmed.chainer.Acquire(t.Context(), unconfirmed.req)
	require.Equal(t, StageExchange, outcome.Stage)
	require.False(t, outcome.Succeeded(), "without a confirmed audience the identity provider refuses the remote issuer as audience")

	confirmed := newDevIDPFixture(t, devIDPOptions{remoteIssuer: remoteIssuer, confirmAudience: true})
	token, outcome := confirmed.chainer.Acquire(t.Context(), confirmed.req)
	require.True(t, outcome.Succeeded(), "outcome: %+v", outcome)
	require.NotEmpty(t, token.Value())
	observed := confirmed.observed.all()
	require.Len(t, observed, 1)
	require.True(t, observed[0].GrantValidated)
	require.True(t, observed[0].Outcome.Succeeded())
	require.Equal(t, remoteIssuer, observed[0].RemoteIssuer)
	require.Equal(t, confirmed.idp.ResourceASURL(devIDPResourceSlug), observed[0].Audience)
	require.NotEqual(t, observed[0].RemoteIssuer, observed[0].Audience)

	failed := unconfirmed.observed.all()
	require.Len(t, failed, 1)
	require.False(t, failed[0].GrantValidated)
	require.Equal(t, remoteIssuer, failed[0].RemoteIssuer)
	require.Equal(t, remoteIssuer, failed[0].Audience)
}
