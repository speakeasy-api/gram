package integrationcredentials_test

import (
	"context"
	"fmt"
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
	devicerepo "github.com/speakeasy-api/gram/server/internal/deviceintegrations/repo"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/integrationcredentials"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/orgencryption/orgencryptiontest"
	orgencryptionrepo "github.com/speakeasy-api/gram/server/internal/orgencryption/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
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
	credentials *integrationcredentials.Credentials
}

func newTestInstance(t *testing.T) *testInstance {
	t.Helper()

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	orgID := "org_" + uuid.NewString()
	_, err = orgrepo.New(conn).UpsertOrganizationMetadata(t.Context(), orgrepo.UpsertOrganizationMetadataParams{
		ID:          orgID,
		Name:        "Integration Credentials Test Org",
		Slug:        "integration-credentials-" + uuid.NewString(),
		WorkosID:    conv.ToPGText(orgID),
		Whitelisted: pgtype.Bool{Bool: false, Valid: false},
	})
	require.NoError(t, err)

	fixture := orgencryptiontest.New(t, conn)
	platform := testenv.NewEncryptionClient(t)
	transitions := orgencryption.NewTransitions(testenv.NewLogger(t), conn, audit.NewLogger(), fixture.Provisioner, orgencryption.TransitionsConfig{ClearSettleDelay: time.Millisecond})
	cipher := orgencryption.NewCipher(testenv.NewLogger(t), platform, fixture.Service, transitions)

	return &testInstance{
		conn:        conn,
		orgID:       orgID,
		fixture:     fixture,
		platform:    platform,
		transitions: transitions,
		cipher:      cipher,
		credentials: integrationcredentials.New(testenv.NewLogger(t), cipher),
	}
}

func (ti *testInstance) createConfig(t *testing.T) devicerepo.DeviceIntegrationConfig {
	t.Helper()

	config, err := devicerepo.New(ti.conn).InsertConfig(t.Context(), devicerepo.InsertConfigParams{
		OrganizationID: ti.orgID,
		Provider:       "provider-" + uuid.NewString()[:8],
		Settings:       []byte("{}"),
		Enabled:        true,
	})
	require.NoError(t, err)

	return config
}

// writeConfig is the fenced device integration write used by the tests.
func (ti *testInstance) writeConfig(id uuid.UUID) integrationcredentials.Write {
	return func(ctx context.Context, copies orgencryption.Copies, generation int64) (bool, error) {
		_, err := devicerepo.New(ti.conn).SetConfigCredentials(ctx, devicerepo.SetConfigCredentialsParams{
			PlatformCopy:      copies.Platform,
			OrgCopy:           copies.Org,
			Settings:          []byte("{}"),
			Enabled:           true,
			ID:                id,
			OrganizationID:    ti.orgID,
			DecidedGeneration: generation,
		})
		if err != nil {
			if isNoRows(err) {
				return false, nil
			}
			return false, fmt.Errorf("set config credentials: %w", err)
		}
		return true, nil
	}
}

func (ti *testInstance) stored(t *testing.T, config devicerepo.DeviceIntegrationConfig) orgencryption.Copies {
	t.Helper()

	row, err := devicerepo.New(ti.conn).GetConfigByOrgAndProvider(t.Context(), devicerepo.GetConfigByOrgAndProviderParams{OrganizationID: config.OrganizationID, Provider: config.Provider})
	require.NoError(t, err)

	return integrationcredentials.Stored(row.CredentialsEncrypted, row.CredentialsOrgEncrypted)
}

func (ti *testInstance) enroll(t *testing.T) {
	t.Helper()

	_, err := ti.transitions.Enroll(t.Context(), ti.orgID, orgencryption.UseCaseIntegrationCredentials, orgencryption.SystemActor())
	require.NoError(t, err)
}

func (ti *testInstance) finalize(t *testing.T) {
	t.Helper()

	stores := integrationcredentials.CopyStores(ti.conn)

	_, err := orgencryption.NewRunner(testenv.NewLogger(t), ti.conn, ti.cipher, testenv.NewMeterProvider(t)).Run(t.Context(), ti.orgID, orgencryption.UseCaseIntegrationCredentials, stores)
	require.NoError(t, err)

	err = orgencryptionrepo.New(ti.conn).SetEncryptionTransitionStateChangedAt(t.Context(), orgencryptionrepo.SetEncryptionTransitionStateChangedAtParams{
		StateChangedAt: pgtype.Timestamptz{Time: time.Now().Add(-time.Hour), InfinityModifier: pgtype.Finite, Valid: true},
		OrganizationID: ti.orgID,
		UseCase:        string(orgencryption.UseCaseIntegrationCredentials),
	})
	require.NoError(t, err)

	_, err = ti.transitions.Finalize(t.Context(), ti.orgID, orgencryption.UseCaseIntegrationCredentials, orgencryption.FinalizeRequest{MinimumDualWrite: time.Minute, Stores: stores}, orgencryption.SystemActor())
	require.NoError(t, err)

	_, err = ti.transitions.ClearPlatformCopies(t.Context(), ti.orgID, orgencryption.UseCaseIntegrationCredentials, stores, orgencryption.SystemActor())
	require.NoError(t, err)
}
