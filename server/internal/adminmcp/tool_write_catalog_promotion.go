package adminmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/catalogpromotion"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

// catalogPromotionMutator shares the same transactional rules as the admin UI.
type catalogPromotionMutator interface {
	GetCandidate(context.Context, catalogpromotion.Target) (catalogpromotion.Candidate, error)
	ValidateTx(context.Context, pgx.Tx, catalogpromotion.Target, string, catalogpromotion.PublicMetadata) (catalogpromotion.Candidate, error)
	PromoteTx(context.Context, pgx.Tx, catalogpromotion.Request, string) (catalogpromotion.Candidate, error)
}

// PrepareCatalogPromotionInput names the exact source and independently reviewed public record.
type PrepareCatalogPromotionInput struct {
	// OrganizationID is the canonical source organization.
	OrganizationID string `json:"organization_id" jsonschema:"Exact canonical organization ID"`
	// ProjectID is the canonical source project.
	ProjectID string `json:"project_id" jsonschema:"Exact canonical project UUID"`
	// MCPServerID identifies the remote source inside that project.
	MCPServerID string `json:"mcp_server_id" jsonschema:"Exact MCP server UUID from the candidate"`
	// SourceRevision binds the reviewed configuration.
	SourceRevision string `json:"source_revision" jsonschema:"Exact current source revision from get_registry_promotion_candidate"`
	// Public contains only metadata deliberately approved for public distribution.
	Public catalogpromotion.PublicMetadata `json:"public" jsonschema:"Independently reviewed public metadata; never copy customer endpoints, credentials, names or configuration"`
	// PublicMetadataConfirmed attests that these fields can be publicly distributed.
	PublicMetadataConfirmed bool `json:"public_metadata_confirmed" jsonschema:"Confirm every public field is suitable for the public catalogue and setup docs"`
	// RetryKey deduplicates preparation of this exact change.
	RetryKey string `json:"retry_key" jsonschema:"Unique retry key for this exact change, at most 128 characters"`
}

type catalogPromotionWriter struct {
	store   *proposalStore
	writes  WriteConfig
	baseURL string
	mutator catalogPromotionMutator
	now     func() time.Time
}

// catalogPromotionReceipt identifies the committed publication without copying domain content.
type catalogPromotionReceipt struct {
	// PromotionID identifies the durable promotion record.
	PromotionID string `json:"promotion_id"`

	// RegistryEntryID identifies the published catalogue entry.
	RegistryEntryID string `json:"registry_entry_id"`
}

func catalogPromotionRequest(input PrepareCatalogPromotionInput) (catalogpromotion.Request, error) {
	if err := checkPrepareTarget(input.OrganizationID, input.RetryKey); err != nil {
		return catalogpromotion.Request{}, err
	}
	if !validRegistryUUID(input.ProjectID) || !validRegistryUUID(input.MCPServerID) || input.SourceRevision == "" || len(input.SourceRevision) > maxRegistryText || !input.PublicMetadataConfirmed {
		return catalogpromotion.Request{}, errors.New("provide exact project and server UUIDs, a current source revision, and confirm the public metadata")
	}
	return catalogpromotion.Request{
		Target:         catalogpromotion.Target{OrganizationID: input.OrganizationID, ProjectID: uuid.MustParse(input.ProjectID), MCPServerID: uuid.MustParse(input.MCPServerID)},
		SourceRevision: input.SourceRevision, Public: input.Public,
		PublicMetadataConfirmed: true, RetryKey: input.RetryKey,
	}, nil
}

