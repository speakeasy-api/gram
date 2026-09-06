package mcpapproval

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_approval"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// DecisionVersionState is the complete request state an optimistic decision
// version binds. It is populated only after the request row is locked.
type DecisionVersionState struct {
	RequestID           uuid.UUID `json:"request_id"`
	Status              string    `json:"status"`
	UpdatedAt           time.Time `json:"updated_at"`
	EvidenceVersion     int32     `json:"evidence_version"`
	EvidenceCollectedAt time.Time `json:"evidence_collected_at"`
	EvidenceChangedAt   time.Time `json:"evidence_changed_at"`
	LatestDecisionID    uuid.UUID `json:"latest_decision_id"`
	LatestDecision      string    `json:"latest_decision"`
	LatestDecisionAt    time.Time `json:"latest_decision_at"`
}

// DecisionCommandInput contains only server-resolved identities. Transport
// adapters must decode opaque references before calling the command.
type DecisionCommandInput struct {
	OrganizationID       string
	ProjectID            uuid.UUID
	RequestID            uuid.UUID
	Decision             string
	Rationale            string
	GrantedPrincipalURNs []string
	ResearchReportID     uuid.NullUUID
	ActorUserID          string
	ActorEmail           *string
	ValidateLocked       func(DecisionVersionState) error
}

// ResolveDecisionTarget resolves one exact project target without returning its
// raw value. Cross-organization targets are indistinguishable from missing.
func (s *Service) ResolveDecisionTarget(ctx context.Context, organizationID string, projectID uuid.UUID, targetKind, targetKey string) (uuid.UUID, error) {
	if s == nil || s.db == nil || organizationID == "" || projectID == uuid.Nil || strings.TrimSpace(targetKey) == "" || (targetKind != targetKindServerURL && targetKind != targetKindStdioCommand) {
		return uuid.Nil, oops.E(oops.CodeBadRequest, nil, "invalid approval target")
	}
	request, err := repo.New(s.db).GetApprovalRequestByTarget(ctx, repo.GetApprovalRequestByTargetParams{ProjectID: projectID, TargetKind: targetKind, TargetKey: targetKey})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && request.OrganizationID != organizationID) {
		return uuid.Nil, oops.E(oops.CodeNotFound, nil, "approval request not found")
	}
	if err != nil {
		return uuid.Nil, oops.E(oops.CodeUnexpected, err, "error reading approval request").LogError(ctx, s.logger)
	}
	return request.ID, nil
}

