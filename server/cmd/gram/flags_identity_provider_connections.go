package gram

import "github.com/urfave/cli/v2"

const (
	identityProviderKMSKeyRingFlag          = "identity-provider-kms-key-ring"
	identityProviderSigningCredentialIDFlag = "identity-provider-signing-credential-id"
	identityProviderKMSKeyRingLocalDefault  = "projects/gram-local/locations/global/keyRings/identity-provider-connections"
)

func identityProviderConnectionFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:    identityProviderKMSKeyRingFlag,
			Usage:   "GCP KMS key ring identity provider connections mint signing keys in.",
			EnvVars: []string{"GRAM_IDENTITY_PROVIDER_KMS_KEY_RING"},
		},
		&cli.StringFlag{
			Name:    identityProviderSigningCredentialIDFlag,
			Usage:   "ID of the platform-tier GCP IAM credential that signs identity provider client assertions.",
			EnvVars: []string{"GRAM_IDENTITY_PROVIDER_SIGNING_CREDENTIAL_ID"},
		},
	}
}
