package clientsecrets_test

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
	"github.com/speakeasy-api/gram/server/internal/remotesessions/clientsecrets"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
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
	conn        *pgxpool.Pool
	orgID       string
	projectID   uuid.UUID
	issuerID    uuid.UUID
	fixture     *orgencryptiontest.Fixture
	platform    *encryption.Client
	transitions *orgencryption.Transitions
	cipher      *orgencryption.Cipher
	secrets     *clientsecrets.Secrets
}

func newTestInstance(t *testing.T) *testInstance {
	t.Helper()

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	orgID := "org_" + uuid.NewString()
	_, err = orgrepo.New(conn).UpsertOrganizationMetadata(t.Context(), orgrepo.UpsertOrganizationMetadataParams{
		ID:          orgID,
		Name:        "Client Secrets Test Org",
		Slug:        "client-secrets-" + uuid.NewString(),
		WorkosID:    conv.ToPGText(orgID),
		Whitelisted: pgtype.Bool{Bool: false, Valid: false},
	})
	require.NoError(t, err)

	project, err := projectsrepo.New(conn).CreateProject(t.Context(), projectsrepo.CreateProjectParams{
		Name:           "Client Secrets Test Project",
		Slug:           "client-secrets-" + uuid.NewString()[:8],
		OrganizationID: orgID,
	})
	require.NoError(t, err)

	issuer, err := repo.New(conn).CreateRemoteSessionIssuer(t.Context(), repo.CreateRemoteSessionIssuerParams{
		ProjectID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Slug:      "client-secrets-" + uuid.NewString()[:8],
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

	fixture := orgencryptiontest.New(t, conn)
	platform := testenv.NewEncryptionClient(t)
	transitions := orgencryption.NewTransitions(testenv.NewLogger(t), conn, audit.NewLogger(), fixture.Provisioner, orgencryption.TransitionsConfig{ClearSettleDelay: time.Millisecond})
	cipher := orgencryption.NewCipher(testenv.NewLogger(t), platform, fixture.Service, transitions)

	return &testInstance{
		conn:        conn,
		orgID:       orgID,
		projectID:   project.ID,
		issuerID:    issuer.ID,
		fixture:     fixture,
		platform:    platform,
		transitions: transitions,
		cipher:      cipher,
		secrets:     clientsecrets.New(testenv.NewLogger(t), cipher, platform),
	}
}

// createClient creates a client with no secret, owned by owner.
func (ti *testInstance) createClient(t *testing.T, owner clientsecrets.Owner) repo.RemoteSessionClient {
	t.Helper()

	client, err := repo.New(ti.conn).CreateRemoteSessionClient(t.Context(), repo.CreateRemoteSessionClientParams{
		ProjectID:               owner.ProjectID,
		OrganizationID:          owner.OrganizationID,
		RemoteSessionIssuerID:   ti.issuerID,
		ClientID:                "client-" + uuid.NewString(),
		TokenEndpointAuthMethod: conv.ToPGText("client_secret_basic"),
	})
	require.NoError(t, err)

	return client
}

// projectOwner owns a client through the test project and its organization.
func (ti *testInstance) projectOwner() clientsecrets.Owner {
	return clientsecrets.OwnerOf(conv.ToPGText(ti.orgID), uuid.NullUUID{UUID: ti.projectID, Valid: true})
}

// organizationOwner owns a client through the test organization alone.
func (ti *testInstance) organizationOwner() clientsecrets.Owner {
	return clientsecrets.OwnerOf(conv.ToPGText(ti.orgID), uuid.NullUUID{UUID: uuid.Nil, Valid: false})
}

// enroll moves the test organization's client secrets to dual_write.
func (ti *testInstance) enroll(t *testing.T) {
	t.Helper()

	_, err := ti.transitions.Enroll(t.Context(), ti.orgID, orgencryption.UseCaseOAuthClientSecrets, orgencryption.SystemActor())
	require.NoError(t, err)
}

// finalize moves the test organization's client secrets from dual_write to
// org_kms and clears their platform-key copies.
func (ti *testInstance) finalize(t *testing.T) {
	t.Helper()

	stores := []orgencryption.CopyStore{clientsecrets.NewCopyStore(ti.conn)}

	_, err := orgencryption.NewRunner(testenv.NewLogger(t), ti.conn, ti.cipher, testenv.NewMeterProvider(t)).Run(t.Context(), ti.orgID, orgencryption.UseCaseOAuthClientSecrets, stores)
	require.NoError(t, err)

	err = orgencryptionrepo.New(ti.conn).SetEncryptionTransitionStateChangedAt(t.Context(), orgencryptionrepo.SetEncryptionTransitionStateChangedAtParams{
		StateChangedAt: pgtype.Timestamptz{Time: time.Now().Add(-time.Hour), InfinityModifier: pgtype.Finite, Valid: true},
		OrganizationID: ti.orgID,
		UseCase:        string(orgencryption.UseCaseOAuthClientSecrets),
	})
	require.NoError(t, err)

	_, err = ti.transitions.Finalize(t.Context(), ti.orgID, orgencryption.UseCaseOAuthClientSecrets, orgencryption.FinalizeRequest{MinimumDualWrite: time.Minute, Stores: stores}, orgencryption.SystemActor())
	require.NoError(t, err)

	_, err = ti.transitions.ClearPlatformCopies(t.Context(), ti.orgID, orgencryption.UseCaseOAuthClientSecrets, stores, orgencryption.SystemActor())
	require.NoError(t, err)
}

// stored reads the client's copies and revision back from the database.
func (ti *testInstance) stored(t *testing.T, clientID uuid.UUID) repo.RemoteSessionClient {
	t.Helper()

	client, err := repo.New(ti.conn).GetRemoteSessionClientRowFixture(t.Context(), clientID)
	require.NoError(t, err)

	return client
}
