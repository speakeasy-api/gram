package gram

import (
	"github.com/urfave/cli/v2"

	"github.com/speakeasy-api/gram/server/internal/orgencryption"
)

const (
	orgEncryptionKMSKeyRingFlag         = "org-encryption-kms-key-ring"
	orgEncryptionCredentialIDFlag       = "org-encryption-credential-id"
	orgEncryptionServiceAccountFlag     = "org-encryption-service-account"
	orgEncryptionSwitchGracePeriodFlag  = "org-encryption-switch-grace-period"
	orgEncryptionManagedKeyRetention    = "org-encryption-managed-key-retention"
	orgEncryptionKMSKeyRingLocalDefault = "projects/gram-local/locations/global/keyRings/org-encryption"
)

// orgEncryptionFlags configures organization encryption for processes that
// provision keys as well as use them.
func orgEncryptionFlags() []cli.Flag {
	return append(orgEncryptionServiceFlags(),
		&cli.DurationFlag{
			Name:    orgEncryptionSwitchGracePeriodFlag,
			Usage:   "How long a switch to another organization encryption key keeps the previous key readable before it completes.",
			EnvVars: []string{"GRAM_ORG_ENCRYPTION_SWITCH_GRACE_PERIOD"},
			Value:   orgencryption.DefaultSwitchGracePeriod,
		},
		&cli.DurationFlag{
			Name:    orgEncryptionManagedKeyRetention,
			Usage:   "How long a Speakeasy-managed organization encryption key stays enabled after it stops protecting data, before it is disabled.",
			EnvVars: []string{"GRAM_ORG_ENCRYPTION_MANAGED_KEY_RETENTION"},
			Value:   orgencryption.DefaultManagedKeyRetention,
		},
	)
}

// orgEncryptionServiceFlags configures organization encryption for processes
// that only encrypt and decrypt, such as the tailnet gateway.
func orgEncryptionServiceFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:    orgEncryptionKMSKeyRingFlag,
			Usage:   "GCP KMS key ring Speakeasy-managed organization encryption keys are minted in.",
			EnvVars: []string{"GRAM_ORG_ENCRYPTION_KMS_KEY_RING"},
		},
		&cli.StringFlag{
			Name:    orgEncryptionCredentialIDFlag,
			Usage:   "ID of the platform-tier GCP IAM credential that reaches Speakeasy-managed organization encryption keys. Organization encryption is off without it.",
			EnvVars: []string{"GRAM_ORG_ENCRYPTION_CREDENTIAL_ID"},
		},
		&cli.StringFlag{
			Name:    orgEncryptionServiceAccountFlag,
			Usage:   "Service account the organization encryption credential must impersonate; managed keys grant it and customers grant it on their own keys.",
			EnvVars: []string{"GRAM_ORG_ENCRYPTION_SERVICE_ACCOUNT"},
		},
	}
}
