//nolint:exhaustruct // MCP SDK manifests intentionally use documented optional defaults.
package adminmcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	adminservice "github.com/speakeasy-api/gram/server/internal/admin"
)

const (
	maxSupportMatrixChanges = 25
	maxSupportMatrixNote    = 512
)

// SupportMatrixInput is a bounded set of changes to existing matrix entries.
type SupportMatrixInput struct {
	Revision string                `json:"revision" jsonschema:"Exact current revision from get_support_matrix_entry; any later matrix change refuses this write"`
	Changes  []SupportMatrixChange `json:"changes" jsonschema:"One to 25 exact existing mapping or reference entries to update"`
}

type SupportMatrixChange struct {
	Kind          string                    `json:"kind" jsonschema:"mapping or reference"`
	MethodID      string                    `json:"method_id" jsonschema:"Exact stable method ID from get_support_matrix"`
	ProductID     string                    `json:"product_id,omitempty" jsonschema:"Exact stable product ID for mappings; omitted for references"`
	Applicability *string                   `json:"applicability,omitempty" jsonschema:"Mapping applicability: unknown, applicable, or na"`
	Conditions    *string                   `json:"conditions,omitempty" jsonschema:"Mapping conditions, at most 512 UTF-8 bytes"`
	Facts         []SupportMatrixFactChange `json:"facts,omitempty" jsonschema:"Existing capabilities only; each supplied field replaces that field"`
}

type SupportMatrixFactChange struct {
	CapabilityID string  `json:"capability_id" jsonschema:"Exact stable capability ID from get_support_matrix"`
	Status       *string `json:"status,omitempty" jsonschema:"Coverage status: supported, partial, unimplemented, impossible, na, or unknown"`
	Note         *string `json:"note,omitempty" jsonschema:"Operator note, at most 512 UTF-8 bytes"`
	Verify       *bool   `json:"verify,omitempty" jsonschema:"Whether this fact needs verification"`
}

type supportMatrixChangeDiff struct {
	Kind      string          `json:"kind"`
	MethodID  string          `json:"method_id"`
	ProductID string          `json:"product_id,omitempty"`
	Before    json.RawMessage `json:"before"`
	After     json.RawMessage `json:"after"`
}

type supportMatrixReceipt struct {
	RevisionBefore string                    `json:"revision_before"`
	Revision       string                    `json:"revision"`
	Changes        []supportMatrixChangeDiff `json:"changes"`
}

// supportMatrixWriter applies one platform-global support matrix change.
type supportMatrixWriter struct {
	runner *writeRunner
	writes WriteConfig
}

// validSupportMatrixRevision matches the lowercase hex SHA-256 that reads return.
func validSupportMatrixRevision(revision string) bool {
	if len(revision) != sha256.Size*2 || strings.ToLower(revision) != revision {
		return false
	}
	_, err := hex.DecodeString(revision)
	return err == nil
}

