package runnerkeys_test

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

	assetsrepo "github.com/speakeasy-api/gram/server/internal/assets/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	deploymentsrepo "github.com/speakeasy-api/gram/server/internal/deployments/repo"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/functions/runnerkeys"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/orgencryption/orgencryptiontest"
	orgencryptionrepo "github.com/speakeasy-api/gram/server/internal/orgencryption/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true})
	if err != nil {
		log.Fatalf("launch test infrastructure: %v", err)
	}

	infra = res

	code := m.Run()

	if err := cleanup(); err != nil {
		log.Fatalf("cleanup test infrastructure: %v", err)
	}

	os.Exit(code)
}

type testInstance struct {
	conn        *pgxpool.Pool
	orgID       string
	fixture     *orgencryptiontest.Fixture
	platform    *encryption.Client
	transitions *orgencryption.Transitions
	cipher      *orgencryption.Cipher
	keys        *runnerkeys.Keys
}

func newTestInstance(t *testing.T) *testInstance {
	t.Helper()

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	fixture := orgencryptiontest.New(t, conn)
	platform := testenv.NewEncryptionClient(t)
	transitions := orgencryption.NewTransitions(testenv.NewLogger(t), conn, audit.NewLogger(), fixture.Provisioner, orgencryption.TransitionsConfig{ClearSettleDelay: time.Millisecond})
	cipher := orgencryption.NewCipher(testenv.NewLogger(t), platform, fixture.Service, transitions)

	return &testInstance{
		conn:        conn,
		orgID:       createOrganization(t, conn),
		fixture:     fixture,
		platform:    platform,
		transitions: transitions,
		cipher:      cipher,
		keys:        runnerkeys.New(testenv.NewLogger(t), cipher, platform),
	}
}

func createOrganization(t *testing.T, conn *pgxpool.Pool) string {
	t.Helper()

	orgID := "org_" + uuid.NewString()
	_, err := orgrepo.New(conn).UpsertOrganizationMetadata(t.Context(), orgrepo.UpsertOrganizationMetadataParams{
		ID:          orgID,
		Name:        "Runner Keys Test Org",
		Slug:        "runner-keys-" + uuid.NewString(),
		WorkosID:    conv.ToPGText(orgID),
		Whitelisted: pgtype.Bool{Bool: false, Valid: false},
	})
	require.NoError(t, err)

	return orgID
}

// function is one function of a deployment, waiting for its runner key.
type function struct {
	projectID    uuid.UUID
	deploymentID uuid.UUID
	functionID   uuid.UUID
}

// createProject creates a project in the organization.
func (ti *testInstance) createProject(t *testing.T, organizationID string) uuid.UUID {
	t.Helper()

	slug := "runner-keys-" + uuid.NewString()[:8]
	project, err := projectsrepo.New(ti.conn).CreateProject(t.Context(), projectsrepo.CreateProjectParams{
		Name:           slug,
		Slug:           slug,
		OrganizationID: organizationID,
	})
	require.NoError(t, err)

	return project.ID
}

// createFunction creates a deployment of the project with one function. The
// function's asset is stored under assetOrganizationID, which must have a
// metadata row.
func (ti *testInstance) createFunction(t *testing.T, assetOrganizationID string, projectID uuid.UUID) function {
	t.Helper()

	ctx := t.Context()
	deployments := deploymentsrepo.New(ti.conn)
	deploymentID, err := deployments.InsertDeployment(ctx, deploymentsrepo.InsertDeploymentParams{
		ProjectID:      projectID,
		OrganizationID: assetOrganizationID,
		UserID:         "user-" + uuid.NewString(),
		IdempotencyKey: uuid.NewString(),
	})
	require.NoError(t, err)

	asset, err := assetsrepo.New(ti.conn).CreateAsset(ctx, assetsrepo.CreateAssetParams{
		Name: "fn.zip", Url: "file://fn.zip", ProjectID: projectID,
		OrganizationID: assetOrganizationID, Sha256: "fn-" + uuid.NewString(),
		Kind: "functions", ContentType: "application/zip", ContentLength: 1,
	})
	require.NoError(t, err)

	fn, err := deployments.UpsertDeploymentFunctionsAsset(ctx, deploymentsrepo.UpsertDeploymentFunctionsAssetParams{
		DeploymentID: deploymentID, AssetID: asset.ID, Name: "fn", Slug: "fn",
		Runtime: "nodejs:22", MemoryMib: pgtype.Int4{Int32: 128, Valid: true},
		Scale: pgtype.Int4{Int32: 1, Valid: true},
	})
	require.NoError(t, err)

	return function{projectID: projectID, deploymentID: deploymentID, functionID: fn.ID}
}

