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

const catalogCurationSideEffects = "Reject or archive hides an unpublished candidate and suspends discovery until restored. Restore returns it to review and requires fresh eligibility. Does not publish, unpublish, delete the source, or request documentation."

type catalogCurationMutator interface {
	ValidateCurationTx(context.Context, pgx.Tx, string, uuid.UUID, string, string) (catalogpromotion.Candidate, error)
	CurateTx(context.Context, pgx.Tx, catalogpromotion.CurationRequest, string) (catalogpromotion.Candidate, error)
}

// CatalogCurationInput selects an unpublished candidate and exact review revision.
type CatalogCurationInput struct {
	// OrganizationID is the canonical owner of the candidate.
	OrganizationID string `json:"organization_id" jsonschema:"Exact canonical organization ID from the candidate"`
	// PromotionID identifies the candidate.
	PromotionID string `json:"promotion_id" jsonschema:"Exact promotion UUID from list_registry_promotion_candidates"`
	// CurationRevision binds the change to the inspected review state.
	CurationRevision string `json:"curation_revision" jsonschema:"Exact curation_revision from the candidate"`
	// Action is reject, archive, or restore for an unpublished candidate.
	Action string `json:"action" jsonschema:"Exactly reject, archive, or restore; publication is never changed"`
}

type catalogCurationWriter struct {
	runner  *writeRunner
	writes  WriteConfig
	mutator catalogCurationMutator
}

type catalogCurationReceipt struct {
	// PromotionID identifies the curated candidate.
	PromotionID string `json:"promotion_id"`
	// Status confirms the committed review status.
	Status string `json:"status"`
	// SideEffects describes what the change does and does not touch.
	SideEffects string `json:"side_effects"`
}

func curationMatches(candidate catalogpromotion.Candidate, request catalogpromotion.CurationRequest) bool {
	return candidate.ID == request.PromotionID.String() && candidate.OrganizationID == request.OrganizationID && candidate.CurationRevision == request.CurationRevision && curationTransition(candidate.Status, request.Action)
}

func (w *catalogCurationWriter) apply(ctx context.Context, input CatalogCurationInput) (catalogCurationReceipt, error) {
	authority, err := requireWriteAuthority(ctx, w.writes, OperationCurateRegistryPromotionCandidate)
	if err != nil {
		return catalogCurationReceipt{}, err
	}
	if err := checkWriteTarget(input.OrganizationID); err != nil {
		return catalogCurationReceipt{}, err
	}
	if !validRegistryUUID(input.PromotionID) || !validRegistryUUID(input.CurationRevision) || curationStatus(input.Action) == "" {
		return catalogCurationReceipt{}, errors.New("provide an exact candidate UUID, current curation revision, and reject, archive, or restore action")
	}
	// The curation revision guards this tool against repeats. The retry key is
	// only the service's dashboard contract, so every call gets a fresh one.
	request := catalogpromotion.CurationRequest{OrganizationID: input.OrganizationID, PromotionID: uuid.MustParse(input.PromotionID), CurationRevision: input.CurationRevision, RetryKey: uuid.NewString(), Action: input.Action}
	target := writeTarget{OrganizationID: request.OrganizationID, ResourceKind: "registry_promotion", ResourceID: request.PromotionID.String()}
	return runWrite(ctx, w.runner, authority, target, func(ctx context.Context, tx pgx.Tx) (catalogCurationReceipt, error) {
		candidate, err := w.mutator.ValidateCurationTx(ctx, tx, request.OrganizationID, request.PromotionID, request.CurationRevision, request.Action)
		if err != nil {
			return catalogCurationReceipt{}, catalogPromotionValidationError(err)
		}
		if !curationMatches(candidate, request) || !validRegistryUUID(candidate.ProjectID) {
			return catalogCurationReceipt{}, ErrStaleState
		}
		ctx, principal, _ := staffMutation(ctx, authority)
		curated, err := w.mutator.CurateTx(ctx, tx, request, principal.String())
		if err != nil {
			return catalogCurationReceipt{}, errors.New("candidate curation could not be committed")
		}
		if curated.ID != request.PromotionID.String() || curated.OrganizationID != request.OrganizationID || curated.Status != curationStatus(request.Action) || curated.ProjectID != candidate.ProjectID {
			return catalogCurationReceipt{}, errors.New("candidate curation receipt is unavailable")
		}
		return catalogCurationReceipt{PromotionID: curated.ID, Status: curated.Status, SideEffects: catalogCurationSideEffects}, nil
	})
}

func (w *catalogCurationWriter) register(server *mcp.Server) {
	addWriteTool(server, &mcp.Tool{
		Name: string(OperationCurateRegistryPromotionCandidate), Title: "Curate Catalogue Candidate",
		Description: "Reject, archive, or restore an exact unpublished catalogue candidate using its organization, candidate UUID and current curation revision. Reject/archive hides it from the active queue until restored. Does not alter published entries.",
	}, w.apply)
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