// DecideInTransaction records and enforces one decision inside the caller's
// transaction. The request lock, validation callback, frozen evidence, audit,
// legacy drain, and enforcement grants share one commit boundary.
func (s *Service) DecideInTransaction(ctx context.Context, tx pgx.Tx, input DecisionCommandInput) (*gen.ApprovalDecision, error) {
	if s == nil || s.db == nil || s.audit == nil || tx == nil || input.OrganizationID == "" || input.ProjectID == uuid.Nil || input.RequestID == uuid.Nil || input.ActorUserID == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "invalid approval decision command")
	}
	if input.Decision != decisionApproved && input.Decision != decisionDenied {
		return nil, oops.E(oops.CodeBadRequest, nil, "decision must be approved or denied")
	}
	input.Rationale = strings.TrimSpace(input.Rationale)
	if input.Rationale == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "a rationale is required")
	}

	granted := append([]string{}, input.GrantedPrincipalURNs...)
	if input.Decision == decisionDenied {
		granted = []string{}
	} else if len(granted) == 0 {
		granted = []string{authz.AllUsersPrincipal().String()}
	}
	queries := repo.New(s.db).WithTx(tx)
	request, err := queries.GetApprovalRequestForDecision(ctx, repo.GetApprovalRequestForDecisionParams{ID: input.RequestID, ProjectID: input.ProjectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.E(oops.CodeNotFound, err, "approval request not found")
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error reading approval request").LogError(ctx, s.logger)
	}
	if request.OrganizationID != input.OrganizationID {
		return nil, oops.E(oops.CodeUnexpected, nil, "approval request organization mismatch").LogError(ctx, s.logger)
	}

	full, err := queries.GetApprovalRequest(ctx, repo.GetApprovalRequestParams{ID: input.RequestID, ProjectID: input.ProjectID})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error reading locked approval request state").LogError(ctx, s.logger)
	}
	decisions, err := queries.ListDecisionsForApprovalRequest(ctx, repo.ListDecisionsForApprovalRequestParams{McpApprovalRequestID: input.RequestID, ProjectID: input.ProjectID})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error reading approval decisions").LogError(ctx, s.logger)
	}
	state := DecisionVersionState{
		RequestID: input.RequestID, Status: request.Status, UpdatedAt: full.UpdatedAt.Time, EvidenceVersion: request.EvidenceVersion,
		EvidenceCollectedAt: time.Time{}, EvidenceChangedAt: time.Time{}, LatestDecisionID: uuid.Nil, LatestDecision: "", LatestDecisionAt: time.Time{},
	}
	if full.EvidenceCollectedAt.Valid {
		state.EvidenceCollectedAt = full.EvidenceCollectedAt.Time
	}
	if full.EvidenceChangedAt.Valid {
		state.EvidenceChangedAt = full.EvidenceChangedAt.Time
	}
	if len(decisions) > 0 {
		state.LatestDecisionID = decisions[0].ID
		state.LatestDecision = decisions[0].Decision
		state.LatestDecisionAt = decisions[0].DecidedAt.Time
	}
	if input.ValidateLocked != nil {
		if err := input.ValidateLocked(state); err != nil {
			return nil, err
		}
	}

	grantedPrincipals := make([]urn.Principal, 0, len(granted))
	for _, principalURN := range granted {
		principal, err := urn.ParsePrincipal(principalURN)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid granted principal urn")
		}
		if err := authz.ValidatePrincipal(ctx, tx, input.OrganizationID, principal); err != nil {
			if errors.Is(err, authz.ErrPrincipalInvalid) || errors.Is(err, authz.ErrPrincipalNotFound) {
				return nil, oops.E(oops.CodeBadRequest, err, "granted principal does not resolve in this organization")
			}
			return nil, oops.E(oops.CodeUnexpected, err, "error validating granted principal").LogError(ctx, s.logger)
		}
		grantedPrincipals = append(grantedPrincipals, principal)
	}

	if input.ResearchReportID.Valid {
		if _, err := queries.GetResearchReportForDecision(ctx, repo.GetResearchReportForDecisionParams{ID: input.ResearchReportID.UUID, OrganizationID: input.OrganizationID, McpApprovalRequestID: input.RequestID, ProjectID: input.ProjectID}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, oops.E(oops.CodeBadRequest, nil, "research report does not belong to this request")
			}
			return nil, oops.E(oops.CodeUnexpected, err, "error reading research report").LogError(ctx, s.logger)
		}
	}

	decision, err := queries.CreateApprovalDecision(ctx, repo.CreateApprovalDecisionParams{
		OrganizationID: request.OrganizationID, ProjectID: input.ProjectID, McpApprovalRequestID: input.RequestID,
		Decision: input.Decision, DecidedBy: input.ActorUserID, Rationale: pgText(&input.Rationale),
		EvidenceSnapshot: request.CurrentEvidence, EvidenceVersion: request.EvidenceVersion,
		GrantedPrincipalUrns: granted, McpResearchReportID: input.ResearchReportID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error recording decision").LogError(ctx, s.logger)
	}
	if err := s.audit.LogMCPApprovalRequestDecide(ctx, tx, audit.LogMCPApprovalRequestDecideEvent{
		OrganizationID: request.OrganizationID, ProjectID: input.ProjectID,
		Actor: urn.NewPrincipal(urn.PrincipalTypeUser, input.ActorUserID), ActorDisplayName: input.ActorEmail, ActorSlug: nil,
		RequestURN: urn.NewMCPApprovalRequest(input.RequestID), Approved: input.Decision == decisionApproved, TargetRaw: request.TargetRaw,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error auditing decision").LogError(ctx, s.logger)
	}
	if err := queries.SetApprovalRequestStatus(ctx, repo.SetApprovalRequestStatusParams{ID: input.RequestID, ProjectID: input.ProjectID, Status: statusFor[input.Decision]}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error updating approval request status").LogError(ctx, s.logger)
	}
	if err := queries.ClearApprovalRequestEvidenceChange(ctx, repo.ClearApprovalRequestEvidenceChangeParams{ID: input.RequestID, ProjectID: input.ProjectID}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error clearing evidence change flag").LogError(ctx, s.logger)
	}
	authCtx := &contextvalues.AuthContext{
		ActiveOrganizationID: input.OrganizationID, UserID: input.ActorUserID, Email: input.ActorEmail,
		ExternalUserID: "", APIKeyID: "", APIKeyName: "", OrgWidePluginHooksKey: false, SessionID: nil, ProjectID: &input.ProjectID,
		OrganizationSlug: "", AccountType: "", HasActiveSubscription: false, Whitelisted: false, ProjectSlug: nil,
		APIKeyScopes: []string{}, IsAdmin: true, SupportOrganizationID: "",
	}
	if err := s.drainLegacyBypassRequests(ctx, tx, request, input.ProjectID, input.Decision, granted, authCtx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error resolving promoted bypass request").LogError(ctx, s.logger)
	}
	if request.TargetKind == targetKindServerURL {
		if err := reconcileDecisionGrants(ctx, tx, request.OrganizationID, input.ProjectID, request.TargetKey, input.Decision == decisionApproved, grantedPrincipals); err != nil {
			if _, ok := errors.AsType[*oops.ShareableError](err); ok {
				return nil, err
			}
			return nil, oops.E(oops.CodeUnexpected, err, "error enforcing decision").LogError(ctx, s.logger)
		}
	}
	return decisionView(decision), nil
}
