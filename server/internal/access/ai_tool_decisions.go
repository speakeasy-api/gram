package access

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/agent/aitargets"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agent/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

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
	if err := s.authz.Require(ctx, authz.Check{
		Scope:        authz.ScopeOrgAdmin,
		ResourceKind: "",
		ResourceID:   organizationID,
		Dimensions:   nil,
	}); err != nil {
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

	list, err := aitargets.LoadOrganizationList(ctx, queries, organizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "read ai scan targets").LogError(ctx, s.logger)
	}
	// A decision is only meaningful for a tool the organization's catalog
	// knows: nothing would ever match a caller to an id that is not in it,
	// and a row nobody can act on is worse than a clear rejection.
	entry, known := list.Entry(targetID)
	if !known {
		return nil, oops.E(oops.CodeNotFound, nil, "%q is not an AI detection target in this organization's catalog", targetID)
	}

	// A tool Gram cannot recognize at the gateway cannot be decided about.
	// Refused rather than recorded-and-ignored for two reasons: the inventory
	// would report unreviewed whatever was stored, so the write would look
	// like it did nothing; and a block stored against a tool that later starts
	// publishing a document would silently begin enforcing months after
	// somebody set it.
	if !aitargets.Enforceable(entry.Target) {
		return nil, oops.E(oops.CodeBadRequest, nil, "%q publishes no client ID metadata document, so Gram cannot recognize it at the gateway and cannot enforce a decision about it", targetID)
	}

	before := aitargets.UnreviewedDecisionRecord(targetID)
	switch row, err := queries.GetAIToolDecisionForUpdate(ctx, agentrepo.GetAIToolDecisionForUpdateParams{
		OrganizationID: organizationID,
		TargetID:       targetID,
	}); {
	case err == nil:
		before = aitargets.DecisionRecordFromRow(row)
	case !errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeUnexpected, err, "read existing ai tool decision").LogError(ctx, s.logger)
	}

	actor := urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID)
	saved, err := queries.UpsertAIToolDecision(ctx, agentrepo.UpsertAIToolDecisionParams{
		OrganizationID: organizationID,
		TargetID:       targetID,
		Decision:       string(decision),
		Rationale:      conv.ToPGTextEmpty(rationale),
		DecidedBy:      conv.ToPGTextEmpty(actor.String()),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "save ai tool decision").LogError(ctx, s.logger)
	}
	after := aitargets.DecisionRecordFromRow(saved)

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
		Access:   aiToolAccessView(aitargets.SummarizeAccess(entry.Target, after), true),
	}, nil
}

// aiToolAccessView renders an access summary. attributed is false for the
// project-read projection, which sees the verdict but not who reached it or
// why — the same split the detection counts are held to.
func aiToolAccessView(summary aitargets.AccessSummary, attributed bool) *gen.AIToolAccessSummary {
	view := &gen.AIToolAccessSummary{
		State:       string(summary.State),
		Decision:    string(summary.Decision),
		Enforceable: summary.Enforceable,
		Rationale:   nil,
		DecidedBy:   nil,
		DecidedAt:   nil,
	}
	if !attributed {
		return view
	}
	view.Rationale = conv.PtrEmpty(summary.Record.Rationale)
	view.DecidedBy = conv.PtrEmpty(summary.Record.DecidedBy)
	view.DecidedAt = formatOptionalTimeValue(summary.Record.DecidedAt)
	return view
}
