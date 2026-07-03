package risk

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
)

// defaultAckGrace is the minimum window an acknowledgement keeps re-challenge
// suppressed. Even a "don't remember" ack needs enough grace for the agent to
// retry the same call once and have it pass.
const defaultAckGrace = 5 * time.Minute

// toChallengeToolName maps the hook's tool name ("" for non-tool events) to the
// nullable tool_name column.
func toChallengeToolName(toolName string) pgtype.Text {
	if toolName == "" {
		return pgtype.Text{String: "", Valid: false}
	}
	return pgtype.Text{String: toolName, Valid: true}
}

func toChallengeText(v string) pgtype.Text {
	if v == "" {
		return pgtype.Text{String: "", Valid: false}
	}
	return pgtype.Text{String: v, Valid: true}
}

// HasAcknowledgedChallenge reports whether a live acknowledgement exists for
// this (user, policy, tool). The agent retries a warn-challenged call with NO
// token, so this DB lookup — not the ack link's cache token — is what lets the
// retry through. Fail-closed: any error (including an unreachable DB) returns
// false so a warn never silently allows on infra failure.
func (s *Scanner) HasAcknowledgedChallenge(ctx context.Context, projectID uuid.UUID, userID, policyID, toolName string) bool {
	pid, err := uuid.Parse(policyID)
	if err != nil {
		return false
	}
	if userID == "" {
		return false
	}
	_, err = s.repo.GetActiveRiskPolicyAck(ctx, repo.GetActiveRiskPolicyAckParams{
		ProjectID:    projectID,
		UserID:       userID,
		RiskPolicyID: pid,
		ToolName:     toChallengeToolName(toolName),
	})
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			s.logger.WarnContext(ctx, "risk policy ack lookup failed; treating as unacknowledged",
				attr.SlogError(err), attr.SlogRiskPolicyID(policyID))
		}
		return false
	}
	return true
}

// RecordPolicyChallenge upserts the challenged-state row for a warn match.
// Log-safe only: it never receives or stores the raw matched value. Best-effort
// — a failure to record must not change the enforcement decision.
func (s *Scanner) RecordPolicyChallenge(ctx context.Context, organizationID string, projectID uuid.UUID, userID, policyID, toolName, policyName, entity, ruleID string) {
	pid, err := uuid.Parse(policyID)
	if err != nil || userID == "" || organizationID == "" {
		return
	}
	if _, err := s.repo.UpsertRiskPolicyChallenge(ctx, repo.UpsertRiskPolicyChallengeParams{
		ID:             uuid.New(),
		OrganizationID: organizationID,
		ProjectID:      projectID,
		RiskPolicyID:   pid,
		UserID:         userID,
		ToolName:       toChallengeToolName(toolName),
		PolicyName:     toChallengeText(policyName),
		Entity:         toChallengeText(entity),
		RuleID:         toChallengeText(ruleID),
	}); err != nil {
		s.logger.WarnContext(ctx, "failed to record risk policy challenge",
			attr.SlogError(err), attr.SlogRiskPolicyID(policyID))
	}
}

// AcknowledgeRiskPolicyChallenge redeems a warn/challenge ack link. Self-service:
// the same user who was warned confirms and the challenge is recorded as
// acknowledged, so the retried action proceeds. No admin approval, no RBAC grant.
func (s *Service) AcknowledgeRiskPolicyChallenge(ctx context.Context, payload *gen.AcknowledgeRiskPolicyChallengePayload) (*gen.AcknowledgeRiskPolicyChallengeResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	record, err := lookupPolicyAckRecord(ctx, s.cache, payload.AckToken)
	if err != nil {
		// Cache unreachable is an infra failure, not a bad link.
		if errors.Is(err, errPolicyAckStoreUnavailable) {
			return nil, oops.E(oops.CodeUnexpected, err, "load risk policy challenge").LogError(ctx, s.logger)
		}
		return nil, oops.E(oops.CodeInvalid, err, "invalid or expired risk policy challenge token")
	}

	// Bind redemption to the caller: the token is a bearer reference, so a
	// leaked link cannot be cashed by another org or user.
	if record.OrganizationID != authCtx.ActiveOrganizationID {
		return nil, oops.C(oops.CodeForbidden)
	}
	if record.UserID != authCtx.UserID {
		return nil, oops.C(oops.CodeForbidden)
	}

	projectID, err := uuid.Parse(record.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid risk policy challenge project id")
	}
	policyID, err := uuid.Parse(record.RiskPolicyID)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid risk policy challenge policy id")
	}

	window := record.RememberFor
	if window <= 0 {
		window = defaultAckGrace
	}
	expiresAt := time.Now().Add(window)

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin risk policy challenge ack").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	if _, err := projectsrepo.New(dbtx).GetProjectByIDAndOrganizationID(ctx, projectsrepo.GetProjectByIDAndOrganizationIDParams{
		ID:             projectID,
		OrganizationID: record.OrganizationID,
	}); err != nil {
		return nil, oops.E(oops.CodeNotFound, err, "project not found").LogError(ctx, s.logger)
	}

	q := repo.New(dbtx)
	row, err := q.MarkRiskPolicyChallengeAcknowledged(ctx, repo.MarkRiskPolicyChallengeAcknowledgedParams{
		ID:             uuid.New(),
		OrganizationID: record.OrganizationID,
		ProjectID:      projectID,
		RiskPolicyID:   policyID,
		UserID:         record.UserID,
		ToolName:       toChallengeToolName(ptrValOr(record.ToolName)),
		PolicyName:     toChallengeText(record.PolicyName),
		ExpiresAt:      pgtype.Timestamptz{Time: expiresAt, Valid: true},
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "acknowledge risk policy challenge").LogError(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit risk policy challenge ack").LogError(ctx, s.logger)
	}

	// One-shot: drop the cache token so the link can't be replayed (TTL also bounds it).
	invalidatePolicyAckToken(ctx, s.cache, payload.AckToken)

	// TODO(epic-g audit): add a first-class audit event
	// (audit.ActionRiskPolicyChallengeAcknowledge) mirroring the bypass-request
	// audit machinery. Interim: structured log carrying only log-safe fields.
	s.logger.InfoContext(ctx, "risk policy challenge acknowledged",
		attr.SlogRiskPolicyID(policyID.String()),
		attr.SlogUserID(authCtx.UserID),
	)
	_ = audit.ActionRiskPolicyChallengeAcknowledge

	name := row.PolicyName.String
	exp := expiresAt.UTC().Format(time.RFC3339)
	return &gen.AcknowledgeRiskPolicyChallengeResult{
		Acknowledged: true,
		PolicyName:   &name,
		ExpiresAt:    &exp,
	}, nil
}

func ptrValOr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
