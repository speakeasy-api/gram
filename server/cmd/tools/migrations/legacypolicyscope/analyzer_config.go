package legacypolicyscope

import (
	"encoding/json"
	"fmt"

	ra "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
)

// detectionScopesKey is the analyzer_config member this migration rewrites. It
// must match the json tag on risk_analysis.AnalyzerConfig.DetectionScopes.
const detectionScopesKey = "detection_scopes"

// withDetectionScopes returns base with detection_scopes replaced by scopes,
// or removed when scopes is empty, leaving every other member of the object
// byte-identical.
//
// ra.WithDetectionScopes round-trips through the typed AnalyzerConfig struct
// and so silently drops members it does not know about. That is acceptable for
// a policy edit made by an operator who can see the result; a bulk migration
// writing every policy row in the fleet would destroy an option written by a
// newer server than the one this tool was built from.
func withDetectionScopes(base []byte, scopes []ra.DetectionScopeConfig) ([]byte, error) {
	config := map[string]json.RawMessage{}
	if len(base) > 0 {
		if err := json.Unmarshal(base, &config); err != nil {
			// Refuse rather than write a config reconstructed from a blob we
			// could not read: the unreadable members would be lost.
			return nil, fmt.Errorf("decode analyzer config: %w", err)
		}
	}
	if config == nil {
		// A stored JSON null decodes into a nil map.
		config = map[string]json.RawMessage{}
	}

	if len(scopes) == 0 {
		delete(config, detectionScopesKey)
	} else {
		encoded, err := json.Marshal(scopes)
		if err != nil {
			return nil, fmt.Errorf("encode detection scopes: %w", err)
		}
		config[detectionScopesKey] = encoded
	}

	out, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("marshal analyzer config: %w", err)
	}
	return out, nil
}
