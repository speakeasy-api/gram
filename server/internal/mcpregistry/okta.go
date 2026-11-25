package mcpregistry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/mcpregistry/repo"
	"github.com/speakeasy-api/gram/server/internal/oktaissuer"
)

// OktaNamespace holds the staff-curated mapping from Okta Integration Network
// (OIN) applications to a catalog entry. Okta exposes nothing linking an app to
// an MCP server, so this metadata is the only source for suggestions and
// Cross App Access prefill.
const OktaNamespace = "com.speakeasy.ai/okta"

const oktaNamespacePath = "/_meta/com.speakeasy.ai~1okta"

// ErrNoOktaMapping reports a record without the namespace.
var ErrNoOktaMapping = errors.New("registry record has no okta mapping")

// OktaMapping is the decoded namespace. Field rules live in the schema overlay
// and validateOktaMapping; decoding a stored record never applies them, so
// consumers filter on published and ValidateStored before trusting a mapping.
type OktaMapping struct {
	OINNames         []string `json:"oinNames"`
	OINIntegrationID string   `json:"oinIntegrationId,omitempty"`
	XAASignOnModes   []string `json:"xaaSignOnModes,omitempty"`
	XAAIssuer        string   `json:"xaaIssuer,omitempty"`
}

// ParseOktaMapping decodes the namespace. Keys are matched exactly, like the
// SQL conflict scan, so a case-variant key never reads as a claim; any other
// key inside the namespace is an error.
func ParseOktaMapping(data json.RawMessage) (OktaMapping, error) {
	var mapping OktaMapping
	var root, meta map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		return mapping, fmt.Errorf("decode registry record: %w", err)
	}
	if raw, ok := root["_meta"]; ok {
		if err := json.Unmarshal(raw, &meta); err != nil {
			return mapping, fmt.Errorf("decode registry metadata: %w", err)
		}
	}
	raw, ok := meta[OktaNamespace]
	if !ok {
		return mapping, ErrNoOktaMapping
	}
	// encoding/json folds case on struct fields, so keys are checked first.
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return mapping, fmt.Errorf("decode registry okta mapping: %w", err)
	}
	if fields == nil {
		return mapping, errors.New("decode registry okta mapping: namespace must be an object")
	}
	for key := range fields {
		switch key {
		case "oinNames", "oinIntegrationId", "xaaSignOnModes", "xaaIssuer":
		default:
			return mapping, errors.New("decode registry okta mapping: unknown field")
		}
	}
	if err := json.Unmarshal(raw, &mapping); err != nil {
		return OktaMapping{}, fmt.Errorf("decode registry okta mapping: %w", err)
	}
	return mapping, nil
}

// validateOktaMapping applies the checks the schema cannot express.
func validateOktaMapping(meta map[string]any) []Issue {
	mapping, ok := meta[OktaNamespace].(map[string]any)
	if !ok {
		return nil
	}
	if names, ok := mapping["oinNames"].([]any); ok {
		for i, name := range names {
			if s, ok := name.(string); ok && oktaissuer.HasInvisible(s) {
				return []Issue{{Path: oktaNamespacePath + "/oinNames/" + strconv.Itoa(i), Message: "whitespace and control characters are not allowed"}}
			}
		}
	}
	if issuer, ok := mapping["xaaIssuer"].(string); ok {
		if err := oktaissuer.Validate(issuer); err != nil {
			return []Issue{{Path: oktaNamespacePath + "/xaaIssuer", Message: err.Error()}}
		}
	}
	return nil
}

// checkOktaMappingConflicts enforces OIN-key uniqueness across the catalog,
// published or not. The caller holds a transaction; the advisory lock
// serializes writers because no index can enforce element-wise uniqueness on
// a jsonb array. Never take the lock before LockEntry, or Save could deadlock.
func checkOktaMappingConflicts(ctx context.Context, q *repo.Queries, id uuid.UUID, data json.RawMessage) error {
	mapping, err := ParseOktaMapping(data)
	if errors.Is(err, ErrNoOktaMapping) || (err == nil && len(mapping.OINNames) == 0) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := q.LockOktaMappings(ctx); err != nil {
		return fmt.Errorf("lock registry okta mappings: %w", err)
	}
	taken, err := q.ListOktaMappingConflicts(ctx, repo.ListOktaMappingConflictsParams{ID: id, Names: mapping.OINNames})
	if err != nil {
		return fmt.Errorf("list registry okta mapping conflicts: %w", err)
	}
	if len(taken) == 0 {
		return nil
	}
	holders := make(map[string]string, len(taken))
	for _, row := range taken {
		holders[row.OinName] = row.EntryName
	}
	issues := make([]Issue, 0, len(taken))
	for i, name := range mapping.OINNames {
		if holder, ok := holders[name]; ok {
			// Entry names are catalog identifiers staff can search for, never
			// the rejected input.
			issues = append(issues, Issue{Path: oktaNamespacePath + "/oinNames/" + strconv.Itoa(i), Message: "OIN name is already mapped by entry " + holder})
		}
	}
	return &InvalidError{Issues: issues}
}
