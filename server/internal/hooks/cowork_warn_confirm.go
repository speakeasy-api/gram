package hooks

// Warn & confirm for typed prompts in Claude Cowork.
//
// In Claude Code a warn on a prompt blocks it with an acknowledgement link the
// person opens, then they retry. Cowork shows a hook's block reason cut off
// after a couple of lines, as plain text, so the long link never fits and none
// is clickable. There the warning leads with a short link (<site>/ack#<token>)
// that the person copies into a browser to approve, then retries.

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/hookevents"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/risk"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// coworkConfirmGrace keeps a Cowork confirmation alive long enough for the
// confirmed call itself to pass (and an immediate retry of it).
const coworkConfirmGrace = 2 * time.Minute

// pendingChallenge names one challenged call: a prompt (no tool name) or a
// tool call, under one policy.
type pendingChallenge struct {
	PolicyID        uuid.UUID `json:"policy_id"`
	ToolName        string    `json:"tool_name"`
	CallFingerprint string    `json:"call_fingerprint"`
}

// acknowledgePendingChallenge turns a pending challenge raised within window
// into a short-lived acknowledgement and records it in the audit log, in one
// transaction. false means there was no such challenge.
func (s *Service) acknowledgePendingChallenge(ctx context.Context, ev hookevents.Event, pending pendingChallenge, window time.Duration, event string) bool {
	if s.db == nil || ev.Context.User.ID == "" || pending.CallFingerprint == "" {
		return false
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		s.logger.WarnContext(ctx, "begin warn confirmation", attr.SlogError(err))
		return false
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	row, err := riskrepo.New(dbtx).ConfirmPendingRiskPolicyChallenge(ctx, riskrepo.ConfirmPendingRiskPolicyChallengeParams{
		GraceSeconds:    coworkConfirmGrace.Seconds(),
		ProjectID:       ev.Context.ProjectID,
		UserID:          ev.Context.User.ID,
		RiskPolicyID:    pending.PolicyID,
		ToolName:        conv.ToPGTextEmpty(pending.ToolName),
		CallFingerprint: pgtype.Text{String: pending.CallFingerprint, Valid: true},
		WindowSeconds:   window.Seconds(),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// No pending challenge: the common first-send case.
		return false
	}
	if err != nil {
		s.logger.WarnContext(ctx, "confirm warn failed",
			attr.SlogError(err),
			attr.SlogRiskPolicyID(pending.PolicyID.String()),
		)
		return false
	}

	if err := s.audit.LogRiskPolicyChallengeAcknowledge(ctx, dbtx, audit.LogRiskPolicyChallengeAcknowledgeEvent{
		OrganizationID:   ev.Context.OrganizationID,
		ProjectID:        ev.Context.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, ev.Context.User.ID),
		ActorDisplayName: conv.PtrEmpty(ev.Context.User.Email),
		ActorSlug:        nil,
		RiskPolicyID:     pending.PolicyID,
		RiskPolicyName:   row.PolicyName.String,
	}); err != nil {
		s.logger.WarnContext(ctx, "audit warn confirmation", attr.SlogError(err))
		return false
	}
	if err := dbtx.Commit(ctx); err != nil {
		s.logger.WarnContext(ctx, "commit warn confirmation", attr.SlogError(err))
		return false
	}

	s.logger.InfoContext(ctx, "warn confirmed in Cowork",
		attr.SlogEvent(event),
		attr.SlogRiskPolicyID(pending.PolicyID.String()),
	)
	return true
}

// coworkWarnUserReason is the block text shown in Cowork. The short
// acknowledgement link leads, because Cowork cuts the reason off after a
// couple of lines and shows it as plain text: the person copies the link.
func coworkWarnUserReason(scanResult *risk.ScanResult, ackURL string) string {
	return fmt.Sprintf("%s Speakeasy warning: to send it anyway, approve at this link, then try again. %s", ackURL, renderWarnBody(scanResult))
}

// coworkWarnAckURL mints the short acknowledgement link for a Cowork prompt
// warning. It returns "" when no link can be made, and the prompt is then
// blocked, as a warn is everywhere without a link.
func (s *Service) coworkWarnAckURL(ctx context.Context, ev hookevents.Event, scanResult *risk.ScanResult) string {
	if s.cache == nil || ev.Context.User.ID == "" {
		return ""
	}
	siteURL := s.orgSiteURL(ctx, ev.Context.OrganizationID)
	if siteURL == nil {
		return ""
	}
	ackURL, _, err := risk.GeneratePolicyAckShortURL(ctx, s.cache, siteURL, policyAckInput(ev, scanResult, ""), 0)
	if err != nil {
		s.logger.WarnContext(ctx, "failed to generate cowork ack link", attr.SlogError(err))
		return ""
	}
	return ackURL
}
