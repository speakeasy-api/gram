//nolint:exhaustruct // SDK optional defaults.
package adminmcp

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcpregistry"
	"time"
)

type PrepareSetRegistryIconsInput struct {
	EntryID           string                   `json:"entry_id" jsonschema:"Exact registry entry UUID"`
	ExpectedUpdatedAt string                   `json:"expected_updated_at" jsonschema:"Exact updated_at revision returned by get_registry_entry"`
	Icons             mcpregistry.CatalogIcons `json:"icons" jsonschema:"Full replacement of default and dark curated SVG icons; omitted slots are removed; an empty object removes both"`
	RetryKey          string                   `json:"retry_key" jsonschema:"Unique retry key for this exact change, up to 128 characters"`
}
type registryIconsPreview struct {
	EntryID string                   `json:"entry_id"`
	Before  mcpregistry.CatalogIcons `json:"before"`
	After   mcpregistry.CatalogIcons `json:"after"`
}
type registryIconsWriter struct {
	store    *proposalStore
	registry *mcpregistry.Service
	writes   WriteConfig
	baseURL  string
}

func (s *registryIconsWriter) prepare(ctx context.Context, input PrepareSetRegistryIconsInput) (ProposalOutput, error) {
	authority, err := requireWriteAuthority(ctx, s.writes, OperationSetRegistryIcons)
	if err != nil {
		return ProposalOutput{}, err
	}
	owner, err := ownerFromAuthority(authority)
	if err != nil {
		return ProposalOutput{}, err
	}
	if !validRegistryUUID(input.EntryID) || input.RetryKey == "" || len(input.RetryKey) > maxIdempotencyKeyLength {
		return ProposalOutput{}, errors.New("provide an exact entry UUID and retry key of at most 128 characters")
	}
	if _, err := mcpregistry.ParseToken(input.ExpectedUpdatedAt); err != nil {
		return ProposalOutput{}, err
	}
	args, err := json.Marshal(input)
	if err != nil {
		return ProposalOutput{}, err
	}
	if len(args) > maxProposalArgumentBytes {
		return ProposalOutput{}, errors.New("icon replacement exceeds storage limit")
	}
	old, err := s.registry.Get(ctx, uuid.MustParse(input.EntryID))
	if err != nil {
		return ProposalOutput{}, err
	}
	before, err := mcpregistry.ParseCatalogIcons(old.Data)
	if err != nil {
		return ProposalOutput{}, err
	}
	preview, err := json.Marshal(registryIconsPreview{EntryID: input.EntryID, Before: before, After: input.Icons})
	if err != nil {
		return ProposalOutput{}, err
	}
	expected, _ := json.Marshal(struct {
		Revision string `json:"revision"`
	}{input.ExpectedUpdatedAt})
	check := func(ctx context.Context, tx pgx.Tx) error {
		_, err := s.registry.CheckCatalogIconsTx(ctx, tx, old.ID, input.ExpectedUpdatedAt, input.Icons)
		return err
	}
	proposal, replay, err := s.store.CreateChecked(ctx, owner, NewProposal{Operation: OperationSetRegistryIcons, SchemaVersion: 1, IdempotencyKey: input.RetryKey, Arguments: args, ExpectedState: expected, Preview: preview}, time.Now(), check)
	if err != nil {
		return ProposalOutput{}, err
	}
	return proposalOutput(proposal, replay, s.baseURL), nil
}
func (s *registryIconsWriter) arguments(p Proposal) (PrepareSetRegistryIconsInput, error) {
	var args PrepareSetRegistryIconsInput
	if p.Operation != OperationSetRegistryIcons || p.SchemaVersion != 1 || !p.PlatformGlobal || p.Target.OrganizationID != "" || p.Target.ProjectID.Valid || p.Target.ResourceID != "" || p.Target.ResourceKind != "" {
		return args, ErrProposalInvalidated
	}
	if json.Unmarshal(p.Arguments, &args) != nil || !validRegistryUUID(args.EntryID) {
		return args, ErrProposalInvalidated
	}
	expected, _ := json.Marshal(struct {
		Revision string `json:"revision"`
	}{args.ExpectedUpdatedAt})
	digest, err := stateDigest(expected)
	if err != nil || !digestsEqual(digest, p.ExpectedStateDigest) {
		return args, ErrProposalInvalidated
	}
	return args, nil
}
func (s *registryIconsWriter) revalidate(ctx context.Context, tx pgx.Tx, p Proposal) error {
	args, err := s.arguments(p)
	if err != nil {
		return err
	}
	_, err = s.registry.CheckCatalogIconsTx(ctx, tx, uuid.MustParse(args.EntryID), args.ExpectedUpdatedAt, args.Icons)
	if err != nil {
		return registryCreateError(err)
	}
	return nil
}
func (s *registryIconsWriter) execution(_ writeAuthority) ProposalExecution {
	return ProposalExecution{Run: func(ctx context.Context, tx pgx.Tx, p Proposal) (string, json.RawMessage, error) {
		args, err := s.arguments(p)
		if err != nil {
			return "", nil, err
		}
		updated, err := s.registry.SetCatalogIconsTx(ctx, tx, uuid.MustParse(args.EntryID), args.ExpectedUpdatedAt, args.Icons)
		if err != nil {
			return "", nil, registryCreateError(err)
		}
		receipt, err := json.Marshal(registryCreateReceipt{ID: updated.ID.String(), UpdatedAt: mcpregistry.Token(updated)})
		return "succeeded", receipt, err
	}}
}
func (s *registryIconsWriter) view(p Proposal) (proposalView, error) {
	if _, err := s.arguments(p); err != nil {
		return proposalView{}, err
	}
	var preview registryIconsPreview
	if json.Unmarshal(p.Preview, &preview) != nil {
		return proposalView{}, ErrProposalInvalidated
	}
	before, _ := json.Marshal(preview.Before)
	after, _ := json.Marshal(preview.After)
	return proposalView{Summary: "Replace curated catalog icons for " + preview.EntryID, PlatformGlobal: true, Changes: []proposalViewChange{{Setting: "Complete curated icon set", Before: string(before), After: string(after)}}, SideEffects: "Missing default or dark slots are removed. Empty icons removes both. Publication and upstream server.icons are unchanged."}, nil
}
func (s *registryIconsWriter) registerPrepare(server *mcp.Server) {
	addProposalTool(server, &mcp.Tool{Name: "prepare_set_registry_icons", Title: "Prepare Registry Icons", Description: "Prepare full replacement of the curated default/dark SVG icon set at envelope _meta[com.speakeasy.ai/icons] for an exact registry entry and revision. Missing slots are removed; {} clears both. Upload public platform SVG assets with create_image_upload_url first. Requires admin:write and separate same-staff browser approval before execution. Does not edit arbitrary metadata or upstream server.icons.", Annotations: &mcp.ToolAnnotations{DestructiveHint: new(true)}}, func(ctx context.Context, _ *mcp.CallToolRequest, in PrepareSetRegistryIconsInput) (*mcp.CallToolResult, ProposalOutput, error) {
		out, err := s.prepare(ctx, in)
		return nil, out, err
	})
}

var _ operationWriter = (*registryIconsWriter)(nil)
