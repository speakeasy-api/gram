package mcp_test

import (
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/dev-idp/pkg/devidptest"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/oauthtest"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/usersessions/clientauth"
	"github.com/speakeasy-api/gram/server/internal/usersessions/jwks"
	"github.com/speakeasy-api/gram/server/internal/usersessions/replay"
)

// The subject a platform vouches for: a declared, bounded resource rather than
// the ephemeral job.
const liveWorkloadSubject = "repo:acme/payments-api:ref:refs/heads/main"

// liveWorkloadAudience is the authorization server the assertions address.
const liveWorkloadAudience = "https://gram.example.com/mcp/workload-live"

// liveWorkloadFixture is one organization with one project, a dev-idp serving
// as the workload issuer over HTTPS, and a verifier that fetches that issuer's
// key set. Issuer and admission rows are left to each test, since which tier
// holds them is what the tests vary.
type liveWorkloadFixture struct {
	conn           *pgxpool.Pool
	verifier       *clientauth.Verifier
	issuer         *devidptest.Instance
	organizationID string
	projectID      uuid.UUID

	// jwksURI is the key set the issuer's discovery document advertises,
	// recorded on issuer rows exactly as registration would store it.
	jwksURI string

	// connectionsAtSetup is how many connections the issuer had accepted once
	// the fixture was built. Discovery made one, so a test asserting that a
	// rejection never reached the issuer compares against this, not zero. It
	// counts connections so that an attempt failing before it sends a request
	// still registers.
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

	guard, err := replay.NewRedisGuard(client, string(testenv.NewCacheSuffix(t, "workload-live-replay")), clientauth.DefaultMaxReplayHold)
	require.NoError(t, err)

	verifier, err := clientauth.NewVerifier(keys, guard)
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

// newLiveWorkloadProject adds a project to an organization that already
// exists. A sibling has to be built this way: both workload tables pin
// project_id to the row's own organization through a composite key, so a
// project from another organization cannot be written at all.
func newLiveWorkloadProject(t *testing.T, conn *pgxpool.Pool, organizationID string) uuid.UUID {
	t.Helper()

	project, err := projectsrepo.New(conn).CreateProject(t.Context(), projectsrepo.CreateProjectParams{
		Name: "Workload Project", Slug: fmt.Sprintf("workload-%s", uuid.NewString()[:8]), OrganizationID: organizationID,
	})
	require.NoError(t, err)

	return project.ID
}

// seedIssuer registers the dev-idp in a tenancy, with the issuer identifier
// and jwks_uri its discovery document advertises.
func (f liveWorkloadFixture) seedIssuer(t *testing.T, organizationID string, projectID uuid.NullUUID) uuid.UUID {
	t.Helper()

	var id uuid.UUID
	err := f.conn.QueryRow( //nolint:glint // notestingrawsql: no create query exists yet; writes belong to the management API milestone
		t.Context(), `
		INSERT INTO workload_issuers (organization_id, project_id, name, issuer, jwks_uri)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id
	`, organizationID, projectID, "live-issuer", f.issuer.OAuth21URL, f.jwksURI).Scan(&id)
	require.NoError(t, err)

	return id
}

func (f liveWorkloadFixture) seedAdmission(t *testing.T, organizationID string, projectID uuid.NullUUID, issuerID uuid.UUID) {
	t.Helper()

	_, err := f.conn.Exec( //nolint:glint // notestingrawsql: no create query exists yet; writes belong to the management API milestone
		t.Context(), `
		INSERT INTO workload_identity_admissions (organization_id, project_id, workload_issuer_id, subject)
		VALUES ($1, $2, $3, $4)
	`, organizationID, projectID, issuerID, liveWorkloadSubject)
	require.NoError(t, err)
}

func (f liveWorkloadFixture) softDeleteIssuer(t *testing.T, id uuid.UUID) {
	t.Helper()

	_, err := f.conn.Exec( //nolint:glint // notestingrawsql: no delete query exists yet; writes belong to the management API milestone
		t.Context(), `UPDATE workload_issuers SET deleted_at = clock_timestamp() WHERE id = $1`, id)
	require.NoError(t, err)
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

// present mints a fresh assertion for the admitted subject and runs it through
// every admission stage. Fresh on each call, so a second presentation is never
// refused as a replay before the stage under test is reached.
func (f liveWorkloadFixture) present(t *testing.T, endpoint *mcp.ResolvedMcpEndpoint) error {
	t.Helper()

	raw := oauthtest.MintWorkloadAssertion(t, f.issuer, oauthtest.WorkloadClaims(f.issuer, liveWorkloadSubject, liveWorkloadAudience))

	err := mcp.AdmitWorkloadAssertion(t.Context(), f.conn, f.verifier, endpoint, clientauth.Audiences{
		Issuer:   liveWorkloadAudience,
		Endpoint: liveWorkloadAudience + "/token",
	}, raw)
	if err != nil {
		return fmt.Errorf("admit workload assertion: %w", err)
	}

	return nil
}

// The baseline the rejections below depart from, and the proof the request
// counter they rely on counts: an admitted workload passes every stage, and
// doing so reaches the issuer.
func TestWorkloadAssertionPipeline_AdmittedWorkloadFromALiveIssuerPasses(t *testing.T) {
	t.Parallel()

	f := newLiveWorkloadFixture(t)
	issuerID := f.seedIssuer(t, f.organizationID, uuid.NullUUID{})
	f.seedAdmission(t, f.organizationID, uuid.NullUUID{UUID: f.projectID, Valid: true}, issuerID)

	err := f.present(t, f.endpoint(f.organizationID, f.projectID))

	require.NoError(t, err)
	require.Greater(t, f.issuer.Connections(), f.connectionsAtSetup, "an admitted assertion must have fetched the key set, or the no-egress tests assert against a counter that never moves")
}

// An issuer registered by a different organization is not trusted here, and
// the refusal costs no outbound request. Pinned at the issuer itself: the only
// server a fetch could have reached records none, rather than the test
// inferring the absence of egress from the verdict.
//
// The other organization has registered and admitted this very issuer and
// subject, so nothing but tenancy separates the two.
func TestWorkloadAssertionPipeline_IssuerOutsideTheTenancyMakesNoOutboundRequest(t *testing.T) {
	t.Parallel()

	f := newLiveWorkloadFixture(t)
	other := newLiveWorkloadOrganization(t, f.conn)
	otherIssuerID := f.seedIssuer(t, other, uuid.NullUUID{})
	f.seedAdmission(t, other, uuid.NullUUID{}, otherIssuerID)

	err := f.present(t, f.endpoint(f.organizationID, f.projectID))

	require.ErrorIs(t, err, mcp.ErrWorkloadIssuerUntrusted)
	require.Equal(t, f.connectionsAtSetup, f.issuer.Connections(), "an untrusted issuer must be refused before any connection to it is attempted")
}

// A soft-deleted issuer row is how an administrator withdraws trust. It stops
// resolving, so the assertion is refused at the issuer stage, and nothing is
// fetched for it, though its admission row is still in place.
func TestWorkloadAssertionPipeline_SoftDeletedIssuerIsUntrusted(t *testing.T) {
	t.Parallel()

	f := newLiveWorkloadFixture(t)
	issuerID := f.seedIssuer(t, f.organizationID, uuid.NullUUID{})
	f.seedAdmission(t, f.organizationID, uuid.NullUUID{}, issuerID)
	f.softDeleteIssuer(t, issuerID)

	err := f.present(t, f.endpoint(f.organizationID, f.projectID))

	require.ErrorIs(t, err, mcp.ErrWorkloadIssuerUntrusted)
	require.Equal(t, f.connectionsAtSetup, f.issuer.Connections(), "a withdrawn issuer must be refused before any connection to it is attempted")
}

// A subject admitted in one project is not admitted in a sibling project of the
// same organization. The assertion is genuine and verifies, since the issuer
// is shared at the organization tier, so the refusal is subject admission
// doing the isolation the project tier exists for.
//
// The sibling itself passing is the control: without it, a pipeline refusing
// every project-tier admission would pass this test too.
func TestWorkloadAssertionPipeline_ASiblingProjectsAdmissionDoesNotAdmit(t *testing.T) {
	t.Parallel()

	f := newLiveWorkloadFixture(t)
	sibling := newLiveWorkloadProject(t, f.conn, f.organizationID)
	issuerID := f.seedIssuer(t, f.organizationID, uuid.NullUUID{})
	f.seedAdmission(t, f.organizationID, uuid.NullUUID{UUID: sibling, Valid: true}, issuerID)

	err := f.present(t, f.endpoint(f.organizationID, f.projectID))
	require.ErrorIs(t, err, mcp.ErrWorkloadNotAdmitted)

	err = f.present(t, f.endpoint(f.organizationID, sibling))
	require.NoError(t, err, "the admission must admit its own project, or the refusal above proves nothing")
}

// An organization-tier admission admits the workload in every project of the
// organization. The positive counterpart to the sibling test, so the tier
// check cannot be satisfied by refusing everything.
func TestWorkloadAssertionPipeline_OrganizationTierAdmissionAdmitsEveryProject(t *testing.T) {
	t.Parallel()

	f := newLiveWorkloadFixture(t)
	other := newLiveWorkloadProject(t, f.conn, f.organizationID)
	issuerID := f.seedIssuer(t, f.organizationID, uuid.NullUUID{})
	f.seedAdmission(t, f.organizationID, uuid.NullUUID{}, issuerID)

	require.NoError(t, f.present(t, f.endpoint(f.organizationID, f.projectID)))
	require.NoError(t, f.present(t, f.endpoint(f.organizationID, other)))
}
