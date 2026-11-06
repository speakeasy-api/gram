//nolint:exhaustruct // MCP SDK manifests intentionally use documented optional defaults.
package adminmcp

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/catalogauth"
	"github.com/speakeasy-api/gram/server/internal/catalogpromotion"
	"github.com/speakeasy-api/gram/server/internal/setupguides"
)

const catalogPromotionSideEffects = "Publishes the reviewed fields to the Speakeasy catalogue, visible under the existing catalogue rollout. Requests a public setup-docs guide unless one already exists or is in review for the guide slug. Guide publication retains the setup-docs repository's merge review. Source credentials and configuration are excluded."

// catalogPromotionMutator shares the same transactional rules as the admin UI.
type catalogPromotionMutator interface {
	GetCandidate(context.Context, catalogpromotion.Target) (catalogpromotion.Candidate, error)
	DiscoverAuth(context.Context, string, string) catalogauth.Discovery
	ValidateTx(context.Context, pgx.Tx, catalogpromotion.Target, string, string, catalogpromotion.PublicMetadata, bool) (catalogpromotion.Candidate, error)
	PromoteTx(context.Context, pgx.Tx, catalogpromotion.Request, string) (catalogpromotion.Candidate, error)
}

// CatalogPromotionInput names the exact source and independently reviewed public record.
type CatalogPromotionInput struct {
	// OrganizationID is the canonical source organization.
	OrganizationID string `json:"organization_id" jsonschema:"Exact canonical organization ID"`
	// ProjectID is the canonical source project.
	ProjectID string `json:"project_id" jsonschema:"Exact canonical project UUID"`
	// MCPServerID identifies the remote source inside that project.
	MCPServerID string `json:"mcp_server_id" jsonschema:"Exact MCP server UUID from the candidate"`
	// SourceRevision binds the reviewed configuration.
	SourceRevision string `json:"source_revision" jsonschema:"Exact current source revision from get_registry_promotion_candidate"`
	// CurationRevision binds the change to the current staff review state.
	CurationRevision string `json:"curation_revision" jsonschema:"Exact current curation_revision from the candidate"`
	// Public contains only metadata deliberately approved for public distribution.
	Public catalogpromotion.PublicMetadata `json:"public" jsonschema:"Independently reviewed public metadata; never copy customer endpoints, credentials, names or configuration"`
	// PublicMetadataConfirmed attests that these fields can be publicly distributed.
	PublicMetadataConfirmed bool `json:"public_metadata_confirmed" jsonschema:"Confirm every public field is suitable for the public catalogue and setup docs"`
	// OverrideEligibility permits promotion if health evidence does not meet the criteria.
	OverrideEligibility bool `json:"override_eligibility,omitempty" jsonschema:"Explicit staff permission to promote if health evidence is insufficient, failed, or unavailable; never bypasses source safety checks"`
}

type catalogPromotionWriter struct {
	runner  *writeRunner
	writes  WriteConfig
	mutator catalogPromotionMutator
}

// catalogPromotionReceipt identifies the committed publication without copying domain content.
type catalogPromotionReceipt struct {
	// PromotionID identifies the durable promotion record.
	PromotionID string `json:"promotion_id"`

	// RegistryEntryID identifies the published catalogue entry.
	RegistryEntryID string `json:"registry_entry_id"`

	// SetupGuide says whether the guide slug already had a setup guide.
	SetupGuide string `json:"setup_guide"`

	// SideEffects describes what publication does and does not touch.
	SideEffects string `json:"side_effects"`
}

func catalogPromotionRequest(input CatalogPromotionInput) (catalogpromotion.Request, error) {
	if err := checkWriteTarget(input.OrganizationID); err != nil {
		return catalogpromotion.Request{}, err
	}
	if !validRegistryUUID(input.ProjectID) || !validRegistryUUID(input.MCPServerID) || input.SourceRevision == "" || len(input.SourceRevision) > maxRegistryText || !input.PublicMetadataConfirmed || !validRegistryUUID(input.CurationRevision) {
		return catalogpromotion.Request{}, errors.New("provide exact project and server UUIDs, current source_revision and curation_revision values, and confirm the public metadata")
	}
	// The source and curation revisions guard this tool against repeats. The
	// retry key is only the service's dashboard contract, so every call gets a
	// fresh one.
	return catalogpromotion.Request{
		Target:         catalogpromotion.Target{OrganizationID: input.OrganizationID, ProjectID: uuid.MustParse(input.ProjectID), MCPServerID: uuid.MustParse(input.MCPServerID)},
		SourceRevision: input.SourceRevision, CurationRevision: input.CurationRevision, Public: input.Public,
		PublicMetadataConfirmed: true, OverrideEligibility: input.OverrideEligibility, RetryKey: uuid.NewString(), AuthDiscovery: catalogauth.Discovery{SupportsDCR: false, SupportsCIMD: false, AuthSetup: "", Status: ""},
	}, nil
}

