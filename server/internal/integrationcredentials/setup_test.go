package integrationcredentials_test

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/integrationcredentials"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/orgencryption/orgencryptiontest"
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
	conn     *pgxpool.Pool
	orgID    string
	legacy   *encryption.Client
	features *feature.InMemory
	fixture  *orgencryptiontest.Fixture
	cipher   *integrationcredentials.Cipher
}

func newTestInstance(t *testing.T) *testInstance {
	t.Helper()

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	legacy := testenv.NewEncryptionClient(t)
	features := &feature.InMemory{}
	fixture := orgencryptiontest.New(t, conn)

	return &testInstance{
		conn:     conn,
		orgID:    createOrganization(t, conn),
		legacy:   legacy,
		features: features,
		fixture:  fixture,
		cipher:   integrationcredentials.New(testenv.NewLogger(t), legacy, fixture.Service, fixture.Provisioner, features),
	}
}

func createOrganization(t *testing.T, conn *pgxpool.Pool) string {
	t.Helper()

	orgID := "org_" + uuid.NewString()
	_, err := orgrepo.New(conn).UpsertOrganizationMetadata(t.Context(), orgrepo.UpsertOrganizationMetadataParams{
		ID:          orgID,
		Name:        "Integration Credentials Test Org",
		Slug:        "integration-credentials-" + uuid.NewString(),
		WorkosID:    conv.ToPGText(orgID),
		Whitelisted: pgtype.Bool{Bool: false, Valid: false},
	})
	require.NoError(t, err)

	return orgID
}

func (ti *testInstance) enroll(organizationID string) {
	ti.features.SetFlag(feature.FlagOrgEncryptedIntegrationCredentials, organizationID, true)
}

func (ti *testInstance) unenroll(organizationID string) {
	ti.features.SetFlag(feature.FlagOrgEncryptedIntegrationCredentials, organizationID, false)
}

func (ti *testInstance) suspend(t *testing.T, organizationID string) {
	t.Helper()

	require.NoError(t, ti.fixture.Provisioner.Suspend(t.Context(), organizationID, "customer disabled the key", orgencryption.SystemActor()))
}

func stored(sealed integrationcredentials.Sealed) integrationcredentials.Stored {
	return integrationcredentials.Stored{Legacy: conv.ToPGText(sealed.Legacy), Org: sealed.Org}
}
