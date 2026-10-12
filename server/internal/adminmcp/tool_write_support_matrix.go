//nolint:exhaustruct // MCP SDK manifests intentionally use documented optional defaults.
package adminmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"
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

type PrepareSupportMatrixInput struct {
	Revision string                `json:"revision" jsonschema:"Exact current revision from get_support_matrix_entry; any later matrix change requires a new proposal"`
	Changes  []SupportMatrixChange `json:"changes" jsonschema:"One to 25 exact existing mapping or reference entries to update"`
	RetryKey string                `json:"retry_key" jsonschema:"Unique retry key for this exact change (up to 128 characters)"`
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

type supportMatrixExpected struct {
	Revision string `json:"revision"`
}

type supportMatrixArguments struct {
	Revision string                `json:"revision"`
	Changes  []SupportMatrixChange `json:"changes"`
}

type supportMatrixPreviewChange struct {
	Kind      string          `json:"kind"`
	MethodID  string          `json:"method_id"`
	ProductID string          `json:"product_id,omitempty"`
	Before    json.RawMessage `json:"before"`
	After     json.RawMessage `json:"after"`
}

type supportMatrixPreview struct {
	RevisionBefore string                       `json:"revision_before"`
	RevisionAfter  string                       `json:"revision_after,omitempty"`
	Changes        []supportMatrixPreviewChange `json:"changes"`
}

type supportMatrixReceipt struct {
	Revision string `json:"revision"`
}

// supportMatrixWriter executes one platform-global support matrix proposal.
type supportMatrixWriter struct {
	store   *proposalStore
	writes  WriteConfig
	baseURL string
}

func (s *supportMatrixWriter) readMatrix(ctx context.Context, tx pgx.Tx) (*gen.SupportMatrix, error) {
	matrix, err := adminservice.ReadSupportMatrixTx(ctx, tx)
	if err != nil {
		return nil, fmt.Errorf("read locked support matrix: %w", err)
	}
	return matrix, nil
}

func (s *supportMatrixWriter) expectedState(ctx context.Context, tx pgx.Tx, proposal Proposal) (supportMatrixExpected, error) {
	if proposal.Operation != OperationUpdateSupportMatrix || proposal.SchemaVersion != 1 || !proposal.PlatformGlobal || proposal.Target.OrganizationID != "" || proposal.Target.ProjectID.Valid || proposal.Target.ResourceID != "" || proposal.Target.ResourceKind != "" {
		return supportMatrixExpected{}, ErrProposalInvalidated
	}
	var args supportMatrixArguments
	if err := json.Unmarshal(proposal.Arguments, &args); err != nil || args.Revision == "" || len(args.Changes) == 0 {
		return supportMatrixExpected{}, ErrProposalInvalidated
	}
	matrix, err := s.readMatrix(ctx, tx)
	if err != nil {
		return supportMatrixExpected{}, err
	}
	if matrix.Revision != args.Revision {
		return supportMatrixExpected{}, ErrStaleState
	}
	if _, err := normalizeSupportMatrixChanges(args.Changes); err != nil {
		return supportMatrixExpected{}, ErrProposalInvalidated
	}
	encoded, err := json.Marshal(supportMatrixExpected{Revision: matrix.Revision})
	if err != nil {
		return supportMatrixExpected{}, fmt.Errorf("encode expected support matrix state: %w", err)
	}
	digest, err := stateDigest(encoded)
	if err != nil || !digestsEqual(digest, proposal.ExpectedStateDigest) {
		return supportMatrixExpected{}, ErrStaleState
	}
	return supportMatrixExpected{Revision: args.Revision}, nil
}

func (s *supportMatrixWriter) revalidate(ctx context.Context, tx pgx.Tx, proposal Proposal) error {
	_, err := s.expectedState(ctx, tx, proposal)
	return err
}

func (s *supportMatrixWriter) prepare(ctx context.Context, input PrepareSupportMatrixInput) (ProposalOutput, error) {
	authority, err := requireWriteAuthority(ctx, s.writes, OperationUpdateSupportMatrix)
	if err != nil {
		return ProposalOutput{}, err
	}
	owner, err := ownerFromAuthority(authority)
	if err != nil {
		return ProposalOutput{}, err
	}
	if input.RetryKey == "" || len(input.RetryKey) > maxIdempotencyKeyLength || input.Revision == "" || len(input.Revision) > maxSupportMatrixText {
		return ProposalOutput{}, errors.New("provide the current matrix revision and a retry key of at most 128 characters")
	}
	if len(input.Changes) == 0 || len(input.Changes) > maxSupportMatrixChanges {
		return ProposalOutput{}, fmt.Errorf("changes must contain between 1 and %d entries", maxSupportMatrixChanges)
	}
	input.Changes, err = normalizeSupportMatrixChanges(input.Changes)
	if err != nil {
		return ProposalOutput{}, err
	}
	tx, err := s.store.db.Begin(ctx)
	if err != nil {
		return ProposalOutput{}, fmt.Errorf("begin support matrix proposal preparation: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	matrix, err := s.readMatrix(ctx, tx)
	if err != nil {
		return ProposalOutput{}, err
	}
	if matrix.Revision != input.Revision {
		return ProposalOutput{}, ErrStaleState
	}
	before, err := json.Marshal(supportMatrixExpected{Revision: matrix.Revision})
	if err != nil {
		return ProposalOutput{}, fmt.Errorf("encode current support matrix revision: %w", err)
	}
	draft, err := applySupportMatrixChanges(matrix.Draft, input.Changes)
	if err != nil {
		return ProposalOutput{}, err
	}
	if err := adminservice.ValidateSupportDraft(draft, matrix); err != nil {
		return ProposalOutput{}, fmt.Errorf("validate support matrix changes: %w", err)
	}
	if reflect.DeepEqual(draft, matrix.Draft) {
		return ProposalOutput{}, errors.New("these changes match the current support matrix; nothing would change")
	}
	if err := tx.Commit(ctx); err != nil {
		return ProposalOutput{}, fmt.Errorf("finish support matrix proposal preparation: %w", err)
	}
	arguments, err := json.Marshal(supportMatrixArguments{Revision: matrix.Revision, Changes: input.Changes})
	if err != nil {
		return ProposalOutput{}, fmt.Errorf("encode support matrix arguments: %w", err)
	}
	changes, err := previewSupportMatrixChanges(matrix.Draft, draft, input.Changes)
	if err != nil {
		return ProposalOutput{}, err
	}
	preview, err := json.Marshal(supportMatrixPreview{RevisionBefore: matrix.Revision, Changes: changes})
	if err != nil {
		return ProposalOutput{}, fmt.Errorf("encode support matrix preview: %w", err)
	}
	if len(arguments) > maxProposalArgumentBytes || len(before) > maxProposalArgumentBytes || len(preview) > maxProposalPreviewBytes {
		return ProposalOutput{}, errors.New("support matrix proposal exceeds the existing storage limit; reduce the change set")
	}
	proposal, replay, err := s.store.Create(ctx, owner, NewProposal{Operation: OperationUpdateSupportMatrix, SchemaVersion: 1, IdempotencyKey: input.RetryKey, Arguments: arguments, ExpectedState: before, Preview: preview}, time.Now())
	if err != nil {
		return ProposalOutput{}, err
	}
	return proposalOutput(proposal, replay, s.baseURL), nil
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

func previewSupportMatrixChanges(before, after *gen.SupportDraft, changes []SupportMatrixChange) ([]supportMatrixPreviewChange, error) {
	preview := make([]supportMatrixPreviewChange, 0, len(changes))
	for _, change := range changes {
		oldJSON, err := json.Marshal(previewSupportMatrixFields(before, change))
		if err != nil {
			return nil, fmt.Errorf("encode support matrix before preview: %w", err)
		}
		newJSON, err := json.Marshal(previewSupportMatrixFields(after, change))
		if err != nil {
			return nil, fmt.Errorf("encode support matrix after preview: %w", err)
		}
		preview = append(preview, supportMatrixPreviewChange{Kind: change.Kind, MethodID: change.MethodID, ProductID: change.ProductID, Before: oldJSON, After: newJSON})
	}
	return preview, nil
}

func previewSupportMatrixFields(draft *gen.SupportDraft, change SupportMatrixChange) map[string]any {
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

func (s *supportMatrixWriter) view(proposal Proposal) (proposalView, error) {
	if !proposal.PlatformGlobal || proposal.Target.OrganizationID != "" || proposal.Target.ProjectID.Valid || proposal.Target.ResourceID != "" || proposal.Target.ResourceKind != "" {
		return proposalView{}, ErrProposalInvalidated
	}
	var preview supportMatrixPreview
	if err := json.Unmarshal(proposal.Preview, &preview); err != nil || preview.RevisionBefore == "" || len(preview.Changes) == 0 {
		return proposalView{}, ErrProposalInvalidated
	}
	changes := make([]proposalViewChange, 0, len(preview.Changes))
	for _, change := range preview.Changes {
		label := change.Kind + " " + change.MethodID
		if change.ProductID != "" {
			label += "/" + change.ProductID
		}
		if len(change.Before) == 0 || len(change.After) == 0 {
			return proposalView{}, ErrProposalInvalidated
		}
		changes = append(changes, proposalViewChange{Setting: label, Before: string(change.Before), After: string(change.After)})
	}
	return proposalView{Summary: "Update the platform-global support matrix", PlatformGlobal: true, Changes: changes}, nil
}

func (s *supportMatrixWriter) execution(_ writeAuthority) ProposalExecution {
	return ProposalExecution{Run: func(ctx context.Context, tx pgx.Tx, proposal Proposal) (string, json.RawMessage, error) {
		if _, err := s.expectedState(ctx, tx, proposal); err != nil {
			return "", nil, err
		}
		var args supportMatrixArguments
		if err := json.Unmarshal(proposal.Arguments, &args); err != nil || len(args.Changes) == 0 {
			return "", nil, ErrProposalInvalidated
		}
		matrix, err := s.readMatrix(ctx, tx)
		if err != nil {
			return "", nil, err
		}
		draft, err := applySupportMatrixChanges(matrix.Draft, args.Changes)
		if err != nil {
			return "", nil, ErrProposalInvalidated
		}
		updated, err := adminservice.UpdateSupportMatrixTx(ctx, tx, &gen.UpdateSupportMatrixPayload{Revision: args.Revision, Draft: draft})
		if err != nil {
			return "", nil, fmt.Errorf("update support matrix: %w", err)
		}
		result, err := json.Marshal(supportMatrixReceipt{Revision: updated.Revision})
		if err != nil {
			return "", nil, fmt.Errorf("encode support matrix receipt: %w", err)
		}
		return "succeeded", result, nil
	}}
}

func (s *supportMatrixWriter) registerPrepare(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{Name: "prepare_update_support_matrix", Title: "Prepare Global Support Matrix Update", Description: "Prepare bounded changes to existing global mapping or reference entries only. Use stable IDs and the exact revision from get_support_matrix_entry. Only supplied fields change; all other entries, notes, conditions, and verification flags are preserved. The before/after preview includes only supplied fields and is stored for separate same-staff approval. Does not create catalogue entries or modify issuers. Requires admin:write.", Annotations: &mcp.ToolAnnotations{DestructiveHint: new(true)}}, func(ctx context.Context, _ *mcp.CallToolRequest, input PrepareSupportMatrixInput) (*mcp.CallToolResult, ProposalOutput, error) {
		out, err := s.prepare(ctx, input)
		return nil, out, err
	})
}

var _ operationWriter = (*supportMatrixWriter)(nil)
