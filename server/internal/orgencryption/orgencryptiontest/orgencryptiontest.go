// Package orgencryptiontest builds organization encryption against the local
// KMS stand-in, for tests in this package and in consumers.
package orgencryptiontest

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	extcredrepo "github.com/speakeasy-api/gram/server/internal/externalcredentials/repo"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/orgencryption/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/gcp/gcpauth"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/gcp/gcpkms"
)

// KeyRing is the Speakeasy-owned ring fixture managed KEKs are minted in.
const KeyRing = "projects/gram-stub/locations/global/keyRings/org-encryption"

// CustomerKeyRing is the ring fixture customer KEKs live in.
const CustomerKeyRing = "projects/customer-stub/locations/global/keyRings/customer"

// PlatformServiceAccount is the platform credential's target, in the stub
// resolver's own project so screening accepts it.
func PlatformServiceAccount() string {
	_, domain, _ := strings.Cut(gcpauth.StubResolverPrincipal, "@")
	return "org-encryption@" + domain
}

// KMS is the local stand-in every fixture client shares, with a count of
// decrypt calls so tests can prove the DEK cache works.
type KMS struct {
	*gcpkms.LocalEncryptionClient

	mu       sync.Mutex
	decrypts int
}

// Decrypts counts Decrypt calls made through the fixture.
func (k *KMS) Decrypts() int {
	k.mu.Lock()
	defer k.mu.Unlock()

	return k.decrypts
}

// Decrypt counts the call and delegates to the stand-in.
func (k *KMS) Decrypt(ctx context.Context, keyName string, ciphertext, aad []byte) ([]byte, error) {
	k.mu.Lock()
	k.decrypts++
	k.mu.Unlock()

	return k.LocalEncryptionClient.Decrypt(ctx, keyName, ciphertext, aad) //nolint:wrapcheck // the fixture is transparent
}

// Clients is the EncryptionClientFactory over the shared stand-in.
func (k *KMS) Clients(_ context.Context, _ oauth2.TokenSource) (gcpkms.EncryptionClient, error) {
	return k, nil
}

// ProvisioningClients is the EncryptionProvisioningClientFactory over the
// shared stand-in.
func (k *KMS) ProvisioningClients(_ context.Context, _ oauth2.TokenSource) (gcpkms.EncryptionProvisioningClient, error) {
	return k, nil
}

// Fixture is organization encryption wired over one test database.
type Fixture struct {
	// Service encrypts and decrypts.
	Service *orgencryption.Service

	// Provisioner manages keys; it drops Service's caches after each change.
	Provisioner *orgencryption.Provisioner

	// KMS is the local stand-in behind both.
	KMS *KMS

	// Keys is the configuration both were built with.
	Keys orgencryption.KeyConfig
}

// New wires a Service and Provisioner over conn, creating the platform
// credential they depend on.
func New(t *testing.T, conn *pgxpool.Pool) *Fixture {
	t.Helper()

	return NewWithConfig(t, conn, orgencryption.ProvisionerConfig{
		Keys:              orgencryption.KeyConfig{KeyRing: "", PlatformCredentialID: uuid.Nil, PlatformServiceAccount: ""},
		SwitchGracePeriod: 0,
	})
}

// NewWithConfig is New with provisioner settings; Keys is filled in.
func NewWithConfig(t *testing.T, conn *pgxpool.Pool, cfg orgencryption.ProvisionerConfig) *Fixture {
	t.Helper()

	local, err := gcpkms.NewLocalEncryptionClient()
	require.NoError(t, err)
	kms := &KMS{LocalEncryptionClient: local, mu: sync.Mutex{}, decrypts: 0}

	cfg.Keys = orgencryption.KeyConfig{
		KeyRing:                KeyRing,
		PlatformCredentialID:   CreatePlatformCredential(t, conn),
		PlatformServiceAccount: PlatformServiceAccount(),
	}

	identity := gcpauth.NewIdentity(gcpauth.NewStubResolver())

	service, err := orgencryption.NewService(testenv.NewLogger(t), conn, identity, kms.Clients, cfg.Keys)
	require.NoError(t, err)

	provisioner, err := orgencryption.NewProvisioner(testenv.NewLogger(t), conn, identity, kms.Clients, kms.ProvisioningClients, audit.NewLogger(), service, cfg)
	require.NoError(t, err)

	return &Fixture{Service: service, Provisioner: provisioner, KMS: kms, Keys: cfg.Keys}
}

// CreatePlatformCredential writes the platform-tier gcp_iam credential managed
// KEKs are reached with and returns its id.
func CreatePlatformCredential(t *testing.T, conn *pgxpool.Pool) uuid.UUID {
	t.Helper()

	return createGcpIamCredential(t, conn, pgtype.Text{String: "", Valid: false}, PlatformServiceAccount())
}

// CreateCustomerKey writes a customer-managed KEK for the organization, with a
// credential of its own, and returns the external key id.
func CreateCustomerKey(t *testing.T, conn *pgxpool.Pool, organizationID string, keyID string) uuid.UUID {
	t.Helper()

	_, domain, _ := strings.Cut(gcpauth.StubResolverPrincipal, "@")
	credentialID := createGcpIamCredential(t, conn, conv.ToPGText(organizationID), "customer-kek@"+domain)

	q := repo.New(conn)
	key, err := q.CreateEncryptionExternalKey(t.Context(), repo.CreateEncryptionExternalKeyParams{
		OrganizationID:       conv.ToPGText(organizationID),
		ExternalCredentialID: credentialID,
		Algorithm:            gcpkms.SymmetricAlgorithm,
		Name:                 "customer key " + keyID,
	})
	require.NoError(t, err)

	require.NoError(t, q.CreateEncryptionGcpKmsKey(t.Context(), repo.CreateEncryptionGcpKmsKeyParams{
		ExternalKeyID: key.ID,
		ResourceName:  CustomerKeyName(keyID),
	}))

	return key.ID
}

// CustomerKeyName is the KMS resource name CreateCustomerKey records.
func CustomerKeyName(keyID string) string {
	return CustomerKeyRing + "/cryptoKeys/" + keyID
}

func createGcpIamCredential(t *testing.T, conn *pgxpool.Pool, organizationID pgtype.Text, serviceAccount string) uuid.UUID {
	t.Helper()

	q := extcredrepo.New(conn)
	ec, err := q.CreateExternalCredential(t.Context(), extcredrepo.CreateExternalCredentialParams{
		OrganizationID: organizationID,
		Provider:       "gcp_iam",
		Name:           "org-encryption-" + uuid.NewString(),
	})
	require.NoError(t, err)

	_, err = q.CreateGcpIamCredential(t.Context(), extcredrepo.CreateGcpIamCredentialParams{
		ExternalCredentialID:      ec.ID,
		ImpersonateServiceAccount: conv.ToPGText(serviceAccount),
		WifPoolID:                 pgtype.Text{String: "", Valid: false},
		WifProviderID:             pgtype.Text{String: "", Valid: false},
		WifProjectNumber:          pgtype.Text{String: "", Valid: false},
		SkipProjectVerification:   true,
	})
	require.NoError(t, err)

	return ec.ID
}
