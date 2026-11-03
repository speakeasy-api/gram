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

type catalogCurationMutator interface {
	ValidateCurationTx(context.Context, pgx.Tx, string, uuid.UUID, string, string) (catalogpromotion.Candidate, error)
	CurateTx(context.Context, pgx.Tx, catalogpromotion.CurationRequest, string) (catalogpromotion.Candidate, error)
}

// PrepareCatalogCurationInput selects an unpublished candidate and exact review revision.
type PrepareCatalogCurationInput struct {
	// OrganizationID is the canonical owner of the candidate.
	OrganizationID string `json:"organization_id" jsonschema:"Exact canonical organization ID from the candidate"`
	// PromotionID identifies the candidate.
	PromotionID string `json:"promotion_id" jsonschema:"Exact promotion UUID from list_registry_promotion_candidates"`
	// CurationRevision binds the change to the inspected review state.
	CurationRevision string `json:"curation_revision" jsonschema:"Exact curation_revision from the candidate"`
	// Action is reject, archive, or restore for an unpublished candidate.
	Action string `json:"action" jsonschema:"Exactly reject, archive, or restore; publication is never changed"`
	// RetryKey deduplicates this exact request.
	RetryKey string `json:"retry_key" jsonschema:"Unique retry key for this exact change, at most 128 characters"`
}

type catalogCurationWriter struct {
	store   *proposalStore
	writes  WriteConfig
	baseURL string
	mutator catalogCurationMutator
	now     func() time.Time
}

type storedCuration struct {
	Request   catalogpromotion.CurationRequest `json:"request"`
	ProjectID uuid.NullUUID                    `json:"project_id"`
}

type catalogCurationReceipt struct {
	// PromotionID identifies the curated candidate.
	PromotionID string `json:"promotion_id"`
	// Status confirms the committed review status.
	Status string `json:"status"`
}

func curationMatches(candidate catalogpromotion.Candidate, request catalogpromotion.CurationRequest) bool {
	return candidate.ID == request.PromotionID.String() && candidate.OrganizationID == request.OrganizationID && candidate.CurationRevision == request.CurationRevision && curationTransition(candidate.Status, request.Action)
}

