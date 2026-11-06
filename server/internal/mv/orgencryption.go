package mv

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	gen "github.com/speakeasy-api/gram/server/gen/org_encryption"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/orgencryption/repo"
)

// BuildOrgEncryptionConfigView renders an organization's encryption
// configuration. It names keys by id only.
func BuildOrgEncryptionConfigView(config repo.OrganizationEncryptionConfig) *gen.OrgEncryptionConfig {
	var previous *string
	if config.PreviousKekExternalKeyID.Valid {
		previous = new(config.PreviousKekExternalKeyID.UUID.String())
	}

	return &gen.OrgEncryptionConfig{
		ID:                config.ID.String(),
		KeyMode:           config.Mode,
		ConfigState:       config.State,
		KeyID:             config.KekExternalKeyID.String(),
		PreviousKeyID:     previous,
		SwitchStartedAt:   timestampView(config.SwitchStartedAt),
		SwitchCompletedAt: timestampView(config.SwitchCompletedAt),
		SuspendedAt:       timestampView(config.SuspendedAt),
		SuspendedReason:   conv.FromPGText[string](config.SuspendedReason),
		LastCheckAt:       timestampView(config.LastKekCheckAt),
		LastCheckError:    conv.FromPGText[string](config.LastKekCheckError),
	}
}

// BuildOrgEncryptionKeyView renders one encryption key. A managed key hides
// the platform credential it is reached with.
func BuildOrgEncryptionKeyView(key repo.ExternalKey, resourceName string, managed bool) *gen.OrgEncryptionKey {
	var credentialID *string
	if !managed {
		credentialID = new(key.ExternalCredentialID.String())
	}

	return &gen.OrgEncryptionKey{
		ID:                     key.ID.String(),
		Name:                   key.Name,
		Provider:               key.Provider,
		ResourceName:           resourceName,
		Managed:                managed,
		ExternalCredentialID:   credentialID,
		CustomerGrantReference: conv.FromPGText[string](key.CustomerGrantReference),
		CreatedAt:              key.CreatedAt.Time.Format(time.RFC3339),
	}
}

func timestampView(ts pgtype.Timestamptz) *string {
	if !ts.Valid {
		return nil
	}

	return new(ts.Time.Format(time.RFC3339))
}
