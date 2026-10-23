package admission

import "github.com/speakeasy-api/gram/server/internal/aivendors"

// Preset is a live catalog document and its owning product.
type Preset = aivendors.Preset

// CatalogMatch reports whether an enabled exact URL or pattern admits a client ID.
func CatalogMatch(catalog *aivendors.Snapshot, clientID string) (AdmitReason, bool) {
	preset, ok := catalog.CatalogPreset(clientID)
	if !ok {
		return "", false
	}
	if preset.IsPattern() {
		return AdmitCatalogPattern, true
	}
	return AdmitCatalogExact, true
}