func (s *supportMatrixWriter) apply(ctx context.Context, input SupportMatrixInput) (supportMatrixReceipt, error) {
	authority, err := requireWriteAuthority(ctx, s.writes, OperationUpdateSupportMatrix)
	if err != nil {
		return supportMatrixReceipt{}, err
	}
	if !validSupportMatrixRevision(input.Revision) {
		return supportMatrixReceipt{}, errors.New("provide the current matrix revision")
	}
	if len(input.Changes) == 0 || len(input.Changes) > maxSupportMatrixChanges {
		return supportMatrixReceipt{}, fmt.Errorf("changes must contain between 1 and %d entries", maxSupportMatrixChanges)
	}
	changes, err := normalizeSupportMatrixChanges(input.Changes)
	if err != nil {
		return supportMatrixReceipt{}, err
	}
	target := writeTarget{ResourceKind: "support_matrix", ResourceID: input.Revision}
	return runWrite(ctx, s.runner, authority, target, func(ctx context.Context, tx pgx.Tx) (supportMatrixReceipt, error) {
		matrix, err := adminservice.ReadSupportMatrixTx(ctx, tx)
		if err != nil {
			return supportMatrixReceipt{}, fmt.Errorf("read locked support matrix: %w", err)
		}
		if matrix.Revision != input.Revision {
			return supportMatrixReceipt{}, ErrStaleState
		}
		draft, err := applySupportMatrixChanges(matrix.Draft, changes)
		if err != nil {
			return supportMatrixReceipt{}, err
		}
		if err := adminservice.ValidateSupportDraft(draft, matrix); err != nil {
			return supportMatrixReceipt{}, fmt.Errorf("validate support matrix changes: %w", err)
		}
		if reflect.DeepEqual(draft, matrix.Draft) {
			return supportMatrixReceipt{}, errors.New("these changes match the current support matrix; nothing would change")
		}
		diffs, err := diffSupportMatrixChanges(matrix.Draft, draft, changes)
		if err != nil {
			return supportMatrixReceipt{}, err
		}
		updated, err := adminservice.UpdateSupportMatrixTx(ctx, tx, &gen.UpdateSupportMatrixPayload{Revision: matrix.Revision, Draft: draft})
		if err != nil {
			return supportMatrixReceipt{}, fmt.Errorf("update support matrix: %w", err)
		}
		return supportMatrixReceipt{RevisionBefore: matrix.Revision, Revision: updated.Revision, Changes: diffs}, nil
	})
}

func normalizeSupportMatrixChanges(changes []SupportMatrixChange) ([]SupportMatrixChange, error) {
	seenEntries := make(map[string]bool, len(changes))
	seenFacts := make(map[string]bool)
	for _, change := range changes {
		if err := validateSupportMatrixEntryTarget(GetSupportMatrixEntryInput{Kind: change.Kind, MethodID: change.MethodID, ProductID: change.ProductID}); err != nil {
			return nil, err
		}
		key := change.Kind + "/" + change.MethodID + "/" + change.ProductID
		if seenEntries[key] {
			return nil, errors.New("changes contains a duplicate entry")
		}
		seenEntries[key] = true
		if change.Kind == "mapping" {
			if change.Applicability == nil && change.Conditions == nil && len(change.Facts) == 0 {
				return nil, errors.New("each mapping change must update at least one field")
			}
			if change.Applicability != nil && *change.Applicability != "unknown" && *change.Applicability != "applicable" && *change.Applicability != "na" {
				return nil, errors.New("applicability must be unknown, applicable, or na")
			}
			if change.Conditions != nil && (len(*change.Conditions) > maxSupportMatrixNote || !utf8.ValidString(*change.Conditions)) {
				return nil, errors.New("conditions must be valid UTF-8 and at most 512 bytes")
			}
		} else if change.Applicability != nil || change.Conditions != nil || len(change.Facts) == 0 {
			return nil, errors.New("each reference change must update one or more facts")
		}
		for _, fact := range change.Facts {
			if !validSupportMatrixText(fact.CapabilityID) || fact.Status == nil && fact.Note == nil && fact.Verify == nil {
				return nil, errors.New("each fact change needs an exact capability ID and at least one field")
			}
			if fact.Status != nil && !validSupportMatrixStatus(*fact.Status) {
				return nil, errors.New("invalid support matrix status")
			}
			if fact.Note != nil && (len(*fact.Note) > maxSupportMatrixNote || !utf8.ValidString(*fact.Note)) {
				return nil, errors.New("notes must be valid UTF-8 and at most 512 bytes")
			}
			factKey := change.Kind + "/" + change.MethodID + "/" + change.ProductID + "/" + fact.CapabilityID
			if seenFacts[factKey] {
				return nil, errors.New("changes contains a duplicate capability fact")
			}
			seenFacts[factKey] = true
		}
	}
	return changes, nil
}

