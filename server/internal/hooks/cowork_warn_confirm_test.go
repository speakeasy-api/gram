package hooks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/risk"
	riskRepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

// dbChallengeScanner warns on every prompt and keeps challenges and
// acknowledgements in the real table, as the production scanner does.
type dbChallengeScanner struct {
	stubResultScanner
	repo     *riskRepo.Queries
	policyID uuid.UUID
}

func (s *dbChallengeScanner) ScanForEnforcement(_ context.Context, req risk.RealtimeScanRequest) (*risk.ScanResult, error) {
	sum := sha256.Sum256([]byte(req.Text))
	message := "%{match} looks like sensitive information."
	return &risk.ScanResult{
		Action:          "warn",
		PolicyID:        s.policyID.String(),
		PolicyName:      "Government IDs (warn)",
		Description:     "matched US_SSN",
		Entity:          "US_SSN",
		MatchedValue:    "618-27-4093",
		UserMessage:     &message,
		CallFingerprint: hex.EncodeToString(sum[:]),
	}, nil
}

func (s *dbChallengeScanner) HasAcknowledgedChallenge(ctx context.Context, projectID uuid.UUID, userID, _, toolName, callFingerprint string) bool {
	_, err := s.repo.GetActiveRiskPolicyAck(ctx, riskRepo.GetActiveRiskPolicyAckParams{
		ProjectID:       projectID,
		UserID:          userID,
		RiskPolicyID:    s.policyID,
		ToolName:        conv.ToPGTextEmpty(toolName),
		CallFingerprint: pgtype.Text{String: callFingerprint, Valid: true},
	})
	return err == nil
}

func (s *dbChallengeScanner) RecordPolicyChallenge(ctx context.Context, organizationID string, projectID uuid.UUID, userID, _, toolName, policyName, entity, _, callFingerprint string) {
	_, _ = s.repo.UpsertRiskPolicyChallenge(ctx, riskRepo.UpsertRiskPolicyChallengeParams{
		ID:              uuid.New(),
		OrganizationID:  organizationID,
		ProjectID:       projectID,
		RiskPolicyID:    s.policyID,
		UserID:          userID,
		ToolName:        conv.ToPGTextEmpty(toolName),
		CallFingerprint: pgtype.Text{String: callFingerprint, Valid: true},
		PolicyName:      pgtype.Text{String: policyName, Valid: true},
		Entity:          pgtype.Text{String: entity, Valid: true},
		RuleID:          pgtype.Text{String: "", Valid: false},
	})
}

// newCoworkWarnTest returns a hooks service whose scanner warns on every scan
// and keeps challenges in the real table.
func newCoworkWarnTest(t *testing.T) (context.Context, *testInstance, *contextvalues.AuthContext) {
	t.Helper()

	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	ti.service.productFeatures = alwaysEnabledFeatures{}

	policyID, err := uuid.NewV7()
	require.NoError(t, err)
	_, err = riskRepo.New(ti.conn).CreateRiskPolicy(ctx, riskRepo.CreateRiskPolicyParams{
		ID:             policyID,
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: authCtx.ActiveOrganizationID,
		Name:           "Government IDs (warn)",
		Sources:        []string{"presidio"},
		Enabled:        true,
		Action:         "warn",
		AudienceType:   "everyone",
		AutoName:       false,
	})
	require.NoError(t, err)
	ti.service.riskScanner = &dbChallengeScanner{stubResultScanner: stubResultScanner{}, repo: riskRepo.New(ti.conn), policyID: policyID}
	return ctx, ti, authCtx
}

