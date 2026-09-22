package mcp_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/dev-idp/pkg/devidptest"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/issuerurl"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/oauthtest"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	assertioncore "github.com/speakeasy-api/gram/server/internal/usersessions/assertion"
	"github.com/speakeasy-api/gram/server/internal/usersessions/assertion/workload"
	"github.com/speakeasy-api/gram/server/internal/usersessions/jwks"
	"github.com/speakeasy-api/gram/server/internal/usersessions/replay"
)

// The external subject the issuer vouches for.
const liveWorkloadSubject = "repo:acme/payments-api:ref:refs/heads/main"

// liveWorkloadAudience is the authorization server the assertions address.
const liveWorkloadAudience = "https://gram.example.com/mcp/workload-live"

// liveWorkloadFixture is one organization with one project, a dev-idp serving
// as the workload issuer over HTTPS, and a verifier that fetches that issuer's
// key set. Issuer and admission rows are left to each test, since which tier
// holds them is what the tests vary.
type liveWorkloadFixture struct {
	conn           *pgxpool.Pool
	verifier       *workload.Verifier
	issuer         *devidptest.Instance
	organizationID string
	projectID      uuid.UUID

	// jwksURI is the key set the issuer's discovery document advertises,
	// recorded on issuer rows exactly as registration would store it.
	jwksURI string

	// connectionsAtSetup is the issuer's connection count after fixture setup,
	// which includes discovery. No-egress assertions compare against it.
	connectionsAtSetup int64
}

func newLiveWorkloadFixture(t *testing.T) liveWorkloadFixture {
	t.Helper()

	conn, err := infra.CloneTestDatabase(t, "workloadlive")
	require.NoError(t, err)

	issuer := devidptest.Launch(t, devidptest.LaunchOpts{EnableWorkOS: false, Key: nil, TLS: true})
	jwksURI := oauthtest.DiscoverWorkloadJWKSURI(t, issuer)

	client, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	logger := testenv.NewLogger(t)
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{}, guardian.WithTLSRootCAs(issuer.RootCAs()))
	require.NoError(t, err)

	store := ratelimit.NewRedisStore(client)
	keys, err := jwks.NewKeyResolver(
		jwks.NewResolver(policy, testenv.NewMeterProvider(t), logger),
		jwks.NewMemoryCache(),
		ratelimit.New(store, string(testenv.NewCacheSuffix(t, "workload-live-refresh")), ratelimit.PerMinute(1000)),
		ratelimit.New(store, string(testenv.NewCacheSuffix(t, "workload-live-fetch")), ratelimit.PerMinute(1000)),
		logger,
	)
	require.NoError(t, err)

	guard, err := replay.NewRedisGuard(client, string(testenv.NewCacheSuffix(t, "workload-live-replay")), assertioncore.ReplayHoldFor(mcp.WorkloadAssertionMaxLifetime))
	require.NoError(t, err)

	verifier, err := workload.NewVerifier(keys, guard)
	require.NoError(t, err)

	organizationID := newLiveWorkloadOrganization(t, conn)

	return liveWorkloadFixture{
		conn:               conn,
		verifier:           verifier,
		issuer:             issuer,
		organizationID:     organizationID,
		projectID:          newLiveWorkloadProject(t, conn, organizationID),
		jwksURI:            jwksURI,
		connectionsAtSetup: issuer.Connections(),
	}
}