func (w *catalogCurationWriter) prepare(ctx context.Context, input PrepareCatalogCurationInput) (ProposalOutput, error) {
	authority, err := requireWriteAuthority(ctx, w.writes, OperationCurateRegistryPromotionCandidate)
	if err != nil {
		return ProposalOutput{}, err
	}
	owner, err := ownerFromAuthority(authority)
	if err != nil {
		return ProposalOutput{}, err
	}
	if err := checkPrepareTarget(input.OrganizationID, input.RetryKey); err != nil {
		return ProposalOutput{}, err
	}
	if !validRegistryUUID(input.PromotionID) || !validRegistryUUID(input.CurationRevision) || curationStatus(input.Action) == "" {
		return ProposalOutput{}, errors.New("provide an exact candidate UUID, current curation revision, and reject, archive, or restore action")
	}
	request := catalogpromotion.CurationRequest{OrganizationID: input.OrganizationID, PromotionID: uuid.MustParse(input.PromotionID), CurationRevision: input.CurationRevision, RetryKey: input.RetryKey, Action: input.Action}
	tx, err := w.store.db.Begin(ctx)
	if err != nil {
		return ProposalOutput{}, fmt.Errorf("begin candidate curation preparation: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(context.WithoutCancel(ctx)) })
	candidate, err := w.mutator.ValidateCurationTx(ctx, tx, request.OrganizationID, request.PromotionID, request.CurationRevision, request.Action)
	if err != nil {
		return ProposalOutput{}, catalogPromotionValidationError(err)
	}
	if !curationMatches(candidate, request) {
		return ProposalOutput{}, ErrStaleState
	}
	project, err := curationProject(candidate)
	if err != nil {
		return ProposalOutput{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ProposalOutput{}, fmt.Errorf("finish candidate curation preparation: %w", err)
	}
	args, err := json.Marshal(storedCuration{Request: request, ProjectID: project})
	if err != nil {
		return ProposalOutput{}, fmt.Errorf("encode candidate curation: %w", err)
	}
	proposal, replay, err := w.store.Create(ctx, owner, NewProposal{
		Operation: OperationCurateRegistryPromotionCandidate, SchemaVersion: 1,
		Target:         ProposalTarget{OrganizationID: request.OrganizationID, ProjectID: project, ResourceKind: "registry_promotion", ResourceID: request.PromotionID.String()},
		IdempotencyKey: request.RetryKey, Arguments: args, ExpectedState: args, Preview: args,
	}, w.now())
	if err != nil {
		return ProposalOutput{}, err
	}
	return proposalOutput(proposal, replay, w.baseURL), nil
}

func storedCatalogCuration(proposal Proposal) (storedCuration, error) {
	var args storedCuration
	if proposal.Operation != OperationCurateRegistryPromotionCandidate || proposal.SchemaVersion != 1 || proposal.PlatformGlobal || proposal.Target.ResourceKind != "registry_promotion" || json.Unmarshal(proposal.Arguments, &args) != nil {
		return args, ErrProposalInvalidated
	}
	request := args.Request
	if request.OrganizationID != proposal.Target.OrganizationID || request.PromotionID == uuid.Nil || request.PromotionID.String() != proposal.Target.ResourceID || args.ProjectID != proposal.Target.ProjectID || request.RetryKey != proposal.IdempotencyKey || !validRegistryUUID(request.CurationRevision) || !args.ProjectID.Valid || args.ProjectID.UUID == uuid.Nil || curationStatus(request.Action) == "" {
		return args, ErrProposalInvalidated
	}
	digest, err := stateDigest(proposal.Arguments)
	if err != nil || !digestsEqual(digest, proposal.ExpectedStateDigest) {
		return args, ErrProposalInvalidated
	}
	return args, nil
}

func (w *catalogCurationWriter) revalidate(ctx context.Context, tx pgx.Tx, proposal Proposal) error {
	args, err := storedCatalogCuration(proposal)
	if err != nil {
		return err
	}
	candidate, err := w.mutator.ValidateCurationTx(ctx, tx, args.Request.OrganizationID, args.Request.PromotionID, args.Request.CurationRevision, args.Request.Action)
	if err != nil {
		return catalogPromotionValidationError(err)
	}
	if !curationMatches(candidate, args.Request) {
		return ErrStaleState
	}
	project, err := curationProject(candidate)
	if err != nil || project != args.ProjectID {
		return ErrStaleState
	}
	return nil
}

func (w *catalogCurationWriter) view(proposal Proposal) (proposalView, error) {
	args, err := storedCatalogCuration(proposal)
	if err != nil {
		return proposalView{}, err
	}
	var preview storedCuration
	if json.Unmarshal(proposal.Preview, &preview) != nil || preview != args {
		return proposalView{}, ErrProposalInvalidated
	}
	return proposalView{
		Summary:        "Change catalogue promotion review status",
		PlatformGlobal: false, OrganizationID: args.Request.OrganizationID, OrganizationName: "", OrganizationSlug: "",
		Changes: []proposalViewChange{
			{Setting: "Candidate", Before: "", After: args.Request.PromotionID.String()},
			{Setting: "Review revision", Before: "", After: args.Request.CurationRevision},
			{Setting: "Action", Before: "", After: args.Request.Action},
			{Setting: "Review status", Before: "", After: curationStatus(args.Request.Action)},
		},
		SideEffects: "Reject or archive hides an unpublished candidate and suspends discovery until restored. Restore returns it to review and requires fresh eligibility. Does not publish, unpublish, delete the source, or request documentation.",
	}, nil
}

func (w *catalogCurationWriter) execution(authority writeAuthority) ProposalExecution {
	return ProposalExecution{Lock: nil, AfterCommit: nil, Run: func(ctx context.Context, tx pgx.Tx, proposal Proposal) (string, json.RawMessage, error) {
		if err := w.revalidate(ctx, tx, proposal); err != nil {
			return "", nil, err
		}
		args, err := storedCatalogCuration(proposal)
		if err != nil {
			return "", nil, err
		}
		ctx, principal, _ := staffMutation(ctx, authority)
		candidate, err := w.mutator.CurateTx(ctx, tx, args.Request, principal.String())
		if err != nil {
			return "", nil, errors.New("candidate curation could not be committed")
		}
		if candidate.ID != args.Request.PromotionID.String() || candidate.OrganizationID != args.Request.OrganizationID || candidate.Status != curationStatus(args.Request.Action) || candidate.ProjectID != args.ProjectID.UUID.String() {
			return "", nil, errors.New("candidate curation receipt is unavailable")
		}
		result, err := json.Marshal(catalogCurationReceipt{PromotionID: candidate.ID, Status: candidate.Status})
		if err != nil {
			return "", nil, fmt.Errorf("encode candidate curation receipt: %w", err)
		}
		return "succeeded", result, nil
	}}
}

func (w *catalogCurationWriter) registerPrepare(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Meta: nil, InputSchema: nil, OutputSchema: nil, Icons: nil,
		Name: "prepare_registry_promotion_curation", Title: "Prepare Catalogue Candidate Review Status",
		Description: "Prepare reject, archive, or restore of an exact unpublished catalogue candidate using its organization, candidate UUID and current curation revision. Reject/archive hides it from the active queue until restored. Creates a stored preview for same-staff browser approval; execute_admin_proposal applies the change. Requires admin:write. Does not modify status during preparation or alter published entries.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: new(false), IdempotentHint: true, OpenWorldHint: new(false), Title: ""},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input PrepareCatalogCurationInput) (*mcp.CallToolResult, ProposalOutput, error) {
		out, err := w.prepare(ctx, input)
		return nil, out, err
	})
}

func curationStatus(action string) string {
	switch action {
	case "reject":
		return "rejected"
	case "archive":
		return "archived"
	case "restore":
		return "candidate"
	default:
		return ""
	}
}

func curationTransition(status, action string) bool {
	switch action {
	case "reject", "archive":
		return status == "candidate"
	case "restore":
		return status == "rejected" || status == "archived"
	default:
		return false
	}
}

func curationProject(candidate catalogpromotion.Candidate) (uuid.NullUUID, error) {
	if !validRegistryUUID(candidate.ProjectID) {
		return uuid.NullUUID{}, ErrStaleState
	}
	return uuid.NullUUID{UUID: uuid.MustParse(candidate.ProjectID), Valid: true}, nil
}