// TestIngest_CoworkPromptWarnLeadsWithShortLink: Cowork cuts a hook's block
// text off after a couple of lines, so a warned prompt leads with a short
// acknowledgement link. Approving it lets the retry through, even a minute
// later, when Cowork's "Message sent at" reminder has changed.
func TestIngest_CoworkPromptWarnLeadsWithShortLink(t *testing.T) {
	t.Parallel()

	ctx, ti, authCtx := newCoworkWarnTest(t)

	sessionID := uuid.NewString()
	surface := surfaceCowork
	send := func(text string) *gen.IngestHookResult {
		t.Helper()
		payload := canonicalIngestPayload("claude", "prompt.submitted", sessionID)
		idempotencyKey := uuid.NewString()
		payload.IdempotencyKey = &idempotencyKey
		payload.Session.Surface = &surface
		promptID := uuid.NewString()
		payload.Session.TurnID = &promptID
		payload.Data = &gen.HookIngestData{Prompt: &gen.HookPromptData{Text: &text}}
		res, err := ti.service.IngestAuthenticated(ctx, authCtx, payload)
		require.NoError(t, err)
		return res
	}
	sentAt := func(minute int) string {
		return fmt.Sprintf("<system-reminder>Message sent at Thu 2026-11-12 13:%02d local time.</system-reminder>\nSort these employees: Avery Lindqvist 618-27-4093", minute)
	}

	first := send(sentAt(36))
	require.Equal(t, "deny", first.Decision, "the first send is challenged")
	require.NotNil(t, first.Message)
	require.Regexp(t, `^https?://\S+/ack#[0-9A-Za-z]{10} Speakeasy warning:`, *first.Message, "the short acknowledgement link leads, before Cowork cuts the text off")
	require.Contains(t, *first.Message, "618-27-4093 looks like sensitive information.")
	require.NotContains(t, *first.Message, "ack_token=", "Cowork gets the short link, not the long one")
	require.NotContains(t, *first.Message, "\x1b", "no terminal escapes in a web UI")

	// The person approves at the link, which marks the challenge acknowledged.
	scanner, ok := ti.service.riskScanner.(*dbChallengeScanner)
	require.True(t, ok)
	_, err := riskRepo.New(ti.conn).MarkRiskPolicyChallengeAcknowledged(ctx, riskRepo.MarkRiskPolicyChallengeAcknowledgedParams{
		ID:              uuid.New(),
		OrganizationID:  authCtx.ActiveOrganizationID,
		ProjectID:       *authCtx.ProjectID,
		RiskPolicyID:    scanner.policyID,
		UserID:          authCtx.UserID,
		ToolName:        pgtype.Text{String: "", Valid: false},
		CallFingerprint: pgtype.Text{String: risk.CoworkPromptCallFingerprint(sentAt(36)), Valid: true},
		PolicyName:      pgtype.Text{String: "Government IDs (warn)", Valid: true},
		ExpiresAt:       pgtype.Timestamptz{Time: time.Now().Add(time.Minute), Valid: true},
	})
	require.NoError(t, err)

	require.Equal(t, "allow", send(sentAt(37)).Decision, "the retry a minute later matches the approved prompt")
	require.Equal(t, "deny", send(sentAt(37)+" and 472-81-3365").Decision, "a different prompt is challenged on its own")
}

// TestIngest_CoworkPromptWarnConfirmedByResend: the first send of a warned
// prompt in Cowork is challenged with the short link and a "send it again"
// instruction, and the identical resend goes through.
func TestIngest_CoworkPromptWarnConfirmedByResend(t *testing.T) {
	t.Parallel()

	ctx, ti, authCtx := newCoworkWarnTest(t)

	sessionID := uuid.NewString()
	surface := surfaceCowork
	sendKeyed := func(text, idempotencyKey string) *gen.IngestHookResult {
		t.Helper()
		payload := canonicalIngestPayload("claude", "prompt.submitted", sessionID)
		payload.IdempotencyKey = &idempotencyKey
		payload.Session.Surface = &surface
		promptID := uuid.NewString()
		payload.Session.TurnID = &promptID
		payload.Data = &gen.HookIngestData{Prompt: &gen.HookPromptData{Text: &text}}
		res, err := ti.service.IngestAuthenticated(ctx, authCtx, payload)
		require.NoError(t, err)
		return res
	}
	send := func(text string) *gen.IngestHookResult {
		t.Helper()
		return sendKeyed(text, uuid.NewString())
	}

	// A transport retry reuses the challenged send's idempotency key. It gets
	// the same challenge and does not count as the person sending again.
	retried := "Payroll check: Mira Okonkwo 305-64-7182"
	key := uuid.NewString()
	require.Equal(t, "deny", sendKeyed(retried, key).Decision)
	require.Equal(t, "deny", sendKeyed(retried, key).Decision, "a retried delivery does not confirm")

	prompt := "Sort these employees: Avery Lindqvist 618-27-4093"
	first := send(prompt)
	require.Equal(t, "deny", first.Decision, "the first send is challenged")
	require.NotNil(t, first.Message)
	require.Regexp(t, `^https?://\S+/ack#[0-9A-Za-z]{10} Speakeasy warning:`, *first.Message, "the short acknowledgement link leads, before Cowork cuts the text off")
	require.Contains(t, *first.Message, "send the same message again")
	require.Contains(t, *first.Message, "618-27-4093 looks like sensitive information.")
	require.NotContains(t, *first.Message, "ack_token=", "Cowork gets the short link, not the long one")
	require.NotContains(t, *first.Message, "\x1b", "no terminal escapes in a web UI")

	require.Equal(t, "allow", send(prompt).Decision, "resending the same prompt confirms the warning")
	require.Equal(t, "allow", send(prompt).Decision, "the confirmation holds for an immediate retry")
	require.Equal(t, "deny", send(prompt+" and 472-81-3365").Decision, "a different prompt is challenged on its own")

	// Claude's apps put the minute a message was sent in a reminder before the
	// prompt, so a resend a minute later differs only there and still confirms.
	minute := func(m int) string {
		return fmt.Sprintf("<system-reminder>Message sent at Thu 2026-11-12 13:%02d local time.</system-reminder>\nCheck Priya Okafor 731-52-8806", m)
	}
	require.Equal(t, "deny", send(minute(36)).Decision)
	require.Equal(t, "allow", send(minute(37)).Decision, "a resend in the next minute is the same prompt")

	// A resend after the window is a fresh first send, so it is challenged.
	late := "Sort these too: Noah Castellanos 472-81-3365"
	require.Equal(t, "deny", send(late).Decision)
	require.NoError(t, testrepo.New(ti.conn).BackdateRiskPolicyChallenges(ctx, testrepo.BackdateRiskPolicyChallengesParams{
		Seconds:   (coworkResendWindow + time.Minute).Seconds(),
		ProjectID: *authCtx.ProjectID,
	}))
	require.Equal(t, "deny", send(late).Decision, "a resend after the window does not confirm")
	require.Equal(t, "allow", send(late).Decision, "the challenge it raised confirms the next resend")

	audited, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRiskPolicyChallengeAcknowledge)
	require.NoError(t, err)
	require.EqualValues(t, 3, audited, "each confirmation is audited once")
}

