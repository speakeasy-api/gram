package mv

import (
	"time"

	gen "github.com/speakeasy-api/gram/server/gen/agent"
	"github.com/speakeasy-api/gram/server/internal/agent/aitargets"
	"github.com/speakeasy-api/gram/server/internal/conv"
)

// BuildAiScanTargetView converts one entry of an organization's list into the
// API type.
func BuildAiScanTargetView(entry aitargets.Entry) *gen.AiScanTarget {
	var plistKey *string
	if entry.VersionHint != nil {
		plistKey = conv.PtrEmpty(entry.VersionHint.PlistKey)
	}
	return &gen.AiScanTarget{
		ID:          entry.ID,
		DisplayName: entry.DisplayName,
		Category:    string(entry.Category),
		Signatures: &gen.AiScanTargetSignatures{
			BundleIds:    emptyIfNil(entry.Signatures.BundleIDs),
			Binaries:     emptyIfNil(entry.Signatures.Binaries),
			ConfigDirs:   emptyIfNil(entry.Signatures.ConfigDirs),
			ProcessNames: emptyIfNil(entry.Signatures.ProcessNames),
		},
		VersionPlistKey: plistKey,
		GatewayClient: &gen.AiScanTargetGatewayClient{
			CimdVendorKeys:  emptyIfNil(entry.GatewayClient.CIMDVendorKeys),
			OauthClientIds:  emptyIfNil(entry.GatewayClient.OAuthClientIDs),
			ClientInfoNames: emptyIfNil(entry.GatewayClient.ClientInfoNames),
		},
		Enabled:    entry.Enabled,
		Origin:     string(entry.Source),
		Customized: entry.Customized,
		CreatedAt:  formatOptionalTime(entry.CreatedAt),
		UpdatedAt:  formatOptionalTime(entry.UpdatedAt),
	}
}

// BuildAiScanTargetListView converts an organization's list into the API
// type.
func BuildAiScanTargetListView(list *aitargets.OrganizationList) *gen.ListAiScanTargetsResult {
	targets := make([]*gen.AiScanTarget, 0, len(list.Entries))
	for _, entry := range list.Entries {
		targets = append(targets, BuildAiScanTargetView(entry))
	}
	return &gen.ListAiScanTargetsResult{
		ListVersion: int(list.Snapshot.ListVersion),
		Etag:        list.Snapshot.ETag,
		Targets:     targets,
	}
}

// formatOptionalTime renders a timestamp, or nothing for the zero value.
func formatOptionalTime(value time.Time) *string {
	if value.IsZero() {
		return nil
	}
	formatted := value.UTC().Format(time.RFC3339)
	return &formatted
}

// emptyIfNil keeps signature arrays present in JSON output.
func emptyIfNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
