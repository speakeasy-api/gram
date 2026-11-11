package hooks

// Warn & confirm for typed prompts in Claude Cowork.
//
// In Claude Code a warn on a prompt blocks it with an acknowledgement link the
// person opens, then they retry. Cowork shows a hook's block reason truncated
// to a couple of lines with no way to expand it, so the link is never visible
// and a retry is challenged again forever. There, sending the same prompt again
// is the confirmation: only a person can submit a prompt (the agent cannot
// resend one), the warning was on screen when they did, and the challenge is
// scoped to that exact prompt (call fingerprint) for a short window.

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/hookevents"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/risk"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	// coworkResendWindow bounds how long after a challenge an identical
	// resend still counts as the confirmation.
	coworkResendWindow = 10 * time.Minute
	// coworkResendGrace keeps the acknowledgement alive long enough for the
	// confirmed prompt itself to pass (and an immediate retry of it).
	coworkResendGrace = 2 * time.Minute
)

// coworkResendConfirms reports whether prompt warns in this session are
// confirmed by resending the prompt. The transcript-capture flag marks Claude
// Cowork sessions, the surface that cannot show the acknowledgement link.
func coworkResendConfirms(payload *gen.IngestPayload) bool {
	return transcriptCaptured(payload)
}

// confirmWarnByResend turns a recent pending challenge for this exact prompt
// into an acknowledgement and records it in the audit log. false means this is
// the first sighting (or the window passed), so the caller challenges it.
func (s *Service) confirmWarnByResend(ctx context.Context, ev hookevents.Event, scanResult *risk.ScanResult) bool {
	ctx, span := s.tracer.Start(ctx, "hooks.confirmWarnByResend")
	defer span.End()

	if s.db == nil || scanResult == nil || ev.Context.User.ID == "" || scanResult.CallFingerprint == "" {
		return false
	}
	policyID, err := uuid.Parse(scanResult.PolicyID)
	if err != nil {
		return false
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		s.logger.WarnContext(ctx, "begin warn resend confirmation", attr.SlogError(err))
		return false
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	row, err := riskrepo.New(dbtx).ConfirmRiskPolicyChallengeByResend(ctx, riskrepo.ConfirmRiskPolicyChallengeByResendParams{
		GraceSeconds:    coworkResendGrace.Seconds(),
		ProjectID:       ev.Context.ProjectID,
		UserID:          ev.Context.User.ID,
		RiskPolicyID:    policyID,
		ToolName:        pgtype.Text{String: "", Valid: false},
		CallFingerprint: pgtype.Text{String: scanResult.CallFingerprint, Valid: true},
		WindowSeconds:   coworkResendWindow.Seconds(),
	})
	if err != nil {
		// No pending challenge (pgx.ErrNoRows) is the common first-send case.
		return false
	}

	if err := s.audit.LogRiskPolicyChallengeAcknowledge(ctx, dbtx, audit.LogRiskPolicyChallengeAcknowledgeEvent{
		OrganizationID:   ev.Context.OrganizationID,
		ProjectID:        ev.Context.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, ev.Context.User.ID),
		ActorDisplayName: conv.PtrEmpty(ev.Context.User.Email),
		ActorSlug:        nil,
		RiskPolicyID:     policyID,
		RiskPolicyName:   row.PolicyName.String,
	}); err != nil {
		s.logger.WarnContext(ctx, "audit warn resend confirmation", attr.SlogError(err))
		return false
	}
	if err := dbtx.Commit(ctx); err != nil {
		s.logger.WarnContext(ctx, "commit warn resend confirmation", attr.SlogError(err))
		return false
	}

	s.logger.InfoContext(ctx, "warn confirmed by resending the prompt",
		attr.SlogEvent("hooks_cowork_warn_resend_confirmed"),
		attr.SlogRiskPolicyID(policyID.String()),
	)
	return true
}

// coworkWarnUserReason is the block text shown in Cowork. The confirmation
// instruction leads, because Cowork cuts the reason off after a couple of
// lines and the instruction is the part the person must not miss.
func coworkWarnUserReason(scanResult *risk.ScanResult) string {
	return fmt.Sprintf("Speakeasy warning. To send it anyway, send the same message again. %s", renderWarnBody(scanResult))
}
