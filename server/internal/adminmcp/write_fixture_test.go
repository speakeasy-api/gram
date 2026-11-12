package adminmcp

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/adminmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
)

const writeTestSubject = "user:staff-subject"

// writeOwner is the connection a fixture's writes run on.
type writeOwner struct {
	SubjectURN   string
	ClientRowID  uuid.UUID
	ConnectionID uuid.UUID
	Generation   uuid.UUID
}

type writeFixture struct {
	db     *pgxpool.Pool
	runner *writeRunner
	cipher *encryption.Client
	owner  writeOwner
	orgA   string
	orgB   string
	reauth func()
}

func newWriteFixture(t *testing.T, name string) writeFixture {
	t.Helper()
	ctx := t.Context()
	db, err := staffMCPInfra.CloneTestDatabase(t, name)
	require.NoError(t, err)

	orgs := orgrepo.New(db)
	orgA, orgB := "org_a_"+uuid.NewString(), "org_b_"+uuid.NewString()
	require.NoError(t, orgs.CreateOrganizationMetadata(ctx, orgrepo.CreateOrganizationMetadataParams{ID: orgA, Name: "Synthetic A", Slug: "synthetic-a-" + uuid.NewString()}))
	require.NoError(t, orgs.CreateOrganizationMetadata(ctx, orgrepo.CreateOrganizationMetadataParams{ID: orgB, Name: "Synthetic B", Slug: "synthetic-b-" + uuid.NewString()}))

	clientID := "client_" + uuid.NewString()
	require.NoError(t, (postgresStaffClientStore{db: db}).RegisterClient(ctx, staffOAuthClient{ID: clientID, Name: "Test editor", SecretHash: "", RedirectURIs: []string{"http://localhost:5555/callback"}, SecretExpiresAt: nil}))
	authorizations := postgresStaffAuthorizationStore{db: db}
	grants := postgresStaffGrantStore{db: db}
	verifier := "test-verifier-" + uuid.NewString() + uuid.NewString()
	cipher, err := encryption.NewWithBytes(make([]byte, 32))
	require.NoError(t, err)
	encryptedSession, err := cipher.Encrypt([]byte("browser-session"))
	require.NoError(t, err)
	now := time.Now()
	authorization := staffAuthorization{
		Subject: writeTestSubject, ClientID: clientID, SessionEnc: encryptedSession, ResourceURI: staffAudience,
		Scopes: []string{ScopeRead, ScopeWrite}, CodeHash: staffTokenHash("code-" + uuid.NewString()), CodeChallenge: staffChallengeForTest(verifier),
		RedirectURI: "http://localhost:5555/callback", ExpiresAt: now.Add(2 * time.Hour), GrantExpires: now.Add(10 * time.Minute),
	}
	require.NoError(t, authorizations.Authorize(ctx, authorization))
	connection, err := grants.ValidateGrant(ctx, authorization.CodeHash, clientID, authorization.RedirectURI, verifier, now)
	require.NoError(t, err)

	return writeFixture{
		db:     db,
		runner: newWriteRunner(db, nil),
		cipher: cipher,
		owner:  writeOwner{SubjectURN: writeTestSubject, ClientRowID: connection.ClientRowID, ConnectionID: connection.ID, Generation: connection.Generation},
		orgA:   orgA,
		orgB:   orgB,
		reauth: func() {
			authorization.CodeHash = staffTokenHash("code-" + uuid.NewString())
			require.NoError(t, authorizations.Authorize(ctx, authorization))
		},
	}
}

func countWriteEvents(t *testing.T, db *pgxpool.Pool, operation WriteOperation, organizationID, event string) int {
	t.Helper()
	count, err := repo.New(db).CountWriteEventsFixture(t.Context(), repo.CountWriteEventsFixtureParams{Operation: conv.ToPGTextEmpty(string(operation)), OrganizationID: conv.ToPGTextEmpty(organizationID), Event: event})
	require.NoError(t, err)
	return int(count)
}

func organizationName(t *testing.T, db *pgxpool.Pool, id string) string {
	t.Helper()
	org, err := orgrepo.New(db).GetOrganizationMetadata(t.Context(), id)
	require.NoError(t, err)
	return org.Name
}