func newLiveWorkloadOrganization(t *testing.T, conn *pgxpool.Pool) string {
	t.Helper()

	id := fmt.Sprintf("org-%s", uuid.NewString()[:8])
	_, err := orgrepo.New(conn).UpsertOrganizationMetadata(t.Context(), orgrepo.UpsertOrganizationMetadataParams{
		ID: id, Name: "Workload Org", Slug: id, WorkosID: pgtype.Text{}, Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)

	return id
}

// newLiveWorkloadProject adds a project to an existing organization. The
// workload tables' composite keys reject a project from another organization.
func newLiveWorkloadProject(t *testing.T, conn *pgxpool.Pool, organizationID string) uuid.UUID {
	t.Helper()

	project, err := projectsrepo.New(conn).CreateProject(t.Context(), projectsrepo.CreateProjectParams{
		Name: "Workload Project", Slug: fmt.Sprintf("workload-%s", uuid.NewString()[:8]), OrganizationID: organizationID,
	})
	require.NoError(t, err)

	return project.ID
}

// seedIssuer writes an issuer row with the given identifier and jwks_uri.
// It stores them as given, so tests can store values validation would refuse;
// the dev-idp's own are f.issuer.OAuth21URL and f.jwksURI.
func (f liveWorkloadFixture) seedIssuer(t *testing.T, organizationID string, projectID uuid.NullUUID, issuerURL, jwksURI string) uuid.UUID {
	t.Helper()

	id, err := testrepo.New(f.conn).CreateWorkloadIssuerFixture(t.Context(), testrepo.CreateWorkloadIssuerFixtureParams{
		OrganizationID: organizationID,
		ProjectID:      projectID,
		Name:           "live-issuer",
		Issuer:         issuerURL,
		JwksUri:        jwksURI,
	})
	require.NoError(t, err)

	return id
}

func (f liveWorkloadFixture) seedAdmission(t *testing.T, organizationID string, projectID uuid.NullUUID, issuerID uuid.UUID) {
	t.Helper()

	require.NoError(t, testrepo.New(f.conn).CreateWorkloadIdentityAdmissionFixture(t.Context(), testrepo.CreateWorkloadIdentityAdmissionFixtureParams{
		OrganizationID:   organizationID,
		ProjectID:        projectID,
		WorkloadIssuerID: issuerID,
		Subject:          liveWorkloadSubject,
	}))
}

func (f liveWorkloadFixture) softDeleteIssuer(t *testing.T, organizationID string, id uuid.UUID) {
	t.Helper()

	deleted, err := testrepo.New(f.conn).SoftDeleteWorkloadIssuerFixture(t.Context(), testrepo.SoftDeleteWorkloadIssuerFixtureParams{
		ID:             id,
		OrganizationID: organizationID,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), deleted)
}

// endpoint is an MCP server in the given tenancy. Its own authorization server
// id is fresh, so replay and fetch budgets never carry between endpoints.
func (f liveWorkloadFixture) endpoint(organizationID string, projectID uuid.UUID) *mcp.ResolvedMcpEndpoint {
	return &mcp.ResolvedMcpEndpoint{
		OrganizationID:      organizationID,
		ProjectID:           projectID,
		UserSessionIssuerID: uuid.New(),
	}
}

// present signs the given claims with the issuer's key and runs them through
// every admission stage. oauthtest.WorkloadClaims mints a fresh jti each call,
// so claims built from it are never refused as a replay.
func (f liveWorkloadFixture) present(t *testing.T, endpoint *mcp.ResolvedMcpEndpoint, claims jwt.Claims) error {
	t.Helper()

	raw := oauthtest.MintWorkloadAssertion(t, f.issuer, "JWT", claims)

	err := mcp.AdmitWorkloadAssertion(t.Context(), f.conn, f.verifier, endpoint, []string{
		liveWorkloadAudience,
		liveWorkloadAudience + "/token",
	}, raw)
	if err != nil {
		return fmt.Errorf("admit workload assertion: %w", err)
	}

	return nil
}

// An admitted workload passes every stage and reaches the issuer.
func TestWorkloadAssertionPipeline_AdmittedWorkloadFromALiveIssuerPasses(t *testing.T) {
	t.Parallel()

	f := newLiveWorkloadFixture(t)
	issuerID := f.seedIssuer(t, f.organizationID, uuid.NullUUID{}, f.issuer.OAuth21URL, f.jwksURI)
	f.seedAdmission(t, f.organizationID, uuid.NullUUID{UUID: f.projectID, Valid: true}, issuerID)

	err := f.present(t, f.endpoint(f.organizationID, f.projectID), oauthtest.WorkloadClaims(f.issuer, liveWorkloadSubject, liveWorkloadAudience))

	require.NoError(t, err)
	require.Greater(t, f.issuer.Connections(), f.connectionsAtSetup, "an admitted assertion must have fetched the key set, or the no-egress tests assert against a counter that never moves")
}

// An issuer registered by another organization is untrusted here, and the
// refusal makes no connection to it. That organization has registered and
// admitted the same issuer and subject, so only tenancy separates them.
func TestWorkloadAssertionPipeline_IssuerOutsideTheTenancyMakesNoOutboundRequest(t *testing.T) {
	t.Parallel()

	f := newLiveWorkloadFixture(t)
	other := newLiveWorkloadOrganization(t, f.conn)
	otherIssuerID := f.seedIssuer(t, other, uuid.NullUUID{}, f.issuer.OAuth21URL, f.jwksURI)
	f.seedAdmission(t, other, uuid.NullUUID{}, otherIssuerID)

	err := f.present(t, f.endpoint(f.organizationID, f.projectID), oauthtest.WorkloadClaims(f.issuer, liveWorkloadSubject, liveWorkloadAudience))

	require.ErrorIs(t, err, mcp.ErrWorkloadIssuerUntrusted)
	require.Equal(t, f.connectionsAtSetup, f.issuer.Connections(), "an untrusted issuer must be refused before any connection to it is attempted")
}

// A soft-deleted issuer stops resolving, so the assertion is refused at the
// issuer stage with no fetch, even though its admission row remains.
func TestWorkloadAssertionPipeline_SoftDeletedIssuerIsUntrusted(t *testing.T) {
	t.Parallel()

	f := newLiveWorkloadFixture(t)
	issuerID := f.seedIssuer(t, f.organizationID, uuid.NullUUID{}, f.issuer.OAuth21URL, f.jwksURI)
	f.seedAdmission(t, f.organizationID, uuid.NullUUID{}, issuerID)
	f.softDeleteIssuer(t, f.organizationID, issuerID)

	err := f.present(t, f.endpoint(f.organizationID, f.projectID), oauthtest.WorkloadClaims(f.issuer, liveWorkloadSubject, liveWorkloadAudience))

	require.ErrorIs(t, err, mcp.ErrWorkloadIssuerUntrusted)
	require.Equal(t, f.connectionsAtSetup, f.issuer.Connections(), "a withdrawn issuer must be refused before any connection to it is attempted")
}

// A subject admitted in one project is not admitted in a sibling project of
// the same organization. The sibling admitting its own subject is the control.
func TestWorkloadAssertionPipeline_ASiblingProjectsAdmissionDoesNotAdmit(t *testing.T) {
	t.Parallel()

	f := newLiveWorkloadFixture(t)
	sibling := newLiveWorkloadProject(t, f.conn, f.organizationID)
	issuerID := f.seedIssuer(t, f.organizationID, uuid.NullUUID{}, f.issuer.OAuth21URL, f.jwksURI)
	f.seedAdmission(t, f.organizationID, uuid.NullUUID{UUID: sibling, Valid: true}, issuerID)

	err := f.present(t, f.endpoint(f.organizationID, f.projectID), oauthtest.WorkloadClaims(f.issuer, liveWorkloadSubject, liveWorkloadAudience))
	require.ErrorIs(t, err, mcp.ErrWorkloadNotAdmitted)

	err = f.present(t, f.endpoint(f.organizationID, sibling), oauthtest.WorkloadClaims(f.issuer, liveWorkloadSubject, liveWorkloadAudience))
	require.NoError(t, err, "the admission must admit its own project, or the refusal above proves nothing")
}

// An organization-tier admission admits the workload in every project of the
// organization.
func TestWorkloadAssertionPipeline_OrganizationTierAdmissionAdmitsEveryProject(t *testing.T) {
	t.Parallel()

	f := newLiveWorkloadFixture(t)
	other := newLiveWorkloadProject(t, f.conn, f.organizationID)
	issuerID := f.seedIssuer(t, f.organizationID, uuid.NullUUID{}, f.issuer.OAuth21URL, f.jwksURI)
	f.seedAdmission(t, f.organizationID, uuid.NullUUID{}, issuerID)

	require.NoError(t, f.present(t, f.endpoint(f.organizationID, f.projectID), oauthtest.WorkloadClaims(f.issuer, liveWorkloadSubject, liveWorkloadAudience)))
	require.NoError(t, f.present(t, f.endpoint(f.organizationID, other), oauthtest.WorkloadClaims(f.issuer, liveWorkloadSubject, liveWorkloadAudience)))
}

// A row whose jwks_uri is plain http is refused at key source construction,
// by name and before any connection, even though the issuer itself is https
// and the workload is admitted.
func TestWorkloadAssertionPipeline_PlainHTTPJwksURIIsRefusedWithoutFetching(t *testing.T) {
	t.Parallel()

	f := newLiveWorkloadFixture(t)
	plainJWKSURI := "http://" + strings.TrimPrefix(f.jwksURI, "https://")
	require.True(t, strings.HasPrefix(f.jwksURI, "https://"), "the fixture's jwks_uri must be https for this downgrade to mean anything")
	issuerID := f.seedIssuer(t, f.organizationID, uuid.NullUUID{}, f.issuer.OAuth21URL, plainJWKSURI)
	f.seedAdmission(t, f.organizationID, uuid.NullUUID{}, issuerID)

	err := f.present(t, f.endpoint(f.organizationID, f.projectID), oauthtest.WorkloadClaims(f.issuer, liveWorkloadSubject, liveWorkloadAudience))

	require.ErrorIs(t, err, jwks.ErrURINotHTTPS)
	require.Equal(t, f.connectionsAtSetup, f.issuer.Connections(), "a plain-http jwks_uri must be refused before any connection is attempted")
}

// An assertion naming a plain-http issuer is untrusted even when a row for
// that exact http issuer exists and admits the subject.
func TestWorkloadAssertionPipeline_PlainHTTPIssuerIsUntrusted(t *testing.T) {
	t.Parallel()

	f := newLiveWorkloadFixture(t)
	plainIssuer := "http://" + strings.TrimPrefix(f.issuer.OAuth21URL, "https://")
	require.True(t, strings.HasPrefix(f.issuer.OAuth21URL, "https://"), "the fixture's issuer must be https for this downgrade to mean anything")
	issuerID := f.seedIssuer(t, f.organizationID, uuid.NullUUID{}, plainIssuer, f.jwksURI)
	f.seedAdmission(t, f.organizationID, uuid.NullUUID{}, issuerID)

	claims := oauthtest.WorkloadClaims(f.issuer, liveWorkloadSubject, liveWorkloadAudience)
	claims.Issuer = plainIssuer
	err := f.present(t, f.endpoint(f.organizationID, f.projectID), claims)

	require.ErrorIs(t, err, mcp.ErrWorkloadIssuerUntrusted)
	require.ErrorIs(t, err, issuerurl.ErrNotHTTPS)
	require.Equal(t, f.connectionsAtSetup, f.issuer.Connections(), "a plain-http issuer must be refused before any connection is attempted")
}