func (w *catalogPromotionWriter) prepare(ctx context.Context, input PrepareCatalogPromotionInput) (ProposalOutput, error) {
	authority, err := requireWriteAuthority(ctx, w.writes, OperationPromoteRegistryCandidate)
	if err != nil {
		return ProposalOutput{}, err
	}
	owner, err := ownerFromAuthority(authority)
	if err != nil {
		return ProposalOutput{}, err
	}
	request, err := catalogPromotionRequest(input)
	if err != nil {
		return ProposalOutput{}, err
	}
	candidate, err := w.mutator.GetCandidate(ctx, request.Target)
	if err != nil {
		return ProposalOutput{}, errors.New("promotion evidence is unavailable")
	}
	if !candidate.Eligible || candidate.SourceRevision != request.SourceRevision {
		return ProposalOutput{}, ErrStaleState
	}
	tx, err := w.store.db.Begin(ctx)
	if err != nil {
		return ProposalOutput{}, fmt.Errorf("begin promotion preparation: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(context.WithoutCancel(ctx)) })
	if _, err := w.mutator.ValidateTx(ctx, tx, request.Target, request.SourceRevision, request.Public); err != nil {
		return ProposalOutput{}, errors.New("promotion target or public metadata is invalid; refresh the candidate")
	}
	if err := tx.Commit(ctx); err != nil {
		return ProposalOutput{}, fmt.Errorf("finish promotion preparation: %w", err)
	}
	args, err := json.Marshal(request)
	if err != nil {
		return ProposalOutput{}, fmt.Errorf("encode promotion proposal: %w", err)
	}
	proposal, replay, err := w.store.Create(ctx, owner, NewProposal{
		Operation: OperationPromoteRegistryCandidate, SchemaVersion: 1,
		Target:         ProposalTarget{OrganizationID: request.Target.OrganizationID, ProjectID: uuid.NullUUID{UUID: request.Target.ProjectID, Valid: true}, ResourceKind: "mcp_server", ResourceID: request.Target.MCPServerID.String()},
		IdempotencyKey: request.RetryKey, Arguments: args, ExpectedState: args, Preview: args,
	}, w.now())
	if err != nil {
		return ProposalOutput{}, err
	}
	return proposalOutput(proposal, replay, w.baseURL), nil
}

func storedCatalogPromotion(proposal Proposal) (catalogpromotion.Request, error) {
	var request catalogpromotion.Request
	if proposal.Operation != OperationPromoteRegistryCandidate || proposal.SchemaVersion != 1 || proposal.PlatformGlobal || !proposal.Target.ProjectID.Valid || proposal.Target.ResourceKind != "mcp_server" || json.Unmarshal(proposal.Arguments, &request) != nil {
		return request, ErrProposalInvalidated
	}
	if !request.PublicMetadataConfirmed || request.Target.OrganizationID != proposal.Target.OrganizationID || request.Target.ProjectID != proposal.Target.ProjectID.UUID || request.Target.MCPServerID.String() != proposal.Target.ResourceID || request.RetryKey != proposal.IdempotencyKey {
		return request, ErrProposalInvalidated
	}
	digest, err := stateDigest(proposal.Arguments)
	if err != nil || !digestsEqual(digest, proposal.ExpectedStateDigest) {
		return request, ErrProposalInvalidated
	}
	return request, nil
}

func (w *catalogPromotionWriter) revalidate(ctx context.Context, tx pgx.Tx, proposal Proposal) error {
	request, err := storedCatalogPromotion(proposal)
	if err != nil {
		return err
	}
	_, err = w.mutator.ValidateTx(ctx, tx, request.Target, request.SourceRevision, request.Public)
	if err != nil {
		return ErrStaleState
	}
	return nil
}

func (w *catalogPromotionWriter) view(proposal Proposal) (proposalView, error) {
	request, err := storedCatalogPromotion(proposal)
	if err != nil {
		return proposalView{}, err
	}
	var preview catalogpromotion.Request
	if json.Unmarshal(proposal.Preview, &preview) != nil || preview != request {
		return proposalView{}, ErrProposalInvalidated
	}
	return proposalView{
		Summary:        "Publish reviewed remote MCP server to the catalogue",
		PlatformGlobal: false, OrganizationID: request.Target.OrganizationID, OrganizationName: "", OrganizationSlug: "",
		Changes: []proposalViewChange{
			{Setting: "Source project", Before: "", After: request.Target.ProjectID.String()},
			{Setting: "Source server", Before: "", After: request.Target.MCPServerID.String()},
			{Setting: "Public catalogue name", Before: "", After: request.Public.Name},
			{Setting: "Public title", Before: "", After: request.Public.Title},
			{Setting: "Public description", Before: "", After: request.Public.Description},
			{Setting: "Public endpoint", Before: "", After: request.Public.URL},
			{Setting: "Public documentation", Before: "", After: request.Public.DocumentationURL},
			{Setting: "Guide slug", Before: "", After: request.Public.DocsSlug},
			{Setting: "Authentication", Before: "", After: request.Public.AuthMode},
		},
		SideEffects: "Publishes the reviewed fields to the Speakeasy catalogue, visible under the existing catalogue rollout. Queues a public setup-docs guide draft. Guide publication retains the setup-docs repository's merge review. Source credentials and configuration are excluded.",
	}, nil
}

func (w *catalogPromotionWriter) execution(authority writeAuthority) ProposalExecution {
	return ProposalExecution{
		Lock: nil, AfterCommit: nil,
		Run: func(ctx context.Context, tx pgx.Tx, proposal Proposal) (string, json.RawMessage, error) {
			if err := w.revalidate(ctx, tx, proposal); err != nil {
				return "", nil, err
			}
			request, err := storedCatalogPromotion(proposal)
			if err != nil {
				return "", nil, err
			}
			ctx, principal, _ := staffMutation(ctx, authority)
			candidate, err := w.mutator.PromoteTx(ctx, tx, request, principal.String())
			if err != nil {
				return "", nil, errors.New("promotion could not be committed")
			}
			if !validRegistryUUID(candidate.ID) || !validRegistryUUID(candidate.RegistryEntryID) {
				return "", nil, errors.New("promotion receipt is unavailable")
			}
			result, err := json.Marshal(catalogPromotionReceipt{PromotionID: candidate.ID, RegistryEntryID: candidate.RegistryEntryID})
			if err != nil {
				return "", nil, fmt.Errorf("encode promotion receipt: %w", err)
			}
			return "succeeded", result, nil
		},
	}
}

func (w *catalogPromotionWriter) registerPrepare(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Meta: nil, InputSchema: nil, OutputSchema: nil, Icons: nil,
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: new(false), IdempotentHint: true, OpenWorldHint: new(false), Title: ""},
		Name:        "prepare_registry_promotion", Title: "Prepare Catalogue Promotion",
		Description: "Prepare publication of a healthy remote MCP source using exact organization, project, server and source revision. Supply independently reviewed public metadata only. Creates a stored preview for same-staff browser approval; execute_admin_proposal publishes and queues setup docs. Requires admin:write. Does not publish during preparation.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input PrepareCatalogPromotionInput) (*mcp.CallToolResult, ProposalOutput, error) {
		out, err := w.prepare(ctx, input)
		return nil, out, err
	})
}
