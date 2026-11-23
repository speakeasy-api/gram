package orgencryption_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"

	"github.com/speakeasy-api/gram/server/internal/audit"
	extcredrepo "github.com/speakeasy-api/gram/server/internal/externalcredentials/repo"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/orgencryption/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/gcp/gcpauth"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/gcp/gcpkms"
)

// mintHookKMS runs onMint after the stand-in mints a KMS key: between the
// provisioner screening the platform credential and recording the key.
type mintHookKMS struct {
	gcpkms.EncryptionProvisioningClient

	onMint func(ctx context.Context)
}

func (k *mintHookKMS) CreateEncryptionKey(ctx context.Context, params gcpkms.CreateEncryptionKeyParams) (*gcpkms.CreatedEncryptionKey, error) {
	created, err := k.EncryptionProvisioningClient.CreateEncryptionKey(ctx, params)
	if err == nil {
		k.onMint(ctx)
	}
	return created, err //nolint:wrapcheck // the hook is transparent
}

// A platform credential deleted after it was screened must not get a managed
// key recorded against it: the soft-deleted row still satisfies the foreign
// key, so only the re-read inside the write transaction catches it.
func TestProvisioner_RefusesCredentialDeletedDuringProvisioning(t *testing.T) {
	t.Parallel()
	ti := newTestInstance(t)
	keys := ti.fixture.Keys

	hooked := func(ctx context.Context, ts oauth2.TokenSource) (gcpkms.EncryptionProvisioningClient, error) {
		client, err := ti.fixture.KMS.ProvisioningClients(ctx, ts)
		if err != nil {
			return nil, err //nolint:wrapcheck // test factory
		}
		return &mintHookKMS{EncryptionProvisioningClient: client, onMint: func(ctx context.Context) {
			_, err := extcredrepo.New(ti.conn).SoftDeleteExternalCredential(ctx, extcredrepo.SoftDeleteExternalCredentialParams{
				ID:             keys.PlatformCredentialID,
				OrganizationID: pgtype.Text{String: "", Valid: false},
				Provider:       "gcp_iam",
			})
			require.NoError(t, err)
		}}, nil
	}

	provisioner, err := orgencryption.NewProvisioner(testenv.NewLogger(t), ti.conn, gcpauth.NewIdentity(gcpauth.NewStubResolver()), ti.fixture.KMS.Clients, hooked, audit.NewLogger(), ti.fixture.Service, orgencryption.ProvisionerConfig{
		Keys:                keys,
		SwitchGracePeriod:   0,
		ManagedKeyRetention: 0,
	})
	require.NoError(t, err)

	err = provisioner.EnsureUseCase(t.Context(), ti.orgID, useCase, orgencryption.SystemActor())
	require.ErrorIs(t, err, orgencryption.ErrPlatformCredentialUnusable)

	_, err = repo.New(ti.conn).GetOrganizationEncryptionConfig(t.Context(), ti.orgID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
}
