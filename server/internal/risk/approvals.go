package risk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/risk/approval"
	approvalrepo "github.com/speakeasy-api/gram/server/internal/risk/approval/repo"
	"github.com/speakeasy-api/gram/server/internal/risk/policycore"
)

// deletedPolicyName stands in for the name of a policy deleted after it held a
// call.
const deletedPolicyName = "Deleted policy"

// approvalViewer is what decides which approval requests a caller sees and
// which they can decide.
type approvalViewer struct {
	userID string

	// isAdmin is true only for a caller whose org admin grant RBAC actually
	// checked. A legacy API key skips RBAC, so it is never an admin here.
	isAdmin bool

	// hasSession is true for a signed-in person, the only kind of caller who
	// may decide.
	hasSession bool

	principals []string

	// policies holds the project's require_approval policies.
	policies map[uuid.UUID]policycore.Policy

	// policyNames names every project policy, so a request whose policy has
	// changed action still shows its name.
	policyNames map[uuid.UUID]string
}

func (v approvalViewer) canApprove(policyID uuid.UUID) bool {
	policy, ok := v.policies[policyID]
	return ok && approval.CanApprove(v.principals, policy.ApprovalConfig)
}

func (v approvalViewer) isRequester(row approvalrepo.RiskPolicyApproval) bool {
	return v.userID != "" && row.RequesterUserID.Valid && row.RequesterUserID.String == v.userID
}

func (v approvalViewer) canView(row approvalrepo.RiskPolicyApproval) bool {
	return v.isAdmin || v.canApprove(row.RiskPolicyID) || v.isRequester(row)
}

// visiblePolicyIDs narrows a listing for a viewer who is not an org admin.
func (v approvalViewer) visiblePolicyIDs() []uuid.UUID {
	if v.isAdmin {
		return nil
	}
	ids := make([]uuid.UUID, 0, len(v.policies))
	for id := range v.policies {
		if v.canApprove(id) {
			ids = append(ids, id)
		}
	}
	return ids
}

func (s *Service) approvalViewer(ctx context.Context, authCtx *contextvalues.AuthContext) (approvalViewer, error) {
	isAdmin, err := s.enforcedOrgAdmin(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return approvalViewer{}, err
	}
	principals, err := s.approvals.ApproverPrincipals(ctx, authCtx.ActiveOrganizationID, authCtx.UserID)
	if err != nil {
		return approvalViewer{}, oops.E(oops.CodeUnexpected, err, "resolve approver roles").LogError(ctx, s.logger)
	}
	rows, err := s.repo.ListRiskPolicies(ctx, *authCtx.ProjectID)
	if err != nil {
		return approvalViewer{}, oops.E(oops.CodeUnexpected, err, "list risk policies").LogError(ctx, s.logger)
	}
	policies := make(map[uuid.UUID]policycore.Policy, len(rows))
	policyNames := make(map[uuid.UUID]string, len(rows))
	for _, row := range rows {
		policyNames[row.ID] = row.Name
		if row.Action != policycore.ActionRequireApproval {
			continue
		}
		policies[row.ID] = policycore.Project(row, nil)
	}
	return approvalViewer{
		userID:      authCtx.UserID,
		isAdmin:     isAdmin,
		hasSession:  authCtx.SessionID != nil && authCtx.UserID != "",
		principals:  principals,
		policies:    policies,
		policyNames: policyNames,
	}, nil
}

// enforcedOrgAdmin reports whether RBAC checked the caller's org admin grant.
// Evaluate answers true when RBAC is not enforced at all, as for a legacy API
// key, and that must not widen what the caller sees.
func (s *Service) enforcedOrgAdmin(ctx context.Context, organizationID string) (bool, error) {
	enforced, err := s.authz.ShouldEnforce(ctx)
	if err != nil {
		return false, oops.E(oops.CodeUnexpected, err, "check admin access").LogError(ctx, s.logger)
	}
	if !enforced {
		return false, nil
	}
	isAdmin, err := s.authz.Evaluate(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: organizationID, Dimensions: nil})
	if err != nil {
		return false, oops.E(oops.CodeUnexpected, err, "check admin access").LogError(ctx, s.logger)
	}
	return isAdmin, nil
}

func (s *Service) requireApprovalsReader(ctx context.Context) (*contextvalues.AuthContext, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}
	return authCtx, nil
}

func (s *Service) ListRiskPolicyApprovals(ctx context.Context, payload *gen.ListRiskPolicyApprovalsPayload) (*gen.ListRiskPolicyApprovalsResult, error) {
	authCtx, err := s.requireApprovalsReader(ctx)
	if err != nil {
		return nil, err
	}
	cursor, err := conv.PtrToNullUUID(payload.Cursor)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid cursor")
	}
	viewer, err := s.approvalViewer(ctx, authCtx)
	if err != nil {
		return nil, err
	}

	// Fetch one extra row to learn whether another page follows.
	rows, err := s.approvals.List(ctx, approval.ListInput{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        *authCtx.ProjectID,
		Status:           approval.Status(conv.PtrValOr(payload.Status, "")),
		VisiblePolicyIDs: viewer.visiblePolicyIDs(),
		ViewerUserID:     authCtx.UserID,
		Cursor:           cursor,
		Limit:            payload.Limit + 1,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list approval requests").LogError(ctx, s.logger)
	}
	var nextCursor *string
	if len(rows) > int(payload.Limit) {
		rows = rows[:payload.Limit]
		nextCursor = new(rows[len(rows)-1].ID.String())
	}

	approvals := make([]*gen.RiskPolicyApproval, 0, len(rows))
	for _, row := range rows {
		approvals = append(approvals, s.riskPolicyApprovalView(viewer, row))
	}
	return &gen.ListRiskPolicyApprovalsResult{Approvals: approvals, NextCursor: nextCursor}, nil
}

