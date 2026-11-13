package gram

import (
	"flag"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"

	"github.com/speakeasy-api/gram/server/internal/testenv"
)

const (
	testOrgEncryptionCredentialID   = "018f2c6e-0000-7000-8000-000000000001"
	testOrgEncryptionKeyRing        = "projects/gram-test/locations/us-west1/keyRings/gram-test-org-encryption"
	testOrgEncryptionServiceAccount = "gram-test-org-encryption@gram-test.iam.gserviceaccount.com"
)

func TestNewOrgEncryptionOffWithoutBuilding(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		values map[string]string
	}{
		{name: "local without credential", values: map[string]string{"environment": "local"}},
		{name: "test without credential", values: map[string]string{"environment": "test"}},
		{name: "dev disabled without credential", values: map[string]string{"environment": "dev", orgEncryptionDisabledFlag: "true"}},
		{name: "prod disabled without credential", values: map[string]string{"environment": "prod", orgEncryptionDisabledFlag: "true"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			deps, err := newOrgEncryption(t.Context(), testenv.NewLogger(t), newOrgEncryptionCLIContext(t, tc.values), nil, nil, nil)
			require.NoError(t, err)
			require.Nil(t, deps.Service)
			require.Nil(t, deps.Provisioner)
		})
	}
}

func TestNewOrgEncryptionRejectsConfiguration(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		values  map[string]string
		wantErr string
	}{
		{
			name:    "dev without credential",
			values:  map[string]string{"environment": "dev"},
			wantErr: `organization encryption is required in the "dev" environment`,
		},
		{
			name:    "prod without credential",
			values:  map[string]string{"environment": "prod"},
			wantErr: `organization encryption is required in the "prod" environment`,
		},
		{
			name:    "unlisted environment without credential",
			values:  map[string]string{"environment": "staging"},
			wantErr: `organization encryption is required in the "staging" environment`,
		},
		{
			name:    "dev with whitespace credential",
			values:  map[string]string{"environment": "dev", orgEncryptionCredentialIDFlag: "  "},
			wantErr: `organization encryption is required in the "dev" environment`,
		},
		{
			name: "local disabled with credential",
			values: map[string]string{
				"environment":                 "local",
				orgEncryptionDisabledFlag:     "true",
				orgEncryptionCredentialIDFlag: testOrgEncryptionCredentialID,
			},
			wantErr: "organization encryption is disabled but a platform credential is configured",
		},
		{
			name: "disabled with credential",
			values: map[string]string{
				"environment":                 "prod",
				orgEncryptionDisabledFlag:     "true",
				orgEncryptionCredentialIDFlag: testOrgEncryptionCredentialID,
			},
			wantErr: "organization encryption is disabled but a platform credential is configured",
		},
		{
			name: "malformed credential id",
			values: map[string]string{
				"environment":                   "prod",
				orgEncryptionCredentialIDFlag:   "not-a-uuid",
				orgEncryptionKMSKeyRingFlag:     testOrgEncryptionKeyRing,
				orgEncryptionServiceAccountFlag: testOrgEncryptionServiceAccount,
			},
			wantErr: "parse organization encryption credential id",
		},
		{
			name: "missing key ring",
			values: map[string]string{
				"environment":                   "prod",
				orgEncryptionCredentialIDFlag:   testOrgEncryptionCredentialID,
				orgEncryptionServiceAccountFlag: testOrgEncryptionServiceAccount,
			},
			wantErr: "organization encryption kms key ring is required",
		},
		{
			name: "missing service account",
			values: map[string]string{
				"environment":                 "prod",
				orgEncryptionCredentialIDFlag: testOrgEncryptionCredentialID,
				orgEncryptionKMSKeyRingFlag:   testOrgEncryptionKeyRing,
			},
			wantErr: "organization encryption platform service account is required",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := newOrgEncryption(t.Context(), testenv.NewLogger(t), newOrgEncryptionCLIContext(t, tc.values), nil, nil, nil)
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}

func TestNewOrgEncryptionBuildsServiceAndProvisioner(t *testing.T) {
	t.Parallel()

	deps, err := newOrgEncryption(t.Context(), testenv.NewLogger(t), newOrgEncryptionCLIContext(t, map[string]string{
		"environment":                   "prod",
		orgEncryptionCredentialIDFlag:   testOrgEncryptionCredentialID,
		orgEncryptionKMSKeyRingFlag:     testOrgEncryptionKeyRing,
		orgEncryptionServiceAccountFlag: testOrgEncryptionServiceAccount,
	}), nil, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, deps.Service)
	require.NotNil(t, deps.Provisioner)
}

// TestOrgEncryptionFlagsOnOAuthCredentialProcesses guards the wiring: each
// process that reads or writes OAuth credentials calls newOrgEncryption, which
// reads these flags, so dropping them from a command would turn its startup
// check off.
func TestOrgEncryptionFlagsOnOAuthCredentialProcesses(t *testing.T) {
	t.Parallel()

	commands := map[string]*cli.Command{
		"admin":                  newAdminCommand(),
		"mcp":                    newMCPCommand(),
		"network-ingress-server": newNetworkIngressServerCommand(),
		"server":                 newStartCommand(),
		"worker":                 newWorkerCommand(),
	}

	for process, command := range commands {
		t.Run(process, func(t *testing.T) {
			t.Parallel()

			for _, item := range orgEncryptionFlags() {
				requireFlag(t, command.Flags, item.Names()[0])
			}
		})
	}
}

func newOrgEncryptionCLIContext(t *testing.T, values map[string]string) *cli.Context {
	t.Helper()

	set := flag.NewFlagSet("test", flag.ContinueOnError)
	require.NoError(t, (&cli.StringFlag{Name: "environment"}).Apply(set))
	for _, item := range orgEncryptionFlags() {
		require.NoError(t, item.Apply(set))
	}
	for key, value := range values {
		require.NoError(t, set.Set(key, value))
	}

	return cli.NewContext(cli.NewApp(), set, nil)
}
