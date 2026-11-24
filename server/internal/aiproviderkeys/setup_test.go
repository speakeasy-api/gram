package aiproviderkeys_test

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/aiproviderkeys"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/orgencryption/orgencryptiontest"
	orgencryptionrepo "github.com/speakeasy-api/gram/server/internal/orgencryption/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	openrouterrepo "github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter/repo"
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
	keys        *aiproviderkeys.Keys
}

func newTestInstance(t *testing.T) *testInstance {
	t.Helper()

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	orgID := "org_" + uuid.NewString()
	_, err = orgrepo.New(conn).UpsertOrganizationMetadata(t.Context(), orgrepo.UpsertOrganizationMetadataParams{
		ID:          orgID,
		Name:        "AI Provider Keys Test Org",
		Slug:        "ai-provider-keys-" + uuid.NewString(),
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
		keys:        aiproviderkeys.New(testenv.NewLogger(t), cipher),
	}
}

// createKey is the fenced OpenRouter key write used by the tests.
func (ti *testInstance) createKey(keyType string) aiproviderkeys.Write {
	return func(ctx context.Context, copies orgencryption.Copies, generation int64) (bool, error) {
		_, err := openrouterrepo.New(ti.conn).CreateOpenRouterAPIKey(ctx, openrouterrepo.CreateOpenRouterAPIKeyParams{
			OrganizationID:    ti.orgID,
			KeyType:           keyType,
			KeyEncrypted:      copies.Platform,
			KeyOrgEncrypted:   copies.Org,
			KeyHash:           "hash-" + keyType,
			MonthlyCredits:    10,
			DecidedGeneration: generation,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("create openrouter key: %w", err)
		}
		return true, nil
	}
}

func (ti *testInstance) stored(t *testing.T, keyType string) orgencryption.Copies {
	t.Helper()

	row, err := openrouterrepo.New(ti.conn).GetOpenRouterAPIKey(t.Context(), openrouterrepo.GetOpenRouterAPIKeyParams{OrganizationID: ti.orgID, KeyType: keyType})
	require.NoError(t, err)

	return aiproviderkeys.Stored(row.KeyEncrypted, row.KeyOrgEncrypted)
}

func (ti *testInstance) enroll(t *testing.T) {
	t.Helper()

	_, err := ti.transitions.Enroll(t.Context(), ti.orgID, orgencryption.UseCaseAIProviderKeys, orgencryption.SystemActor())
	require.NoError(t, err)
}

func (ti *testInstance) finalize(t *testing.T) {
	t.Helper()

	stores := aiproviderkeys.CopyStores(ti.conn)

	_, err := orgencryption.NewRunner(testenv.NewLogger(t), ti.conn, ti.cipher, testenv.NewMeterProvider(t)).Run(t.Context(), ti.orgID, orgencryption.UseCaseAIProviderKeys, stores)
	require.NoError(t, err)

	err = orgencryptionrepo.New(ti.conn).SetEncryptionTransitionStateChangedAt(t.Context(), orgencryptionrepo.SetEncryptionTransitionStateChangedAtParams{
		StateChangedAt: pgtype.Timestamptz{Time: time.Now().Add(-time.Hour), InfinityModifier: pgtype.Finite, Valid: true},
		OrganizationID: ti.orgID,
		UseCase:        string(orgencryption.UseCaseAIProviderKeys),
	})
	require.NoError(t, err)

	_, err = ti.transitions.Finalize(t.Context(), ti.orgID, orgencryption.UseCaseAIProviderKeys, orgencryption.FinalizeRequest{MinimumDualWrite: time.Minute, Stores: stores}, orgencryption.SystemActor())
	require.NoError(t, err)

	_, err = ti.transitions.ClearPlatformCopies(t.Context(), ti.orgID, orgencryption.UseCaseAIProviderKeys, stores, orgencryption.SystemActor())
	require.NoError(t, err)
}