func (s *Service) GetRiskPolicyApproval(ctx context.Context, payload *gen.GetRiskPolicyApprovalPayload) (*gen.RiskPolicyApproval, error) {
	authCtx, err := s.requireApprovalsReader(ctx)
	if err != nil {
		return nil, err
	}
	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid approval request id")
	}
	viewer, err := s.approvalViewer(ctx, authCtx)
	if err != nil {
		return nil, err
	}
	row, err := s.approvals.Get(ctx, authCtx.ActiveOrganizationID, *authCtx.ProjectID, id)
	if errors.Is(err, approval.ErrNotFound) {
		return nil, oops.E(oops.CodeNotFound, err, "approval request not found")
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "get approval request").LogError(ctx, s.logger)
	}
	if !viewer.canView(row) {
		return nil, oops.E(oops.CodeNotFound, nil, "approval request not found")
	}
	return s.riskPolicyApprovalView(viewer, row), nil
}

func (s *Service) DecideRiskPolicyApproval(ctx context.Context, payload *gen.DecideRiskPolicyApprovalPayload) (*gen.RiskPolicyApproval, error) {
	authCtx, err := s.requireApprovalsReader(ctx)
	if err != nil {
		return nil, err
	}
	// Only a person decides. An API key can act for an agent, so a decision
	// needs a signed-in dashboard session.
	if authCtx.SessionID == nil || authCtx.UserID == "" {
		return nil, oops.E(oops.CodeForbidden, nil, "approval requests can only be decided by a signed-in person")
	}
	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid approval request id")
	}
	// Resolve the viewer first: once the decision commits, a failure here would
	// report an error for a decision that was saved.
	viewer, err := s.approvalViewer(ctx, authCtx)
	if err != nil {
		return nil, err
	}

	decided, err := s.approvals.Decide(ctx, approval.DecideInput{
		OrganizationID:     authCtx.ActiveOrganizationID,
		ProjectID:          *authCtx.ProjectID,
		ApprovalID:         id,
		Approve:            payload.Decision == "approve",
		DeciderUserID:      authCtx.UserID,
		DeciderDisplayName: authCtx.Email,
		Via:                approval.DecidedViaDashboard,
		Note:               conv.PtrValOr(payload.Note, ""),
	})
	switch {
	case errors.Is(err, approval.ErrNotFound):
		return nil, oops.E(oops.CodeNotFound, err, "approval request not found")
	case errors.Is(err, approval.ErrNotApprover):
		return nil, oops.E(oops.CodeForbidden, err, "you are not an approver for this policy")
	case errors.Is(err, approval.ErrSelfApproval):
		return nil, oops.E(oops.CodeForbidden, err, "you cannot decide a request for your own call")
	case errors.Is(err, approval.ErrNotPending):
		return nil, oops.E(oops.CodeConflict, err, "this approval request is no longer waiting for a decision")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "decide approval request").LogError(ctx, s.logger)
	}
	return s.riskPolicyApprovalView(viewer, decided), nil
}

func (s *Service) riskPolicyApprovalView(viewer approvalViewer, row approvalrepo.RiskPolicyApproval) *gen.RiskPolicyApproval {
	status := s.approvals.EffectiveStatus(row)
	policyName := deletedPolicyName
	if name, ok := viewer.policyNames[row.RiskPolicyID]; ok {
		policyName = name
	}
	// Keep numbers as stored, so approvers see the exact value the call used.
	preview := map[string]any{}
	decoder := json.NewDecoder(bytes.NewReader(row.ArgumentsPreview))
	decoder.UseNumber()
	if err := decoder.Decode(&preview); err != nil {
		preview = map[string]any{}
	}
	return &gen.RiskPolicyApproval{
		ID:               row.ID.String(),
		PolicyID:         row.RiskPolicyID.String(),
		PolicyName:       policyName,
		PolicyVersion:    row.PolicyVersion,
		Status:           string(status),
		RequesterUrn:     row.RequesterUrn,
		Surface:          row.Surface,
		ToolsetID:        nullUUIDString(row.ToolsetID),
		ChatID:           nullUUIDString(row.ChatID),
		ToolName:         row.ToolName,
		ArgumentsPreview: preview,
		Reason:           conv.FromPGText[string](row.Reason),
		RequestedScope:   emptyToNil(approval.ScopeKind(row.RequestedScope)),
		GrantedScope:     emptyToNil(approval.ScopeKind(row.GrantedScope)),
		DecidedByUrn:     conv.FromPGText[string](row.DecidedByUrn),
		DecidedVia:       conv.FromPGText[string](row.DecidedVia),
		DecisionNote:     conv.FromPGText[string](row.DecisionNote),
		CanDecide:        viewer.hasSession && status == approval.StatusPending && viewer.canApprove(row.RiskPolicyID) && !viewer.isRequester(row),
		RequestedAt:      timestampString(row.RequestedAt),
		DecidedAt:        timestampString(row.DecidedAt),
		ExpiresAt:        timestampString(row.ExpiresAt),
		ConsumedAt:       timestampString(row.ConsumedAt),
		CreatedAt:        row.CreatedAt.Time.UTC().Format(time.RFC3339),
		UpdatedAt:        row.UpdatedAt.Time.UTC().Format(time.RFC3339),
	}
}

func nullUUIDString(value uuid.NullUUID) *string {
	if !value.Valid {
		return nil
	}
	return new(value.UUID.String())
}

func emptyToNil(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func timestampString(value pgtype.Timestamptz) *string {
	if !value.Valid {
		return nil
	}
	return new(value.Time.UTC().Format(time.RFC3339))
}
