package mv

import (
	"time"

	extkeys "github.com/speakeasy-api/gram/server/gen/external_keys"
	"github.com/speakeasy-api/gram/server/internal/conv"
	repo "github.com/speakeasy-api/gram/server/internal/externalkeys/repo"
)

// BuildExternalKeySummaryView converts a supertype row into the
// provider-independent summary returned by the generic list endpoint.
func BuildExternalKeySummaryView(ek repo.ExternalKey) *extkeys.ExternalKeySummary {
	return &extkeys.ExternalKeySummary{
		ID:                     ek.ID.String(),
		OrganizationID:         ek.OrganizationID.String,
		ExternalCredentialID:   ek.ExternalCredentialID.String(),
		Provider:               ek.Provider,
		Algorithm:              ek.Algorithm,
		Name:                   ek.Name,
		CustomerGrantReference: conv.FromPGText[string](ek.CustomerGrantReference),
		CreatedAt:              ek.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:              ek.UpdatedAt.Time.Format(time.RFC3339),
	}
}

// BuildExternalKeySummaryListView converts supertype rows into summaries.
func BuildExternalKeySummaryListView(eks []repo.ExternalKey) []*extkeys.ExternalKeySummary {
	result := make([]*extkeys.ExternalKeySummary, len(eks))
	for i, ek := range eks {
		result[i] = BuildExternalKeySummaryView(ek)
	}
	return result
}

// BuildAwsKmsKeyView joins the supertype and AWS subtype rows into the full AWS
// KMS key view.
func BuildAwsKmsKeyView(ek repo.ExternalKey, aws repo.AwsKmsKey) *extkeys.AwsKmsKey {
	return &extkeys.AwsKmsKey{
		KeyArn:                 aws.KeyArn,
		ID:                     ek.ID.String(),
		OrganizationID:         ek.OrganizationID.String,
		ExternalCredentialID:   ek.ExternalCredentialID.String(),
		Provider:               ek.Provider,
		Algorithm:              ek.Algorithm,
		Name:                   ek.Name,
		CustomerGrantReference: conv.FromPGText[string](ek.CustomerGrantReference),
		CreatedAt:              ek.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:              ek.UpdatedAt.Time.Format(time.RFC3339),
	}
}

// BuildGcpKmsKeyView joins the supertype and GCP subtype rows into the full GCP
// KMS key view.
func BuildGcpKmsKeyView(ek repo.ExternalKey, gcp repo.GcpKmsKey) *extkeys.GcpKmsKey {
	return &extkeys.GcpKmsKey{
		ResourceName:           gcp.ResourceName,
		ID:                     ek.ID.String(),
		OrganizationID:         ek.OrganizationID.String,
		ExternalCredentialID:   ek.ExternalCredentialID.String(),
		Provider:               ek.Provider,
		Algorithm:              ek.Algorithm,
		Name:                   ek.Name,
		CustomerGrantReference: conv.FromPGText[string](ek.CustomerGrantReference),
		CreatedAt:              ek.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:              ek.UpdatedAt.Time.Format(time.RFC3339),
	}
}
