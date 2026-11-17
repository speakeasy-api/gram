package upstreamtokens_test

import (
	"context"
	"log"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/orgencryption/orgencryptiontest"
	orgencryptionrepo "github.com/speakeasy-api/gram/server/internal/orgencryption/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/upstreamtokens"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true})
	if err != nil {
		log.Fatalf("launch test infrastructure: %v", err)
		os.Exit(1)
	}

	infra = res

	code := m.Run()

	if err := cleanup(); err != nil {
		log.Fatalf("cleanup test infrastructure: %v", err)
		os.Exit(1)
	}

	os.Exit(code)
}

type testInstance struct {
	conn              *pgxpool.Pool
	orgID             string
	projectID         uuid.UUID
	remoteIssuerID    uuid.UUID
	userSessionIssuer uuid.UUID
	fixture           *orgencryptiontest.Fixture
	platform          *encryption.Client
	transitions       *orgencryption.Transitions
	cipher            *orgencryption.Cipher
	tokens            *upstreamtokens.Tokens
}

func newTestInstance(t *testing.T) *testInstance {
	t.Helper()

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	orgID := "org_" + uuid.NewString()
	_, err = orgrepo.New(conn).UpsertOrganizationMetadata(t.Context(), orgrepo.UpsertOrganizationMetadataParams{
		ID:          orgID,
		Name:        "Session Tokens Test Org",
		Slug:        "session-tokens-" + uuid.NewString(),
		WorkosID:    conv.ToPGText(orgID),
		Whitelisted: pgtype.Bool{Bool: false, Valid: false},
	})
	require.NoError(t, err)

	project, err := projectsrepo.New(conn).CreateProject(t.Context(), projectsrepo.CreateProjectParams{
		Name:           "Session Tokens Test Project",
		Slug:           "session-tokens-" + uuid.NewString()[:8],
		OrganizationID: orgID,
	})
	require.NoError(t, err)

	remoteIssuer, err := repo.New(conn).CreateRemoteSessionIssuer(t.Context(), repo.CreateRemoteSessionIssuerParams{
		ProjectID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Slug:      "session-tokens-" + uuid.NewString()[:8],
		Issuer:    "https://issuer.example.test/" + uuid.NewString(),

		ScopesSupported:                           []string{},
		GrantTypesSupported:                       []string{},
		AuthorizationGrantProfilesSupported:       []string{},
		ResponseTypesSupported:                    []string{},
		TokenEndpointAuthMethodsSupported:         []string{},
		CodeChallengeMethodsSupported:             []string{},
		IntrospectionEndpointAuthMethodsSupported: []string{},
		IDTokenSigningAlgValuesSupported:          []string{},
		ClaimsSupported:                           []string{},
	})
	require.NoError(t, err)

	userSessionIssuer, err := usersessionsrepo.New(conn).CreateUserSessionIssuer(t.Context(), usersessionsrepo.CreateUserSessionIssuerParams{
		ProjectID:          project.ID,
		Slug:               "session-tokens-" + uuid.NewString()[:8],
		AuthnChallengeMode: "interactive",
		SessionDuration:    pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Days: 0, Months: 0, Valid: true},
	})
	require.NoError(t, err)

	fixture := orgencryptiontest.New(t, conn)
	platform := testenv.NewEncryptionClient(t)
	transitions := orgencryption.NewTransitions(testenv.NewLogger(t), conn, audit.NewLogger(), fixture.Provisioner, orgencryption.TransitionsConfig{ClearSettleDelay: time.Millisecond})
	cipher := orgencryption.NewCipher(testenv.NewLogger(t), platform, fixture.Service, transitions)

	return &testInstance{
		conn:              conn,
		orgID:             orgID,
		projectID:         project.ID,
		remoteIssuerID:    remoteIssuer.ID,
		userSessionIssuer: userSessionIssuer.ID,
		fixture:           fixture,
		platform:          platform,
		transitions:       transitions,
		cipher:            cipher,
		tokens:            upstreamtokens.New(testenv.NewLogger(t), cipher, platform),
	}
}

// org is the test organization as a row's organization_id.
func (ti *testInstance) org() pgtype.Text {
	return conv.ToPGText(ti.orgID)
}

// nullText is a NULL column value.
func nullText() pgtype.Text {
	return pgtype.Text{String: "", Valid: false}
}

// createClient creates a client in the test project.
func (ti *testInstance) createClient(t *testing.T) repo.RemoteSessionClient {
	t.Helper()

	client, err := repo.New(ti.conn).CreateRemoteSessionClient(t.Context(), repo.CreateRemoteSessionClientParams{
		ProjectID:               uuid.NullUUID{UUID: ti.projectID, Valid: true},
		OrganizationID:          ti.org(),
		RemoteSessionIssuerID:   ti.remoteIssuerID,
		ClientID:                "client-" + uuid.NewString(),
		TokenEndpointAuthMethod: conv.ToPGText("none"),
	})
	require.NoError(t, err)

	return client
}

