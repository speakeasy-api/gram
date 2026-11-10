package orgencryption_test

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

	"github.com/speakeasy-api/gram/server/internal/conv"
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

const (
	useCase = orgencryption.UseCaseTailscaleGatewayState
	column  = "example_table.value_encrypted"
)

type testInstance struct {
	conn    *pgxpool.Pool
	orgID   string
	fixture *orgencryptiontest.Fixture
}

func newTestInstance(t *testing.T) *testInstance {
	t.Helper()

	return newTestInstanceWithConfig(t, orgencryption.ProvisionerConfig{
		Keys:                orgencryption.KeyConfig{KeyRing: "", PlatformCredentialID: uuid.Nil, PlatformServiceAccount: ""},
		SwitchGracePeriod:   0,
		ManagedKeyRetention: 0,
	})
}

// newTestInstanceWithoutRetention disables an unused managed KEK on the first
// sweep instead of after the retention.
func newTestInstanceWithoutRetention(t *testing.T) *testInstance {
	t.Helper()

	return newTestInstanceWithConfig(t, orgencryption.ProvisionerConfig{
		Keys:                orgencryption.KeyConfig{KeyRing: "", PlatformCredentialID: uuid.Nil, PlatformServiceAccount: ""},
		SwitchGracePeriod:   0,
		ManagedKeyRetention: time.Nanosecond,
	})
}

func newTestInstanceWithConfig(t *testing.T, cfg orgencryption.ProvisionerConfig) *testInstance {
	t.Helper()

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	return &testInstance{
		conn:    conn,
		orgID:   createOrganization(t, conn),
		fixture: orgencryptiontest.NewWithConfig(t, conn, cfg),
	}
}

func createOrganization(t *testing.T, conn *pgxpool.Pool) string {
	t.Helper()

	orgID := "org_" + uuid.NewString()
	_, err := orgrepo.New(conn).UpsertOrganizationMetadata(t.Context(), orgrepo.UpsertOrganizationMetadataParams{
		ID:          orgID,
		Name:        "Organization Encryption Test Org",
		Slug:        "org-encryption-" + uuid.NewString(),
		WorkosID:    conv.ToPGText(orgID),
		Whitelisted: pgtype.Bool{Bool: false, Valid: false},
	})
	require.NoError(t, err)

	return orgID
}

// provision runs EnsureUseCase for the test organization.
func (ti *testInstance) provision(t *testing.T) {
	t.Helper()

	require.NoError(t, ti.fixture.Provisioner.EnsureUseCase(t.Context(), ti.orgID, useCase, orgencryption.SystemActor()))
}

func (ti *testInstance) encryptor(t *testing.T) *orgencryption.Encryptor {
	t.Helper()

	encryptor, err := ti.fixture.Service.For(ti.orgID, useCase)
	require.NoError(t, err)

	return encryptor
}

// freshService builds a Service over the same database and KMS with empty
// caches, as another process would have.
func (ti *testInstance) freshService(t *testing.T) *orgencryption.Service {
	t.Helper()

	service, err := orgencryption.NewService(testenv.NewLogger(t), ti.conn, newIdentity(), ti.fixture.KMS.Clients, ti.fixture.Keys)
	require.NoError(t, err)

	return service
}
