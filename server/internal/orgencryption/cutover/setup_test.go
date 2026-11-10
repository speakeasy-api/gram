package cutover_test

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/orgencryption/cutover"
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

const column = "example_table.secret_org_encrypted"

type testInstance struct {
	orgID   string
	flags   *feature.InMemory
	fixture *orgencryptiontest.Fixture
	sealer  *cutover.Sealer
}

// newTestInstance wires a Sealer over organization encryption for a fresh
// organization. The cutover flag starts off.
func newTestInstance(t *testing.T) *testInstance {
	t.Helper()

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	orgID := "org_" + uuid.NewString()
	_, err = orgrepo.New(conn).UpsertOrganizationMetadata(t.Context(), orgrepo.UpsertOrganizationMetadataParams{
		ID:          orgID,
		Name:        "Cutover Test Org",
		Slug:        "cutover-" + uuid.NewString(),
		WorkosID:    conv.ToPGText(orgID),
		Whitelisted: pgtype.Bool{Bool: false, Valid: false},
	})
	require.NoError(t, err)

	fixture := orgencryptiontest.New(t, conn)
	flags := &feature.InMemory{}
	sealer := cutover.New(testenv.NewLogger(t), testenv.NewEncryptionClient(t), cutover.AIProviderKeys, &cutover.OrgEncryption{
		Service:     fixture.Service,
		Provisioner: fixture.Provisioner,
		Flags:       flags,
	})

	return &testInstance{orgID: orgID, flags: flags, fixture: fixture, sealer: sealer}
}

func (ti *testInstance) move(enabled bool) {
	ti.flags.SetFlag(cutover.AIProviderKeys.Flag, ti.orgID, enabled)
}

// openOrg decrypts an organization ciphertext directly, bypassing the Sealer.
func (ti *testInstance) openOrg(t *testing.T, ciphertext string) string {
	t.Helper()

	encryptor, err := ti.fixture.Service.For(ti.orgID, cutover.AIProviderKeys.UseCase)
	require.NoError(t, err)

	plaintext, err := encryptor.Decrypt(t.Context(), column, ciphertext)
	require.NoError(t, err)

	return string(plaintext)
}