// TestIngest_CoworkToolWarnAsksAndConfirms: in Cowork a warned tool call is
// answered with "ask", so Cowork shows its own Allow / Deny prompt. The call
// only runs if the person allows it, so its result confirms the warning, and
// that confirmation is audited once.
func TestIngest_CoworkToolWarnAsksAndConfirms(t *testing.T) {
	t.Parallel()

	ctx, ti, authCtx := newCoworkWarnTest(t)

	sessionID := uuid.NewString()
	surface := surfaceCowork
	toolEvent := func(eventType, toolID, command string) *gen.IngestPayload {
		t.Helper()
		payload := canonicalIngestPayload("claude", eventType, sessionID)
		payload.Session.Surface = &surface
		name := "Bash"
		payload.Data = &gen.HookIngestData{ToolCall: &gen.HookToolCallData{ID: &toolID, Name: &name, Input: map[string]any{"command": command}}}
		return payload
	}
	send := func(payload *gen.IngestPayload) *gen.IngestHookResult {
		t.Helper()
		res, err := ti.service.IngestAuthenticated(ctx, authCtx, payload)
		require.NoError(t, err)
		return res
	}
	audited := func() int64 {
		t.Helper()
		n, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRiskPolicyChallengeAcknowledge)
		require.NoError(t, err)
		return n
	}

	command := "echo 618-27-4093 >> notes.txt"
	asked := send(toolEvent("tool.requested", "toolu_1", command))
	require.Equal(t, "ask", asked.Decision, "Cowork asks the person instead of showing a link it would cut off")
	require.NotNil(t, asked.Message)
	require.Contains(t, *asked.Message, "Speakeasy warning. 618-27-4093 looks like sensitive information.")
	require.NotContains(t, *asked.Message, "ack_token=")
	require.Zero(t, audited(), "asking is not a confirmation")

	send(toolEvent("tool.completed", "toolu_1", command))
	require.EqualValues(t, 1, audited(), "the call ran, so the person allowed it")
	send(toolEvent("tool.completed", "toolu_1", command))
	require.EqualValues(t, 1, audited(), "a second result for the same call confirms nothing new")
	send(toolEvent("tool.completed", "toolu_9", "ls"))
	require.EqualValues(t, 1, audited(), "a call that was never asked confirms nothing")

	require.Equal(t, "allow", send(toolEvent("tool.requested", "toolu_2", command)).Decision, "the confirmation holds for an immediate repeat")
	require.Equal(t, "ask", send(toolEvent("tool.requested", "toolu_3", "echo 472-81-3365")).Decision, "a different call is asked on its own")
	send(toolEvent("tool.failed", "toolu_3", "echo 472-81-3365"))
	require.EqualValues(t, 2, audited(), "a call that ran and then failed was still allowed by the person")
}
