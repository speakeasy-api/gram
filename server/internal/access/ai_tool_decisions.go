package access

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/agent/aitargets"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agent/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// removedAIToolMessage refuses a decision about a product removed from the catalog when no decision is on file.
const removedAIToolMessage = "This AI client was removed from the catalog. You can change or clear an existing decision, but not add a new one."

// SetAIToolDecision records whether a detected AI tool may reach the
// organization's MCP gateway.
//
// Org admin to write, deliberately, even though reading the inventory is
// project-scoped: the decision reaches every project's gateway and, when it
// is a block, gives that tool's users an OAuth error their client cannot
// recover from. That is not a call a project viewer makes.
func (s *Service) SetAIToolDecision(ctx context.Context, payload *gen.SetAIToolDecisionPayload) (*gen.SetAIToolDecisionResult, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnauthorized, err, "missing auth context").LogError(ctx, s.logger)
	}
	organizationID := ac.ActiveOrganizationID
	if err := s.requireLiveOrgAdmin(ctx, ac); err != nil {
		return nil, err
	}

	targetID := strings.TrimSpace(payload.TargetID)
	decision := aitargets.Decision(strings.TrimSpace(payload.Decision))
	if !decision.Valid() {
		return nil, oops.E(oops.CodeBadRequest, nil, "unknown decision %q", payload.Decision)
	}
	rationale := strings.TrimSpace(conv.PtrValOr(payload.Rationale, ""))

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin ai tool decision update").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	queries := agentrepo.New(dbtx)
	// One lock covers the catalog and the decisions on it, because they are
	// one row. Held across the checks below so a concurrent edit cannot make
	// them stale before the write lands, and so two admins deciding the same
	// undecided target cannot both audit a transition out of unreviewed.
	if err := queries.AcquireAIScanTargetsLock(ctx, organizationID); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "serialize ai tool decision update").LogError(ctx, s.logger)
	}

	list, err := aitargets.LoadOrganizationList(ctx, queries, organizationID, s.aiCatalog.SnapshotFor(ctx))
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "read ai scan targets").LogError(ctx, s.logger)
	}
	// A decision is only meaningful for a tool the organization's catalog
	// knows: nothing would ever match a caller to an id that is not in it,
	// and a row nobody can act on is worse than a clear rejection.
	entry, known := list.Entry(targetID)
	state, err := queries.GetAIScanTargetCatalogState(ctx, agentrepo.GetAIScanTargetCatalogStateParams{
		OrganizationID: organizationID,
		ID:             targetID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "read AI target catalog state").LogError(ctx, s.logger)
	}
	// A built-in the catalog no longer admits, because it was removed or its
	// documents are all disabled, is still recognized through those documents.
	// One rule covers both: its decision may be changed or cleared only while
	// a stored decision other than unreviewed exists. SummarizeAccess applies
	// it, so this check and every read agree.
	removed := state.ProductRemoved && (!known || entry.Source == aitargets.SourceDefault)
	if removed && entry.Catalog != aitargets.CatalogRemoved {
		// The pod's catalog has not caught up with the removal yet.
		row, err := queries.GetAIScanTargetForUpdate(ctx, agentrepo.GetAIScanTargetForUpdateParams{OrganizationID: organizationID, ID: targetID})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeFailedPrecondition, nil, "%s", removedAIToolMessage).LogWarn(ctx, s.logger)
		}
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "read ai tool decision").LogError(ctx, s.logger)
		}
		entry = aitargets.EntryFromRow(row)
		entry.DisplayName = state.ProductDisplayName
		entry.Source = aitargets.SourceDefault
		entry.Catalog = aitargets.CatalogRemoved
	}
	switch {
	case removed && !aitargets.SummarizeAccess(entry).Enforceable:
		return nil, oops.E(oops.CodeFailedPrecondition, nil, "%s", removedAIToolMessage).LogWarn(ctx, s.logger)
	case !removed && !known:
		return nil, oops.E(oops.CodeNotFound, nil, "%q is not an AI detection target in this organization's catalog", targetID)
	case !removed && !aitargets.SummarizeAccess(entry).Enforceable:
		// A tool Speakeasy cannot recognize at the gateway cannot be decided
		// about. Refused rather than recorded-and-ignored for two reasons: the
		// inventory would report unreviewed whatever was stored, so the write
		// would look like it did nothing; and a block stored against a tool
		// that later starts publishing a document would silently begin
		// enforcing months after somebody set it.
		return nil, oops.E(oops.CodeBadRequest, nil, "%q publishes no client ID metadata document, so Speakeasy cannot recognize it at the gateway and cannot enforce a decision about it", targetID)
	}

	productID := pgtype.Text{String: "", Valid: false}
	if !removed && entry.Source == aitargets.SourceDefault {
		if !state.HasDefinition {
			if !state.ProductExists {
				return nil, oops.E(oops.CodeUnavailable, nil, "Speakeasy cannot save a decision about this tool yet because it is not in the AI tool catalog. Try again later, or contact Speakeasy support if this continues.").LogWarn(ctx, s.logger)
			}
			productID = conv.ToPGText(targetID)
		}
	}

	before := entry.Decision

	actor := urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID)
	saved, err := queries.SetAIScanTargetStatus(ctx, agentrepo.SetAIScanTargetStatusParams{
		OrganizationID: organizationID,
		AiProductID:    productID,
		ID:             targetID,
		Status:         string(decision),
		Rationale:      conv.ToPGTextEmpty(rationale),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "save ai tool decision").LogError(ctx, s.logger)
	}
	after := aitargets.EntryFromRow(saved).Decision
	entry.Decision = after

	if err := s.audit.LogAIToolDecisionSet(ctx, dbtx, audit.LogAIToolDecisionSetEvent{
		OrganizationID:         organizationID,
		Actor:                  actor,
		ActorDisplayName:       ac.Email,
		ActorSlug:              nil,
		DecisionURN:            urn.NewAIToolDecision(organizationID, targetID),
		TargetDisplayName:      entry.DisplayName,
		DecisionSnapshotBefore: &before,
		DecisionSnapshotAfter:  &after,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log ai tool decision change").LogError(ctx, s.logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit ai tool decision update").LogError(ctx, s.logger)
	}

	return &gen.SetAIToolDecisionResult{
		TargetID: targetID,
		Access:   mv.BuildAIToolAccessSummaryView(aitargets.SummarizeAccess(entry)),
	}, nil
}
