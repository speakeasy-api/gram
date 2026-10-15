package gram

import "github.com/urfave/cli/v2"

const (
	identityProviderKMSKeyRingFlag          = "identity-provider-kms-key-ring"
	identityProviderSigningCredentialIDFlag = "identity-provider-signing-credential-id"
	identityProviderSigningServiceAccount   = "identity-provider-signing-service-account"
	identityProviderKMSKeyRingLocalDefault  = "projects/gram-local/locations/global/keyRings/identity-provider-connections"
	internalSessionIssuerSigningPausedFlag  = "internal-session-issuer-signing-paused"
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
		&cli.StringFlag{
			Name:    identityProviderSigningServiceAccount,
			Usage:   "Service account the signing credential must impersonate; any other target is refused.",
			EnvVars: []string{"GRAM_IDENTITY_PROVIDER_SIGNING_SERVICE_ACCOUNT"},
		},
		internalSessionIssuerSigningPausedCLIFlag(),
	}
}

// internalSessionIssuerSigningPausedCLIFlag is the startup kill switch for
// internal session issuer signing; the PostHog flag of the same name pauses
// without a restart.
func internalSessionIssuerSigningPausedCLIFlag() cli.Flag {
	return &cli.BoolFlag{
		Name:    internalSessionIssuerSigningPausedFlag,
		Usage:   "Kill switch: refuse every internal session issuer signature. Discovery keeps serving.",
		EnvVars: []string{"GRAM_INTERNAL_SESSION_ISSUER_SIGNING_PAUSED"},
	}
}
