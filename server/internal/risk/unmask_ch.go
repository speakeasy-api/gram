package risk

import (
	"context"

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

// unmaskRiskResultFromClickHouse serves risk.unmaskResult when the listing is
// flagged onto ClickHouse: listed ids may only exist there, and ClickHouse
// never stores the raw match. The plaintext is reconstructed from the original
// chat data per the row's surface metadata (see RevealMatcher); a
// reconstruction that does not line up with the recorded match length is
// refused rather than served.
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

	reveal := NewRevealMatcher(s.logger, s.repo, s.assetStorage)
	anchor := reveal.LoadAnchor(ctx, projectID, row)

	// The chat:read gate is identical to the Postgres path: the resolved chat
	// is the ingest-stamped id, falling back to the anchored Postgres row's
	// chat. When neither resolves (attribution never resolved and the anchor is
	// gone) the check runs against the nil UUID — mirroring the Postgres path's
	// NULL chat_id — and only a wildcard chat:read grant passes. A stamped id
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

	if err := s.audit.LogRiskResultUnmask(ctx, s.db, audit.LogRiskResultUnmaskEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        projectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		RiskResultID:     row.ID,
		ChatID:           chatID,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "record risk result unmask audit log").LogError(ctx, s.logger)
	}

	return &gen.RiskUnmaskResultResult{
		ID:    row.ID.String(),
		Match: match,
	}, nil
}
