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

type catalogDocsRetryMutator interface {
	ValidateDocsRetryTx(context.Context, pgx.Tx, string, uuid.UUID, string) (catalogpromotion.Candidate, error)
	RetryDocsTx(context.Context, pgx.Tx, catalogpromotion.DocsRetryRequest, string) (catalogpromotion.Candidate, error)
}

// PrepareCatalogDocsRetryInput selects one retained publication and delivery revision.
type PrepareCatalogDocsRetryInput struct {
	// OrganizationID is the canonical owner of the retained publication.
	OrganizationID string `json:"organization_id" jsonschema:"Exact canonical organization ID from the promotion"`
	// PromotionID identifies the retained publication, even after source deletion.
	PromotionID string `json:"promotion_id" jsonschema:"Exact promotion UUID from list_registry_promotion_candidates"`
	// DocsRevision binds the retry to the inspected delivery state.
	DocsRevision string `json:"docs_revision" jsonschema:"Exact docs_revision from the promotion with docs_retry_allowed true"`
	// RetryKey deduplicates this exact request.
	RetryKey string `json:"retry_key" jsonschema:"Unique retry key for this exact retry, at most 128 characters"`
}

type catalogDocsRetryWriter struct {
	store   *proposalStore
	writes  WriteConfig
	baseURL string
	mutator catalogDocsRetryMutator
	now     func() time.Time
}

type storedDocsRetry struct {
	Request   catalogpromotion.DocsRetryRequest `json:"request"`
	ProjectID uuid.NullUUID                     `json:"project_id"`
}

type catalogDocsRetryReceipt struct {
	// PromotionID identifies the retained publication.
	PromotionID string `json:"promotion_id"`
	// DocsStatus confirms the guide request is scheduled again.
	DocsStatus string `json:"docs_status"`
}

func docsRetryProject(candidate catalogpromotion.Candidate) (uuid.NullUUID, error) {
	if candidate.ProjectID == "" {
		return uuid.NullUUID{UUID: uuid.Nil, Valid: false}, nil
	}
	if !validRegistryUUID(candidate.ProjectID) {
		return uuid.NullUUID{}, ErrStaleState
	}
	return uuid.NullUUID{UUID: uuid.MustParse(candidate.ProjectID), Valid: true}, nil
}

func docsRetryMatches(candidate catalogpromotion.Candidate, request catalogpromotion.DocsRetryRequest) bool {
	return candidate.ID == request.PromotionID.String() && candidate.OrganizationID == request.OrganizationID && candidate.DocsRevision == request.DocsRevision && candidate.Status == "promoted" && candidate.DocsRetryAllowed
}

