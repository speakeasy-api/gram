package mv

import (
	"encoding/json"
	"time"

	gen "github.com/speakeasy-api/gram/server/gen/platform_ai_scan_targets"
	"github.com/speakeasy-api/gram/server/internal/agent/aitargets"
	"github.com/speakeasy-api/gram/server/internal/agent/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
)

// BuildAiScanTargetView converts one catalog record into the API type.
func BuildAiScanTargetView(record aitargets.Record) *gen.AiScanTarget {
	var plistKey *string
	if record.VersionHint != nil {
		plistKey = conv.PtrEmpty(record.VersionHint.PlistKey)
	}
	return &gen.AiScanTarget{
		ID:          record.ID,
		DisplayName: record.DisplayName,
		Category:    string(record.Category),
		Signatures: &gen.AiScanTargetSignatures{
			BundleIds:    emptyIfNil(record.Signatures.BundleIDs),
			Binaries:     emptyIfNil(record.Signatures.Binaries),
			ConfigDirs:   emptyIfNil(record.Signatures.ConfigDirs),
			ProcessNames: emptyIfNil(record.Signatures.ProcessNames),
		},
		VersionPlistKey: plistKey,
		Enabled:         record.Enabled,
		CreatedAt:       record.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:       record.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

// BuildAiScanTargetListView converts catalog records into the list type.
func BuildAiScanTargetListView(listVersion int32, etag string, records []aitargets.Record) *gen.ListAiScanTargetsResult {
	targets := make([]*gen.AiScanTarget, 0, len(records))
	for _, record := range records {
		targets = append(targets, BuildAiScanTargetView(record))
	}
	return &gen.ListAiScanTargetsResult{
		ListVersion: int(listVersion),
		Etag:        etag,
		Targets:     targets,
	}
}

// BuildAiScanCatalogRevisionView converts one revision row into the API
// type; an undecodable snapshot is omitted rather than failing the listing.
func BuildAiScanCatalogRevisionView(row repo.AiScanCatalogRevision) *gen.AiScanCatalogRevision {
	return &gen.AiScanCatalogRevision{
		Revision:     int(row.Revision),
		TargetID:     row.TargetID,
		Action:       row.Action,
		ActorUserID:  conv.PtrEmpty(row.ActorUserID.String),
		ActorEmail:   conv.PtrEmpty(row.ActorEmail.String),
		Reason:       conv.PtrEmpty(row.Reason.String),
		TargetBefore: decodeJSONValue(row.TargetBefore),
		TargetAfter:  decodeJSONValue(row.TargetAfter),
		CreatedAt:    row.CreatedAt.Time.UTC().Format(time.RFC3339),
	}
}

func decodeJSONValue(data []byte) any {
	if len(data) == 0 {
		return nil
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return nil
	}
	return value
}

// emptyIfNil keeps signature arrays present in JSON output.
func emptyIfNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