// create stores a runner key for fn through Keys and returns its row id.
func (ti *testInstance) create(t *testing.T, fn function) uuid.UUID {
	t.Helper()

	ids, err := ti.keys.Create(t.Context(), ti.conn, fn.projectID, fn.deploymentID, []uuid.UUID{fn.functionID})
	require.NoError(t, err)
	require.Len(t, ids, 1)

	return ids[0]
}

// stored reads the runner key row back from the database.
func (ti *testInstance) stored(t *testing.T, id uuid.UUID) testrepo.FunctionsAccess {
	t.Helper()

	row, err := testrepo.New(ti.conn).GetFunctionsAccessRowFixture(t.Context(), id)
	require.NoError(t, err)

	return row
}

// open opens the stored runner key through keys.
func (ti *testInstance) open(t *testing.T, keys *runnerkeys.Keys, id uuid.UUID) []byte {
	t.Helper()

	key, err := keys.Open(t.Context(), storedKey(ti.stored(t, id)))
	require.NoError(t, err)

	return key
}

func storedKey(row testrepo.FunctionsAccess) runnerkeys.Stored {
	return runnerkeys.Stored{
		OrganizationID: row.OrganizationID,
		Platform:       row.EncryptionKey,
		Org:            row.EncryptionKeyOrgEncrypted,
	}
}

// enroll moves the test organization's runner keys to dual_write.
func (ti *testInstance) enroll(t *testing.T) {
	t.Helper()

	_, err := ti.transitions.Enroll(t.Context(), ti.orgID, orgencryption.UseCaseFunctionRunnerKeys, orgencryption.SystemActor())
	require.NoError(t, err)
}

func (ti *testInstance) stores() []orgencryption.CopyStore {
	return []orgencryption.CopyStore{runnerkeys.NewCopyStore(ti.conn)}
}

// run backfills and verifies the organization-key copies.
func (ti *testInstance) run(t *testing.T) orgencryption.RunResult {
	t.Helper()

	result, err := orgencryption.NewRunner(testenv.NewLogger(t), ti.conn, ti.cipher, testenv.NewMeterProvider(t)).Run(t.Context(), ti.orgID, orgencryption.UseCaseFunctionRunnerKeys, ti.stores())
	require.NoError(t, err)

	return result
}

// finalize moves the test organization's runner keys from dual_write to
// org_kms and clears their platform-key copies.
func (ti *testInstance) finalize(t *testing.T) {
	t.Helper()

	ti.run(t)

	err := orgencryptionrepo.New(ti.conn).SetEncryptionTransitionStateChangedAt(t.Context(), orgencryptionrepo.SetEncryptionTransitionStateChangedAtParams{
		StateChangedAt: pgtype.Timestamptz{Time: time.Now().Add(-time.Hour), InfinityModifier: pgtype.Finite, Valid: true},
		OrganizationID: ti.orgID,
		UseCase:        string(orgencryption.UseCaseFunctionRunnerKeys),
	})
	require.NoError(t, err)

	_, err = ti.transitions.Finalize(t.Context(), ti.orgID, orgencryption.UseCaseFunctionRunnerKeys, orgencryption.FinalizeRequest{MinimumDualWrite: time.Minute, Stores: ti.stores()}, orgencryption.SystemActor())
	require.NoError(t, err)

	_, err = ti.transitions.ClearPlatformCopies(t.Context(), ti.orgID, orgencryption.UseCaseFunctionRunnerKeys, ti.stores(), orgencryption.SystemActor())
	require.NoError(t, err)
}

// otherProcessKeys is Keys in another process: same database and keys, its
// own transition state cache.
func (ti *testInstance) otherProcessKeys(t *testing.T) *runnerkeys.Keys {
	t.Helper()

	transitions := orgencryption.NewTransitions(testenv.NewLogger(t), ti.conn, audit.NewLogger(), ti.fixture.Provisioner, orgencryption.TransitionsConfig{ClearSettleDelay: time.Millisecond})
	return runnerkeys.New(testenv.NewLogger(t), orgencryption.NewCipher(testenv.NewLogger(t), ti.platform, ti.fixture.Service, transitions), ti.platform)
}