func (w *catalogDocsRetryWriter) prepare(ctx context.Context, input PrepareCatalogDocsRetryInput) (ProposalOutput, error) {
	authority, err := requireWriteAuthority(ctx, w.writes, OperationRetryRegistryPromotionDocs)
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
	if !validRegistryUUID(input.PromotionID) || !validRegistryUUID(input.DocsRevision) {
		return ProposalOutput{}, errors.New("provide an exact promotion UUID and current docs revision")
	}
	request := catalogpromotion.DocsRetryRequest{OrganizationID: input.OrganizationID, PromotionID: uuid.MustParse(input.PromotionID), DocsRevision: input.DocsRevision, RetryKey: input.RetryKey}
	tx, err := w.store.db.Begin(ctx)
	if err != nil {
		return ProposalOutput{}, fmt.Errorf("begin guide retry preparation: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(context.WithoutCancel(ctx)) })
	candidate, err := w.mutator.ValidateDocsRetryTx(ctx, tx, request.OrganizationID, request.PromotionID, request.DocsRevision)
	if err != nil {
		return ProposalOutput{}, docsRetryValidationError(err)
	}
	if !docsRetryMatches(candidate, request) {
		return ProposalOutput{}, ErrStaleState
	}
	project, err := docsRetryProject(candidate)
	if err != nil {
		return ProposalOutput{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ProposalOutput{}, fmt.Errorf("finish guide retry preparation: %w", err)
	}
	args, err := json.Marshal(storedDocsRetry{Request: request, ProjectID: project})
	if err != nil {
		return ProposalOutput{}, fmt.Errorf("encode guide retry: %w", err)
	}
	proposal, replay, err := w.store.Create(ctx, owner, NewProposal{
		Operation: OperationRetryRegistryPromotionDocs, SchemaVersion: 1,
		Target:         ProposalTarget{OrganizationID: request.OrganizationID, ProjectID: project, ResourceKind: "registry_promotion", ResourceID: request.PromotionID.String()},
		IdempotencyKey: request.RetryKey, Arguments: args, ExpectedState: args, Preview: args,
	}, w.now())
	if err != nil {
		return ProposalOutput{}, err
	}
	return proposalOutput(proposal, replay, w.baseURL), nil
}

func storedCatalogDocsRetry(proposal Proposal) (storedDocsRetry, error) {
	var args storedDocsRetry
	if proposal.Operation != OperationRetryRegistryPromotionDocs || proposal.SchemaVersion != 1 || proposal.PlatformGlobal || proposal.Target.ResourceKind != "registry_promotion" || json.Unmarshal(proposal.Arguments, &args) != nil {
		return args, ErrProposalInvalidated
	}
	request := args.Request
	if request.OrganizationID != proposal.Target.OrganizationID || request.PromotionID == uuid.Nil || request.PromotionID.String() != proposal.Target.ResourceID || args.ProjectID != proposal.Target.ProjectID || request.RetryKey != proposal.IdempotencyKey || request.DocsRevision == "" || len(request.DocsRevision) > maxRegistryText {
		return args, ErrProposalInvalidated
	}
	digest, err := stateDigest(proposal.Arguments)
	if err != nil || !digestsEqual(digest, proposal.ExpectedStateDigest) {
		return args, ErrProposalInvalidated
	}
	return args, nil
}

func (w *catalogDocsRetryWriter) revalidate(ctx context.Context, tx pgx.Tx, proposal Proposal) error {
	args, err := storedCatalogDocsRetry(proposal)
	if err != nil {
		return err
	}
	candidate, err := w.mutator.ValidateDocsRetryTx(ctx, tx, args.Request.OrganizationID, args.Request.PromotionID, args.Request.DocsRevision)
	if err != nil {
		return docsRetryValidationError(err)
	}
	if !docsRetryMatches(candidate, args.Request) {
		return ErrStaleState
	}
	project, err := docsRetryProject(candidate)
	if err != nil || project != args.ProjectID {
		return ErrStaleState
	}
	return nil
}

func (w *catalogDocsRetryWriter) view(proposal Proposal) (proposalView, error) {
	args, err := storedCatalogDocsRetry(proposal)
	if err != nil {
		return proposalView{}, err
	}
	var preview storedDocsRetry
	if json.Unmarshal(proposal.Preview, &preview) != nil || preview != args {
		return proposalView{}, ErrProposalInvalidated
	}
	return proposalView{
		Summary:        "Retry the setup documentation request",
		PlatformGlobal: false, OrganizationID: args.Request.OrganizationID, OrganizationName: "", OrganizationSlug: "",
		Changes: []proposalViewChange{
			{Setting: "Publication", Before: "", After: args.Request.PromotionID.String()},
			{Setting: "Delivery revision", Before: "", After: args.Request.DocsRevision},
			{Setting: "Setup guide request", Before: "Needs retry", After: "Scheduled for another attempt"},
		},
		SideEffects: "Schedules another setup-docs guide request using the previously approved public metadata. The published catalogue entry is unchanged. Guide publication retains the setup-docs repository's merge review.",
	}, nil
}

func (w *catalogDocsRetryWriter) execution(authority writeAuthority) ProposalExecution {
	return ProposalExecution{Lock: nil, AfterCommit: nil, Run: func(ctx context.Context, tx pgx.Tx, proposal Proposal) (string, json.RawMessage, error) {
		if err := w.revalidate(ctx, tx, proposal); err != nil {
			return "", nil, err
		}
		args, err := storedCatalogDocsRetry(proposal)
		if err != nil {
			return "", nil, err
		}
		ctx, principal, _ := staffMutation(ctx, authority)
		candidate, err := w.mutator.RetryDocsTx(ctx, tx, args.Request, principal.String())
		if err != nil {
			return "", nil, errors.New("guide retry could not be committed")
		}
		if candidate.ID != args.Request.PromotionID.String() || candidate.OrganizationID != args.Request.OrganizationID || candidate.Status != "promoted" || (candidate.DocsStatus != "pending" && candidate.DocsStatus != "retrying") {
			return "", nil, errors.New("guide retry receipt is unavailable")
		}
		result, err := json.Marshal(catalogDocsRetryReceipt{PromotionID: candidate.ID, DocsStatus: candidate.DocsStatus})
		if err != nil {
			return "", nil, fmt.Errorf("encode guide retry receipt: %w", err)
		}
		return "succeeded", result, nil
	}}
}

func (w *catalogDocsRetryWriter) registerPrepare(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Meta: nil, InputSchema: nil, OutputSchema: nil, Icons: nil,
		Name: "prepare_registry_promotion_docs_retry", Title: "Prepare Setup Documentation Retry",
		Description: "Prepare a retry for an exact promoted catalogue entry whose docs_retry_allowed is true. Use its canonical organization, promotion UUID and current docs revision from list_registry_promotion_candidates. Works for retained publications after source deletion. Creates a stored preview for same-staff browser approval; execute_admin_proposal schedules delivery. Requires admin:write. Does not request the guide during preparation.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: new(false), IdempotentHint: true, OpenWorldHint: new(false), Title: ""},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input PrepareCatalogDocsRetryInput) (*mcp.CallToolResult, ProposalOutput, error) {
		out, err := w.prepare(ctx, input)
		return nil, out, err
	})
}

func docsRetryValidationError(err error) error {
	switch {
	case errors.Is(err, catalogpromotion.ErrStale), errors.Is(err, catalogpromotion.ErrConflict), errors.Is(err, catalogpromotion.ErrIneligible):
		return ErrStaleState
	case errors.Is(err, catalogpromotion.ErrNotFound):
		return errors.New("catalogue promotion not found")
	default:
		return errors.New("catalogue promotion retry state is unavailable")
	}
}
