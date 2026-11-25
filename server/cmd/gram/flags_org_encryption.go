package gram

import (
	"github.com/urfave/cli/v2"

	"github.com/speakeasy-api/gram/server/internal/orgencryption"
)

const (
	chatTranscriptShadowProjectIDsFlag  = "chat-transcript-shadow-project-ids"
	orgEncryptionKMSKeyRingFlag         = "org-encryption-kms-key-ring"
	orgEncryptionCredentialIDFlag       = "org-encryption-credential-id"
	orgEncryptionServiceAccountFlag     = "org-encryption-service-account"
	orgEncryptionSwitchGracePeriodFlag  = "org-encryption-switch-grace-period"
	orgEncryptionManagedKeyRetention    = "org-encryption-managed-key-retention"
	orgEncryptionDisabledFlag           = "org-encryption-disabled"
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
	}
}

// transcriptShadowReadOnlyFlag tells a command that reads chat messages without
// the shadow store which projects keep shadows, so its plaintext reads for them
// are reported.
func transcriptShadowReadOnlyFlag() cli.Flag {
	return &cli.StringSliceFlag{
		Name:    chatTranscriptShadowProjectIDsFlag,
		Usage:   "Project UUIDs that keep encrypted chat transcript shadows. This command reads chat messages but never writes them. With organization encryption configured it opens the shadows; without it, its reads for these projects are served from plaintext, logged as errors and counted as chat.transcript_shadow.plaintext_reads, and fail in local development.",
		EnvVars: []string{"GRAM_CHAT_TRANSCRIPT_SHADOW_PROJECT_IDS"},
	}
}

// transcriptShadowFlag enables shadow writes only on commands that configure them.
func transcriptShadowFlag() cli.Flag {
	return &cli.StringSliceFlag{
		Name:    chatTranscriptShadowProjectIDsFlag,
		Usage:   "Explicit project UUIDs to write encrypted chat transcript shadow copies for, and whose chat reads open them; retains plaintext and does not backfill existing messages. Messages without a shadow are served from plaintext. A stale or unauthentic shadow fails the read, as does any other read error such as a key unwrap or database failure. Before adding back a project that was removed, recapture its messages: a message that already had a shadow and was edited while the project was off has a stale shadow. Disabled by default.",
		EnvVars: []string{"GRAM_CHAT_TRANSCRIPT_SHADOW_PROJECT_IDS"},
	}
}