func applySupportMatrixChanges(current *gen.SupportDraft, changes []SupportMatrixChange) (*gen.SupportDraft, error) {
	encoded, err := json.Marshal(current)
	if err != nil {
		return nil, fmt.Errorf("copy current support matrix draft: %w", err)
	}
	var draft gen.SupportDraft
	if err := json.Unmarshal(encoded, &draft); err != nil {
		return nil, fmt.Errorf("copy current support matrix draft: %w", err)
	}
	for _, change := range changes {
		switch change.Kind {
		case "mapping":
			key := change.MethodID + "/" + change.ProductID
			mapping := draft.Mappings[key]
			if mapping == nil {
				return nil, errors.New("support matrix mapping does not exist")
			}
			if change.Applicability != nil {
				mapping.Applicability = *change.Applicability
			}
			if change.Conditions != nil {
				mapping.Conditions = *change.Conditions
			}
			if err := applySupportMatrixFacts(mapping.Facts, change.Facts); err != nil {
				return nil, err
			}
		case "reference":
			facts, exists := draft.References[change.MethodID]
			if !exists {
				return nil, errors.New("support matrix reference does not exist")
			}
			if err := applySupportMatrixFacts(facts, change.Facts); err != nil {
				return nil, err
			}
		}
	}
	return &draft, nil
}

func applySupportMatrixFacts(current map[string]*gen.SupportFact, changes []SupportMatrixFactChange) error {
	for _, change := range changes {
		fact := current[change.CapabilityID]
		if fact == nil {
			return errors.New("support matrix capability fact does not exist")
		}
		if change.Status != nil {
			fact.Status = *change.Status
		}
		if change.Note != nil {
			fact.Note = *change.Note
		}
		if change.Verify != nil {
			fact.Verify = *change.Verify
		}
	}
	return nil
}

func diffSupportMatrixChanges(before, after *gen.SupportDraft, changes []SupportMatrixChange) ([]supportMatrixChangeDiff, error) {
	diffs := make([]supportMatrixChangeDiff, 0, len(changes))
	for _, change := range changes {
		oldJSON, err := json.Marshal(diffSupportMatrixFields(before, change))
		if err != nil {
			return nil, fmt.Errorf("encode support matrix before state: %w", err)
		}
		newJSON, err := json.Marshal(diffSupportMatrixFields(after, change))
		if err != nil {
			return nil, fmt.Errorf("encode support matrix after state: %w", err)
		}
		diffs = append(diffs, supportMatrixChangeDiff{Kind: change.Kind, MethodID: change.MethodID, ProductID: change.ProductID, Before: oldJSON, After: newJSON})
	}
	return diffs, nil
}

func diffSupportMatrixFields(draft *gen.SupportDraft, change SupportMatrixChange) map[string]any {
	fields := map[string]any{}
	var facts map[string]*gen.SupportFact
	if change.Kind == "mapping" {
		mapping := draft.Mappings[change.MethodID+"/"+change.ProductID]
		if change.Applicability != nil {
			fields["applicability"] = mapping.Applicability
		}
		if change.Conditions != nil {
			fields["conditions"] = mapping.Conditions
		}
		facts = mapping.Facts
	} else {
		facts = draft.References[change.MethodID]
	}
	if len(change.Facts) > 0 {
		selected := make([]SupportMatrixFactChange, 0, len(change.Facts))
		for _, requested := range change.Facts {
			fact := facts[requested.CapabilityID]
			entry := SupportMatrixFactChange{CapabilityID: requested.CapabilityID}
			if requested.Status != nil {
				entry.Status = &fact.Status
			}
			if requested.Note != nil {
				entry.Note = &fact.Note
			}
			if requested.Verify != nil {
				entry.Verify = &fact.Verify
			}
			selected = append(selected, entry)
		}
		fields["facts"] = selected
	}
	return fields
}

func (s *supportMatrixWriter) register(server *mcp.Server) {
	addWriteTool(server, &mcp.Tool{Name: string(OperationUpdateSupportMatrix), Title: "Update Global Support Matrix", Description: "Make bounded changes to existing global mapping or reference entries only. Use stable IDs and the exact revision from get_support_matrix_entry. Only supplied fields change; all other entries, notes, conditions, and verification flags are preserved. The result shows before and after for the supplied fields only. Does not create catalogue entries or modify issuers."}, s.apply)
}

var _ operationWriter = (*supportMatrixWriter)(nil)