func (w *catalogPromotionWriter) apply(ctx context.Context, input CatalogPromotionInput) (catalogPromotionReceipt, error) {
	authority, err := requireWriteAuthority(ctx, w.writes, OperationPromoteRegistryCandidate)
	if err != nil {
		return catalogPromotionReceipt{}, err
	}
	request, err := catalogPromotionRequest(input)
	if err != nil {
		return catalogPromotionReceipt{}, err
	}
	candidate, err := w.mutator.GetCandidate(ctx, request.Target)
	if err != nil {
		return catalogPromotionReceipt{}, catalogPromotionValidationError(err)
	}
	if (!candidate.Eligible && (!request.OverrideEligibility || !candidate.CanOverrideEligibility())) || candidate.SourceRevision != request.SourceRevision || candidate.CurationRevision != request.CurationRevision || candidate.Status != "candidate" {
		return catalogPromotionReceipt{}, ErrStaleState
	}
	// Discovery is a network call, so it stays outside the transaction.
	request.AuthDiscovery = w.mutator.DiscoverAuth(ctx, request.Public.URL, request.Public.AuthMode)
	target := writeTarget{OrganizationID: request.Target.OrganizationID, ResourceKind: "mcp_server", ResourceID: request.Target.MCPServerID.String()}
	return runWrite(ctx, w.runner, authority, target, func(ctx context.Context, tx pgx.Tx) (catalogPromotionReceipt, error) {
		if _, err := w.mutator.ValidateTx(ctx, tx, request.Target, request.SourceRevision, request.CurationRevision, request.Public, request.OverrideEligibility); err != nil {
			return catalogPromotionReceipt{}, catalogPromotionValidationError(err)
		}
		ctx, principal, _ := staffMutation(ctx, authority)
		promoted, err := w.mutator.PromoteTx(ctx, tx, request, principal.String())
		if err != nil {
			return catalogPromotionReceipt{}, errors.New("promotion could not be committed")
		}
		if !validRegistryUUID(promoted.ID) || !validRegistryUUID(promoted.RegistryEntryID) {
			return catalogPromotionReceipt{}, errors.New("promotion receipt is unavailable")
		}
		return catalogPromotionReceipt{PromotionID: promoted.ID, RegistryEntryID: promoted.RegistryEntryID, SetupGuide: setupGuideNote(request.Public.DocsSlug), SideEffects: catalogPromotionSideEffects}, nil
	})
}

// setupGuideNote says whether the guide slug is already taken in the pinned
// setup-docs export, matching the dashboard review form. It is advice only:
// delivery rechecks the docs repository before requesting a guide.
func setupGuideNote(slug string) string {
	if guide, ok := setupguides.Lookup(slug); ok {
		return guide.Title + " already uses this slug. Publishing links to that guide and requests no new one; keep this slug only for the same provider."
	}
	return "None in the setup guides released to Speakeasy (" + setupguides.PinnedVersion + "). A guide is requested unless delivery finds one."
}

func (w *catalogPromotionWriter) register(server *mcp.Server) {
	addWriteTool(server, &mcp.Tool{
		Name: string(OperationPromoteRegistryCandidate), Title: "Promote Catalogue Candidate",
		Description: "Publish a remote MCP source to the catalogue using exact organization, project, server and current source and curation revisions. Supply independently reviewed public metadata only. Set override_eligibility only for explicit staff permission to promote if health evidence is insufficient, failed, or unavailable; source safety checks still apply. Requests a setup guide unless one already exists or is in review; check the guide slug with the user first, because reusing a taken slug links to that existing guide.",
	}, w.apply)
}

func catalogPromotionValidationError(err error) error {
	switch {
	case errors.Is(err, catalogpromotion.ErrPublicTitleTooLong):
		return catalogpromotion.ErrPublicTitleTooLong
	case errors.Is(err, catalogpromotion.ErrPublicDescriptionTooLong):
		return catalogpromotion.ErrPublicDescriptionTooLong
	case errors.Is(err, catalogpromotion.ErrStale), errors.Is(err, catalogpromotion.ErrNotFound), errors.Is(err, catalogpromotion.ErrConflict), errors.Is(err, catalogpromotion.ErrIneligible), errors.Is(err, catalogpromotion.ErrInvalid):
		return ErrStaleState
	default:
		return errors.New("catalogue promotion validation is temporarily unavailable; retry")
	}
}
