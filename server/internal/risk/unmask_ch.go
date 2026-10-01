package risk

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/risk/chrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	riskUnmaskRevealStateAvailable         = "available"
	riskUnmaskRevealStateEvidenceNotStored = "evidence_not_stored"
)

// unmaskRiskResultFromClickHouse serves the finding IDs used by dashboard
// listings. MCP rows read encrypted Postgres evidence. Chat-backed rows
// reconstruct plaintext from original chat data and reject mismatched bounds.
func (s *Service) unmaskRiskResultFromClickHouse(ctx context.Context, authCtx *contextvalues.AuthContext, id uuid.UUID) (*gen.RiskUnmaskResultResult, error) {
	projectID := *authCtx.ProjectID

	row, err := s.findingsCH.GetRiskFindingForUnmask(ctx, chrepo.GetRiskFindingForUnmaskParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      projectID.String(),
		ID:             id,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load risk finding").LogError(ctx, s.logger)
	}
	if row == nil {
		return nil, oops.E(oops.CodeNotFound, nil, "risk result not found")
	}

	// A deleted policy's rows linger in ClickHouse until TTL; they must not
	// be revealable.
	policyID, err := uuid.Parse(row.RiskPolicyID)
	if err != nil {
		return nil, oops.E(oops.CodeNotFound, err, "risk result not found")
	}
	visible, err := s.visiblePolicyIDs(ctx, projectID, uuid.NullUUID{UUID: policyID, Valid: true})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load risk policy").LogError(ctx, s.logger)
	}
	if len(visible) == 0 {
		return nil, oops.E(oops.CodeNotFound, nil, "risk result not found")
	}

	if row.MediationSurface != "" {
		chatID, err := uuid.Parse(row.ChatID)
		if err != nil {
			chatID = uuid.Nil
		}
		if err := s.authz.Require(ctx, authz.ChatReadCheck(chatID.String())); err != nil {
			return nil, err
		}
		if row.MatchLen == 0 {
			return nil, oops.E(oops.CodeNotFound, nil, "risk result has no revealable match content")
		}
		if s.findingEvidence == nil {
			return evidenceNotStoredResult(row.ID), nil
		}
		match, err := s.findingEvidence.Reveal(ctx, authCtx.ActiveOrganizationID, projectID, row.ID, time.Now().UTC())
		if errors.Is(err, ErrMCPFindingEvidenceNotStored) {
			return evidenceNotStoredResult(row.ID), nil
		}
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "load MCP finding evidence").LogError(ctx, s.logger)
		}
		return s.finishRiskResultUnmask(ctx, authCtx, row.ID, chatID, match)
	}

	reveal := NewRevealMatcher(s.logger, s.repo, s.assetStorage)
	anchor := reveal.LoadAnchor(ctx, projectID, row)

	// The chat:read gate runs on the ingest-stamped chat id, falling back to
	// the anchored Postgres row's chat. When neither resolves (attribution
	// never resolved and the anchor is gone) the check runs against the nil
	// UUID, so only a wildcard chat:read grant passes. A stamped id
	// that disagrees with the anchor's chat is refused outright: serving the
	// anchor's content under the stamped chat's grant would hand a caller
	// another chat's transcript.
	chatID, attributed := ResolveChatID(row, anchor)
	if !attributed {
		s.logger.WarnContext(ctx, "risk finding chat id diverges from its anchor; refusing reveal",
			attr.SlogChatID(row.ChatID),
			attr.SlogValueString(row.ID.String()),
		)
		return nil, oops.E(oops.CodeNotFound, nil, "risk result not found")
	}

	if err := s.authz.Require(ctx, authz.ChatReadCheck(chatID.String())); err != nil {
		return nil, err
	}

	reveal.HydratePartContent(ctx, &anchor)

	if row.MatchLen == 0 {
		// Findings without match content (judge verdicts, dead-lettered
		// scans) have nothing to reveal; refusing beats returning "".
		return nil, oops.E(oops.CodeNotFound, nil, "risk result has no revealable match content")
	}
	match, ok := MatchingReconstruction(row.MatchLen, reveal.Candidates(ctx, chatID, row, anchor))
	if !ok {
		return nil, oops.E(oops.CodeNotFound, nil, "risk result content is no longer available")
	}

	return s.finishRiskResultUnmask(ctx, authCtx, row.ID, chatID, match)
}

func evidenceNotStoredResult(id uuid.UUID) *gen.RiskUnmaskResultResult {
	return &gen.RiskUnmaskResultResult{
		ID:          id.String(),
		Match:       "",
		RevealState: riskUnmaskRevealStateEvidenceNotStored,
	}
}

func (s *Service) finishRiskResultUnmask(
	ctx context.Context,
	authCtx *contextvalues.AuthContext,
	id uuid.UUID,
	chatID uuid.UUID,
	match string,
) (*gen.RiskUnmaskResultResult, error) {
	projectID := *authCtx.ProjectID
	if err := s.audit.LogRiskResultUnmask(ctx, s.db, audit.LogRiskResultUnmaskEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        projectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		RiskResultID:     id,
		ChatID:           chatID,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "record risk result unmask audit log").LogError(ctx, s.logger)
	}

	return &gen.RiskUnmaskResultResult{
		ID:          id.String(),
		Match:       match,
		RevealState: riskUnmaskRevealStateAvailable,
	}, nil
}
