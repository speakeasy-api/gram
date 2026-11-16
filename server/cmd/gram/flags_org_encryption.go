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
	orgEncryptionDisabledFlag           = "org-encryption-disabled"
	orgEncryptionKMSKeyRingLocalDefault = "projects/gram-local/locations/global/keyRings/org-encryption"
)

func orgEncryptionFlags() []cli.Flag {
	return []cli.Flag{
		&cli.BoolFlag{
			Name:    orgEncryptionDisabledFlag,
			Usage:   "Run without organization encryption in an environment that otherwise requires it (every environment except local and test). Preview deployments set this because they share dev's KMS keys but not its database. Conflicts with a configured platform credential.",
			EnvVars: []string{"GRAM_ORG_ENCRYPTION_DISABLED"},
		},
		&cli.StringFlag{
			Name:    orgEncryptionKMSKeyRingFlag,
			Usage:   "GCP KMS key ring Speakeasy-managed organization encryption keys are minted in.",
			EnvVars: []string{"GRAM_ORG_ENCRYPTION_KMS_KEY_RING"},
		},
		&cli.StringFlag{
			Name:    orgEncryptionCredentialIDFlag,
			Usage:   "ID of the platform-tier GCP IAM credential that reaches Speakeasy-managed organization encryption keys. Without it, organization encryption is off in local and test environments, and every other environment refuses to start unless organization encryption is disabled.",
			EnvVars: []string{"GRAM_ORG_ENCRYPTION_CREDENTIAL_ID"},
		},
		&cli.StringFlag{
			Name:    orgEncryptionServiceAccountFlag,
			Usage:   "Service account the organization encryption credential must impersonate; managed keys grant it and customers grant it on their own keys.",
			EnvVars: []string{"GRAM_ORG_ENCRYPTION_SERVICE_ACCOUNT"},
		},
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
	}
}