// createSession creates a session with no tokens under a new client of the
// test project, so its organization is the test organization.
func (ti *testInstance) createSession(t *testing.T) repo.RemoteSession {
	t.Helper()

	session, err := repo.New(ti.conn).CreateRemoteSessionFixture(t.Context(), repo.CreateRemoteSessionFixtureParams{
		SubjectUrn:            urn.NewUserSubject("user_" + uuid.NewString()),
		UserSessionIssuerID:   ti.userSessionIssuer,
		RemoteSessionClientID: ti.createClient(t).ID,
	})
	require.NoError(t, err)
	require.Equal(t, ti.org(), session.OrganizationID)

	return session
}

// createUnattributedSession creates a session whose organization_id was never
// backfilled, under a client of the test organization.
func (ti *testInstance) createUnattributedSession(t *testing.T) repo.RemoteSession {
	t.Helper()

	session, err := repo.New(ti.conn).ClearRemoteSessionOrganizationFixture(t.Context(), ti.createSession(t).ID)
	require.NoError(t, err)

	return session
}

// enroll moves the test organization's session tokens to dual_write.
func (ti *testInstance) enroll(t *testing.T) {
	t.Helper()

	_, err := ti.transitions.Enroll(t.Context(), ti.orgID, orgencryption.UseCaseOAuthSessionTokens, orgencryption.SystemActor())
	require.NoError(t, err)
}

// runner backfills and verifies the test organization's session tokens.
func (ti *testInstance) runner(t *testing.T) *orgencryption.Runner {
	t.Helper()

	return orgencryption.NewRunner(testenv.NewLogger(t), ti.conn, ti.cipher, testenv.NewMeterProvider(t))
}

// finalize moves the test organization's session tokens from dual_write to
// org_kms and clears their platform-key copies.
func (ti *testInstance) finalize(t *testing.T) {
	t.Helper()

	stores := upstreamtokens.CopyStores(ti.conn)

	_, err := ti.runner(t).Run(t.Context(), ti.orgID, orgencryption.UseCaseOAuthSessionTokens, stores)
	require.NoError(t, err)

	err = orgencryptionrepo.New(ti.conn).SetEncryptionTransitionStateChangedAt(t.Context(), orgencryptionrepo.SetEncryptionTransitionStateChangedAtParams{
		StateChangedAt: pgtype.Timestamptz{Time: time.Now().Add(-time.Hour), InfinityModifier: pgtype.Finite, Valid: true},
		OrganizationID: ti.orgID,
		UseCase:        string(orgencryption.UseCaseOAuthSessionTokens),
	})
	require.NoError(t, err)

	_, err = ti.transitions.Finalize(t.Context(), ti.orgID, orgencryption.UseCaseOAuthSessionTokens, orgencryption.FinalizeRequest{MinimumDualWrite: time.Minute, Stores: stores}, orgencryption.SystemActor())
	require.NoError(t, err)

	_, err = ti.transitions.ClearPlatformCopies(t.Context(), ti.orgID, orgencryption.UseCaseOAuthSessionTokens, stores, orgencryption.SystemActor())
	require.NoError(t, err)
}

// otherProcessTransitions is transition state in another process: same
// database and keys, its own cache.
func (ti *testInstance) otherProcessTransitions(t *testing.T) *orgencryption.Transitions {
	t.Helper()

	return orgencryption.NewTransitions(testenv.NewLogger(t), ti.conn, audit.NewLogger(), ti.fixture.Provisioner, orgencryption.TransitionsConfig{ClearSettleDelay: time.Millisecond})
}

// stored reads the session row back from the database.
func (ti *testInstance) stored(t *testing.T, sessionID uuid.UUID) repo.RemoteSession {
	t.Helper()

	session, err := repo.New(ti.conn).GetRemoteSessionRowFixture(t.Context(), sessionID)
	require.NoError(t, err)

	return session
}

// open opens a session's stored access and refresh tokens.
func (ti *testInstance) open(t *testing.T, session repo.RemoteSession) (string, string) {
	t.Helper()

	access, err := ti.tokens.Open(t.Context(), ti.conn, session.OrganizationID, upstreamtokens.RemoteSessionAccessToken, upstreamtokens.Stored(session.AccessTokenEncrypted, session.AccessTokenOrgEncrypted))
	require.NoError(t, err)

	refresh, err := ti.tokens.Open(t.Context(), ti.conn, session.OrganizationID, upstreamtokens.RemoteSessionRefreshToken, upstreamtokens.Stored(session.RefreshTokenEncrypted, session.RefreshTokenOrgEncrypted))
	require.NoError(t, err)

	return access, refresh
}
