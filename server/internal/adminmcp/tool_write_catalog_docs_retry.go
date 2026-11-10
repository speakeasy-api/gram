//nolint:exhaustruct // MCP SDK manifests intentionally use documented optional defaults.
package adminmcp

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/speakeasy-api/gram/server/internal/catalogpromotion"
)

const catalogDocsRetrySideEffects = "Schedules another setup-docs guide request using the previously approved public metadata. The published catalogue entry is unchanged. Guide publication retains the setup-docs repository's merge review."

type catalogDocsRetryMutator interface {
	ValidateDocsRetryTx(context.Context, pgx.Tx, string, uuid.UUID, string) (catalogpromotion.Candidate, error)
	RetryDocsTx(context.Context, pgx.Tx, catalogpromotion.DocsRetryRequest, string) (catalogpromotion.Candidate, error)
}

// CatalogDocsRetryInput selects one retained publication and delivery revision.
type CatalogDocsRetryInput struct {
	// OrganizationID is the canonical owner of the retained publication.
	OrganizationID string `json:"organization_id" jsonschema:"Exact canonical organization ID from the promotion"`
	// PromotionID identifies the retained publication, even after source deletion.
	PromotionID string `json:"promotion_id" jsonschema:"Exact promotion UUID from list_registry_promotion_candidates"`
	// DocsRevision binds the retry to the inspected delivery state.
	DocsRevision string `json:"docs_revision" jsonschema:"Exact docs_revision from the promotion with docs_retry_allowed true"`
}

type catalogDocsRetryWriter struct {
	runner  *writeRunner
	writes  WriteConfig
	mutator catalogDocsRetryMutator
}

type catalogDocsRetryReceipt struct {
	// PromotionID identifies the retained publication.
	PromotionID string `json:"promotion_id"`
	// DocsStatus confirms the guide request is scheduled again.
	DocsStatus string `json:"docs_status"`
	// SideEffects describes what the retry does and does not touch.
	SideEffects string `json:"side_effects"`
}

func docsRetryMatches(candidate catalogpromotion.Candidate, request catalogpromotion.DocsRetryRequest) bool {
	return candidate.ID == request.PromotionID.String() && candidate.OrganizationID == request.OrganizationID && candidate.DocsRevision == request.DocsRevision && candidate.Status == "promoted" && candidate.DocsRetryAllowed
}

func (w *catalogDocsRetryWriter) apply(ctx context.Context, input CatalogDocsRetryInput) (catalogDocsRetryReceipt, error) {
	authority, err := requireWriteAuthority(ctx, w.writes, OperationRetryRegistryPromotionDocs)
	if err != nil {
		return catalogDocsRetryReceipt{}, err
	}
	if err := checkWriteTarget(input.OrganizationID); err != nil {
		return catalogDocsRetryReceipt{}, err
	}
	if !validRegistryUUID(input.PromotionID) || !validRegistryUUID(input.DocsRevision) {
		return catalogDocsRetryReceipt{}, errors.New("provide an exact promotion UUID and current docs revision")
	}
	// The docs revision guards this tool against repeats. The retry key is only
	// the service's dashboard contract, so every call gets a fresh one.
	request := catalogpromotion.DocsRetryRequest{OrganizationID: input.OrganizationID, PromotionID: uuid.MustParse(input.PromotionID), DocsRevision: input.DocsRevision, RetryKey: uuid.NewString()}
	target := writeTarget{OrganizationID: request.OrganizationID, ResourceKind: "registry_promotion", ResourceID: request.PromotionID.String()}
	return runWrite(ctx, w.runner, authority, target, func(ctx context.Context, tx pgx.Tx) (catalogDocsRetryReceipt, error) {
		candidate, err := w.mutator.ValidateDocsRetryTx(ctx, tx, request.OrganizationID, request.PromotionID, request.DocsRevision)
		if err != nil {
			return catalogDocsRetryReceipt{}, docsRetryValidationError(err)
		}
		if !docsRetryMatches(candidate, request) || (candidate.ProjectID != "" && !validRegistryUUID(candidate.ProjectID)) {
			return catalogDocsRetryReceipt{}, ErrStaleState
		}
		ctx, principal, _ := staffMutation(ctx, authority)
		retried, err := w.mutator.RetryDocsTx(ctx, tx, request, principal.String())
		if err != nil {
			return catalogDocsRetryReceipt{}, errors.New("guide retry could not be committed")
		}
		if retried.ID != request.PromotionID.String() || retried.OrganizationID != request.OrganizationID || retried.Status != "promoted" || (retried.DocsStatus != "pending" && retried.DocsStatus != "retrying") {
			return catalogDocsRetryReceipt{}, errors.New("guide retry receipt is unavailable")
		}
		return catalogDocsRetryReceipt{PromotionID: retried.ID, DocsStatus: retried.DocsStatus, SideEffects: catalogDocsRetrySideEffects}, nil
	})
}

func (w *catalogDocsRetryWriter) register(server *mcp.Server) {
	addWriteTool(server, &mcp.Tool{
		Name: string(OperationRetryRegistryPromotionDocs), Title: "Retry Setup Documentation",
		Description: "Schedule another setup guide request for an exact promoted catalogue entry whose docs_retry_allowed is true. Use its canonical organization, promotion UUID and current docs revision from list_registry_promotion_candidates. Works for retained publications after source deletion.",
	}, w.apply)
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
