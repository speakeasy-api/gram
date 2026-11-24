package ingresscredentials_test

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
	"github.com/speakeasy-api/gram/server/internal/k8s"
	"github.com/speakeasy-api/gram/server/internal/networkingress/ingresscredentials"
	"github.com/speakeasy-api/gram/server/internal/networkingress/repo"
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
	fixture     *orgencryptiontest.Fixture
	platform    *encryption.Client
	transitions *orgencryption.Transitions
	cipher      *orgencryption.Cipher
	credentials *ingresscredentials.Credentials
}

func newTestInstance(t *testing.T) *testInstance {
	t.Helper()

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	orgID := "org_" + uuid.NewString()
	_, err = orgrepo.New(conn).UpsertOrganizationMetadata(t.Context(), orgrepo.UpsertOrganizationMetadataParams{
		ID:          orgID,
		Name:        "Ingress Credentials Test Org",
		Slug:        "ingress-credentials-" + uuid.NewString(),
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
		credentials: ingresscredentials.New(testenv.NewLogger(t), cipher),
	}
}

// createIngress creates an ingress for the test organization with no
// credentials.
func (ti *testInstance) createIngress(t *testing.T) repo.NetworkIngress {
	t.Helper()

	id := uuid.New()
	names, err := k8s.NewNetworkIngressResourceNames(id)
	require.NoError(t, err)
	resources, err := names.Marshal()
	require.NoError(t, err)

	row, err := repo.New(ti.conn).CreateNetworkIngress(t.Context(), repo.CreateNetworkIngressParams{
		ID: id, OrganizationID: ti.orgID, Provider: "tailscale", Hostname: "private-" + uuid.NewString()[:8],
		EndpointNamespaceKind: "platform", CustomDomainID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Enabled: true, IdentityRequired: false,
		AttestorNamespace: names.Namespace, AttestorServiceAccount: names.AttestorServiceAccount, ProviderResources: resources,
		Runtime: pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)

	return row
}

// stored reads the ingress row back from the database.
func (ti *testInstance) stored(t *testing.T, id uuid.UUID) repo.NetworkIngress {
	t.Helper()

	row, err := repo.New(ti.conn).GetNetworkIngressByID(t.Context(), repo.GetNetworkIngressByIDParams{ID: id, OrganizationID: ti.orgID})
	require.NoError(t, err)

	return row
}

// enroll moves the test organization's credentials to dual_write.
func (ti *testInstance) enroll(t *testing.T) {
	t.Helper()

	_, err := ti.transitions.Enroll(t.Context(), ti.orgID, ingresscredentials.UseCase, orgencryption.SystemActor())
	require.NoError(t, err)
}

// finalize backfills, verifies and finalizes the test organization's
// credentials, then clears their platform-key copies.
func (ti *testInstance) finalize(t *testing.T) {
	t.Helper()

	stores := []orgencryption.CopyStore{ingresscredentials.NewCopyStore(ti.conn)}

	_, err := orgencryption.NewRunner(testenv.NewLogger(t), ti.conn, ti.cipher, testenv.NewMeterProvider(t)).Run(t.Context(), ti.orgID, ingresscredentials.UseCase, stores)
	require.NoError(t, err)

	err = orgencryptionrepo.New(ti.conn).SetEncryptionTransitionStateChangedAt(t.Context(), orgencryptionrepo.SetEncryptionTransitionStateChangedAtParams{
		StateChangedAt: pgtype.Timestamptz{Time: time.Now().Add(-time.Hour), InfinityModifier: pgtype.Finite, Valid: true},
		OrganizationID: ti.orgID,
		UseCase:        string(ingresscredentials.UseCase),
	})
	require.NoError(t, err)

	_, err = ti.transitions.Finalize(t.Context(), ti.orgID, ingresscredentials.UseCase, orgencryption.FinalizeRequest{MinimumDualWrite: time.Minute, Stores: stores}, orgencryption.SystemActor())
	require.NoError(t, err)

	_, err = ti.transitions.ClearPlatformCopies(t.Context(), ti.orgID, ingresscredentials.UseCase, stores, orgencryption.SystemActor())
	require.NoError(t, err)
}
