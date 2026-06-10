package risk

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/google/uuid"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	auditrepo "github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	riskPolicyBypassRequestStatusRequested = "requested"
	riskPolicyBypassRequestStatusApproved  = "approved"
	riskPolicyBypassRequestStatusDenied    = "denied"
	riskPolicyBypassRequestStatusRevoked   = "revoked"

	// PolicyBypassTargetKindShadowMCPServer identifies a Shadow MCP server target.
	PolicyBypassTargetKindShadowMCPServer = "shadow_mcp_server"
	// PolicyBypassWholePolicyTargetKey identifies a whole-policy target.
	PolicyBypassWholePolicyTargetKey = "policy"
)

func (s *Service) ListRiskPolicyBypassRequests(ctx context.Context, payload *gen.ListRiskPolicyBypassRequestsPayload) (*gen.ListRiskPolicyBypassRequestsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}
	if err := validateRiskPolicyBypassRequestStatus(payload.Status); err != nil {
		return nil, err
	}

	policyID, err := conv.PtrToNullUUID(payload.PolicyID)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid policy id")
	}
	rows, err := s.repo.ListRiskPolicyBypassRequests(ctx, repo.ListRiskPolicyBypassRequestsParams{
		ProjectID:    *authCtx.ProjectID,
		RiskPolicyID: policyID,
		Status:       conv.PtrToPGText(payload.Status),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list risk policy bypass requests").Log(ctx, s.logger)
	}

	requests := make([]*gen.RiskPolicyBypassRequest, 0, len(rows))
	for _, row := range rows {
		req, err := riskPolicyBypassRequestView(row)
		if err != nil {
			return nil, err
		}
		requests = append(requests, req)
	}

	return &gen.ListRiskPolicyBypassRequestsResult{Requests: requests}, nil
}

func (s *Service) CreateRiskPolicyBypassRequest(ctx context.Context, payload *gen.CreateRiskPolicyBypassRequestPayload) (*gen.RiskPolicyBypassRequest, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if strings.TrimSpace(s.jwtSecret) == "" {
		return nil, oops.E(oops.CodeUnexpected, nil, "risk policy bypass request tokens are not configured").Log(ctx, s.logger)
	}

	claims, err := parsePolicyBypassRequestToken(s.jwtSecret, payload.RequestToken)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid risk policy bypass request token")
	}
	if claims.OrganizationID != authCtx.ActiveOrganizationID {
		return nil, oops.C(oops.CodeForbidden)
	}
	if claims.RequesterUserID != "" && claims.RequesterUserID != authCtx.UserID {
		return nil, oops.C(oops.CodeForbidden)
	}

	requestID, err := uuid.Parse(claims.ID)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid risk policy bypass request token id")
	}
	projectID, err := uuid.Parse(claims.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid risk policy bypass request project id")
	}
	policyID, err := uuid.Parse(claims.RiskPolicyID)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid risk policy bypass request policy id")
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin risk policy bypass request").Log(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	if _, err := projectsrepo.New(dbtx).GetProjectByIDAndOrganizationID(ctx, projectsrepo.GetProjectByIDAndOrganizationIDParams{
		ID:             projectID,
		OrganizationID: claims.OrganizationID,
	}); err != nil {
		return nil, oops.E(oops.CodeNotFound, err, "project not found").Log(ctx, s.logger)
	}
	q := repo.New(dbtx)
	policy, err := q.GetRiskPolicy(ctx, repo.GetRiskPolicyParams{
		ID:        policyID,
		ProjectID: projectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeNotFound, err, "risk policy not found").Log(ctx, s.logger)
	}

	target, err := riskPolicyBypassTargetFromClaims(claims)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid risk policy bypass request target")
	}
	row, err := q.UpsertRiskPolicyBypassRequest(ctx, repo.UpsertRiskPolicyBypassRequestParams{
		ID:               requestID,
		OrganizationID:   claims.OrganizationID,
		ProjectID:        projectID,
		RiskPolicyID:     policyID,
		TargetKind:       conv.ToPGTextEmpty(target.Kind),
		TargetLabel:      conv.ToPGTextEmpty(target.Label),
		TargetKey:        conv.ToPGText(target.Key),
		TargetDimensions: target.dimensions,
		RequesterUserID:  authCtx.UserID,
		RequesterEmail:   conv.ToPGTextEmpty(conv.PtrValOrEmpty(authCtx.Email, "")),
		Note:             conv.ToPGTextEmpty(strings.TrimSpace(conv.PtrValOr(claims.BlockReason, ""))),
		Status:           riskPolicyBypassRequestStatusRequested,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "create risk policy bypass request").Log(ctx, s.logger)
	}

	if err := s.logRiskPolicyBypassRequestAudit(ctx, dbtx, audit.ActionRiskPolicyBypassRequestCreate, authCtx, policy.ID, policy.ProjectID, policy.Name, nil, &row); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log risk policy bypass request create").Log(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit risk policy bypass request").Log(ctx, s.logger)
	}

	return riskPolicyBypassRequestView(row)
}

func (s *Service) ApproveRiskPolicyBypassRequest(ctx context.Context, payload *gen.ApproveRiskPolicyBypassRequestPayload) (*gen.RiskPolicyBypassRequest, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	requestID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid bypass request id")
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin risk policy bypass approval").Log(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	q := repo.New(dbtx)
	current, err := q.GetRiskPolicyBypassRequest(ctx, repo.GetRiskPolicyBypassRequestParams{
		ID:        requestID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeNotFound, err, "risk policy bypass request not found").Log(ctx, s.logger)
	}
	policy, err := q.GetRiskPolicy(ctx, repo.GetRiskPolicyParams{
		ID:        current.RiskPolicyID,
		ProjectID: current.ProjectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeNotFound, err, "risk policy not found").Log(ctx, s.logger)
	}
	principals, principalURNs, err := riskPolicyBypassGrantPrincipals(current.RequesterUserID, payload.GrantedPrincipalUrns)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid risk policy bypass grant principals")
	}
	if err := validateRiskPolicyBypassGrantPrincipals(ctx, dbtx, authCtx.ActiveOrganizationID, principals); err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid risk policy bypass grant principals")
	}
	selector, err := riskPolicyBypassGrantSelector(current)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "build risk policy bypass selector").Log(ctx, s.logger)
	}
	if current.Status == riskPolicyBypassRequestStatusApproved {
		currentPrincipals, _, err := riskPolicyBypassGrantPrincipals(current.RequesterUserID, current.GrantedPrincipalUrns)
		if err != nil {
			return nil, oops.E(oops.CodeInvalid, err, "invalid current risk policy bypass grant principals")
		}
		principalsToRevoke := riskPolicyBypassGrantPrincipalDifference(currentPrincipals, principals)
		if len(principalsToRevoke) > 0 {
			if err := authz.RevokeResourceFromPrincipals(ctx, dbtx, authz.ResourceGrant{
				Resource: authz.Resource{
					OrganizationID: authCtx.ActiveOrganizationID,
					Scope:          authz.ScopeRiskPolicyBypass,
					ResourceID:     current.RiskPolicyID.String(),
				},
				Effect:     authz.PolicyEffectAllow,
				Principals: principalsToRevoke,
				Selector:   selector,
			}); err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "revoke replaced risk policy bypass grants").Log(ctx, s.logger)
			}
		}
	}
	if err := authz.GrantResourceToPrincipals(ctx, dbtx, authz.ResourceGrant{
		Resource: authz.Resource{
			OrganizationID: authCtx.ActiveOrganizationID,
			Scope:          authz.ScopeRiskPolicyBypass,
			ResourceID:     current.RiskPolicyID.String(),
		},
		Effect:     authz.PolicyEffectAllow,
		Principals: principals,
		Selector:   selector,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "grant risk policy bypass").Log(ctx, s.logger)
	}

	row, err := q.UpdateRiskPolicyBypassRequestStatus(ctx, repo.UpdateRiskPolicyBypassRequestStatusParams{
		Status:               riskPolicyBypassRequestStatusApproved,
		DecidedBy:            conv.ToPGText(authCtx.UserID),
		GrantedPrincipalUrns: principalURNs,
		ID:                   requestID,
		ProjectID:            *authCtx.ProjectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "approve risk policy bypass request").Log(ctx, s.logger)
	}

	if err := s.logRiskPolicyBypassRequestAudit(ctx, dbtx, audit.ActionRiskPolicyBypassRequestApprove, authCtx, current.RiskPolicyID, current.ProjectID, policy.Name, &current, &row); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log risk policy bypass request approval").Log(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit risk policy bypass approval").Log(ctx, s.logger)
	}

	return riskPolicyBypassRequestView(row)
}

func (s *Service) DenyRiskPolicyBypassRequest(ctx context.Context, payload *gen.DenyRiskPolicyBypassRequestPayload) (*gen.RiskPolicyBypassRequest, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	requestID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid bypass request id")
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin risk policy bypass denial").Log(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	q := repo.New(dbtx)
	current, err := q.GetRiskPolicyBypassRequest(ctx, repo.GetRiskPolicyBypassRequestParams{
		ID:        requestID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeNotFound, err, "risk policy bypass request not found").Log(ctx, s.logger)
	}
	policy, err := q.GetRiskPolicy(ctx, repo.GetRiskPolicyParams{
		ID:        current.RiskPolicyID,
		ProjectID: current.ProjectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeNotFound, err, "risk policy not found").Log(ctx, s.logger)
	}
	row, err := q.UpdateRiskPolicyBypassRequestStatus(ctx, repo.UpdateRiskPolicyBypassRequestStatusParams{
		Status:               riskPolicyBypassRequestStatusDenied,
		DecidedBy:            conv.ToPGText(authCtx.UserID),
		GrantedPrincipalUrns: []string{},
		ID:                   requestID,
		ProjectID:            *authCtx.ProjectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeNotFound, err, "risk policy bypass request not found").Log(ctx, s.logger)
	}

	if err := s.logRiskPolicyBypassRequestAudit(ctx, dbtx, audit.ActionRiskPolicyBypassRequestDeny, authCtx, current.RiskPolicyID, current.ProjectID, policy.Name, &current, &row); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log risk policy bypass request denial").Log(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit risk policy bypass denial").Log(ctx, s.logger)
	}

	return riskPolicyBypassRequestView(row)
}

func (s *Service) RevokeRiskPolicyBypassRequest(ctx context.Context, payload *gen.RevokeRiskPolicyBypassRequestPayload) (*gen.RiskPolicyBypassRequest, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	requestID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid bypass request id")
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin risk policy bypass revocation").Log(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	q := repo.New(dbtx)
	current, err := q.GetRiskPolicyBypassRequest(ctx, repo.GetRiskPolicyBypassRequestParams{
		ID:        requestID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeNotFound, err, "risk policy bypass request not found").Log(ctx, s.logger)
	}
	policy, err := q.GetRiskPolicy(ctx, repo.GetRiskPolicyParams{
		ID:        current.RiskPolicyID,
		ProjectID: current.ProjectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeNotFound, err, "risk policy not found").Log(ctx, s.logger)
	}
	principals, _, err := riskPolicyBypassGrantPrincipals(current.RequesterUserID, current.GrantedPrincipalUrns)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid granted risk policy bypass principals")
	}
	selector, err := riskPolicyBypassGrantSelector(current)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "build risk policy bypass selector").Log(ctx, s.logger)
	}
	if err := authz.RevokeResourceFromPrincipals(ctx, dbtx, authz.ResourceGrant{
		Resource: authz.Resource{
			OrganizationID: authCtx.ActiveOrganizationID,
			Scope:          authz.ScopeRiskPolicyBypass,
			ResourceID:     current.RiskPolicyID.String(),
		},
		Effect:     authz.PolicyEffectAllow,
		Principals: principals,
		Selector:   selector,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "revoke risk policy bypass").Log(ctx, s.logger)
	}

	row, err := q.UpdateRiskPolicyBypassRequestStatus(ctx, repo.UpdateRiskPolicyBypassRequestStatusParams{
		Status:               riskPolicyBypassRequestStatusRevoked,
		DecidedBy:            conv.ToPGText(authCtx.UserID),
		GrantedPrincipalUrns: []string{},
		ID:                   requestID,
		ProjectID:            *authCtx.ProjectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "revoke risk policy bypass request").Log(ctx, s.logger)
	}

	if err := s.logRiskPolicyBypassRequestAudit(ctx, dbtx, audit.ActionRiskPolicyBypassRequestRevoke, authCtx, current.RiskPolicyID, current.ProjectID, policy.Name, &current, &row); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log risk policy bypass request revocation").Log(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit risk policy bypass revocation").Log(ctx, s.logger)
	}

	return riskPolicyBypassRequestView(row)
}

func (s *Service) logRiskPolicyBypassRequestAudit(
	ctx context.Context,
	dbtx auditrepo.DBTX,
	action audit.Action,
	authCtx *contextvalues.AuthContext,
	policyID uuid.UUID,
	projectID uuid.UUID,
	policyName string,
	beforeRow *repo.RiskPolicyBypassRequest,
	afterRow *repo.RiskPolicyBypassRequest,
) error {
	var before *audit.RiskPolicyBypassRequestSnapshot
	var err error
	if beforeRow != nil {
		before, err = riskPolicyBypassRequestSnapshot(*beforeRow)
		if err != nil {
			return err
		}
	}

	var after *audit.RiskPolicyBypassRequestSnapshot
	if afterRow != nil {
		after, err = riskPolicyBypassRequestSnapshot(*afterRow)
		if err != nil {
			return err
		}
	}

	metadata, err := riskPolicyBypassRequestAuditMetadata(beforeRow, afterRow)
	if err != nil {
		return err
	}

	event := audit.LogRiskPolicyBypassRequestEvent{
		OrganizationID:                    authCtx.ActiveOrganizationID,
		ProjectID:                         projectID,
		Actor:                             urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:                  authCtx.Email,
		ActorSlug:                         nil,
		RiskPolicyID:                      policyID,
		RiskPolicyName:                    policyName,
		PolicyBypassRequestSnapshotBefore: before,
		PolicyBypassRequestSnapshotAfter:  after,
		Metadata:                          metadata,
	}

	switch action {
	case audit.ActionRiskPolicyBypassRequestCreate:
		if err := s.audit.LogRiskPolicyBypassRequestCreate(ctx, dbtx, event); err != nil {
			return fmt.Errorf("log risk policy bypass request create: %w", err)
		}
	case audit.ActionRiskPolicyBypassRequestApprove:
		if err := s.audit.LogRiskPolicyBypassRequestApprove(ctx, dbtx, event); err != nil {
			return fmt.Errorf("log risk policy bypass request approve: %w", err)
		}
	case audit.ActionRiskPolicyBypassRequestDeny:
		if err := s.audit.LogRiskPolicyBypassRequestDeny(ctx, dbtx, event); err != nil {
			return fmt.Errorf("log risk policy bypass request deny: %w", err)
		}
	case audit.ActionRiskPolicyBypassRequestRevoke:
		if err := s.audit.LogRiskPolicyBypassRequestRevoke(ctx, dbtx, event); err != nil {
			return fmt.Errorf("log risk policy bypass request revoke: %w", err)
		}
	default:
		return fmt.Errorf("unsupported risk policy bypass request audit action %q", action)
	}

	return nil
}

func riskPolicyBypassRequestSnapshot(row repo.RiskPolicyBypassRequest) (*audit.RiskPolicyBypassRequestSnapshot, error) {
	view, err := riskPolicyBypassRequestView(row)
	if err != nil {
		return nil, err
	}

	return &audit.RiskPolicyBypassRequestSnapshot{
		ID:                   view.ID,
		PolicyID:             view.PolicyID,
		TargetKind:           view.TargetKind,
		TargetLabel:          view.TargetLabel,
		TargetKey:            view.TargetKey,
		TargetDimensions:     maps.Clone(view.TargetDimensions),
		RequesterUserID:      view.RequesterUserID,
		RequesterEmail:       view.RequesterEmail,
		Note:                 view.Note,
		Status:               view.Status,
		DecidedBy:            view.DecidedBy,
		GrantedPrincipalURNs: slices.Clone(view.GrantedPrincipalUrns),
		DecidedAt:            view.DecidedAt,
		CreatedAt:            view.CreatedAt,
		UpdatedAt:            view.UpdatedAt,
	}, nil
}

func riskPolicyBypassRequestAuditMetadata(beforeRow *repo.RiskPolicyBypassRequest, afterRow *repo.RiskPolicyBypassRequest) (*audit.RiskPolicyBypassRequestMetadata, error) {
	source := afterRow
	if source == nil {
		source = beforeRow
	}
	if source == nil {
		return nil, nil
	}

	dimensions, err := riskPolicyBypassDimensions(source.TargetDimensions)
	if err != nil {
		return nil, err
	}

	previousStatus := ""
	if beforeRow != nil {
		previousStatus = beforeRow.Status
	}

	return &audit.RiskPolicyBypassRequestMetadata{
		RequestID:            source.ID.String(),
		TargetKind:           conv.FromPGTextOrEmpty[string](source.TargetKind),
		TargetKey:            conv.FromPGTextOrEmpty[string](source.TargetKey),
		TargetDimensions:     dimensions,
		RequesterUserID:      source.RequesterUserID,
		GrantedPrincipalURNs: slices.Clone(source.GrantedPrincipalUrns),
		PreviousStatus:       previousStatus,
		CurrentStatus:        source.Status,
	}, nil
}

func validateRiskPolicyBypassRequestStatus(status *string) error {
	if status == nil || *status == "" {
		return nil
	}
	switch *status {
	case riskPolicyBypassRequestStatusRequested, riskPolicyBypassRequestStatusApproved, riskPolicyBypassRequestStatusDenied, riskPolicyBypassRequestStatusRevoked:
		return nil
	default:
		return oops.E(oops.CodeInvalid, nil, "invalid bypass request status")
	}
}

type riskPolicyBypassRequestTarget struct {
	PolicyBypassTarget
	dimensions []byte
}

func riskPolicyBypassTargetFromClaims(claims *policyBypassRequestClaims) (riskPolicyBypassRequestTarget, error) {
	evidence := shadowmcp.AccessEvidence{
		FullURL:        conv.PtrValOr(claims.ObservedFullURL, ""),
		URLHost:        conv.PtrValOr(claims.ObservedURLHost, ""),
		ServerIdentity: conv.PtrValOr(claims.ObservedServerIdentity, ""),
	}
	target := ShadowMCPPolicyBypassTarget(evidence, conv.PtrValOr(claims.ToolName, ""))
	if target == nil {
		return riskPolicyBypassRequestTarget{}, fmt.Errorf("policy bypass request target evidence is required")
	}
	if observedName := strings.TrimSpace(conv.PtrValOr(claims.ObservedName, "")); observedName != "" && target.Kind == PolicyBypassTargetKindShadowMCPServer {
		target.Label = observedName
	}

	dimensions, err := json.Marshal(target.Dimensions)
	if err != nil {
		return riskPolicyBypassRequestTarget{}, fmt.Errorf("marshal dimensions: %w", err)
	}

	return riskPolicyBypassRequestTarget{
		PolicyBypassTarget: *target,
		dimensions:         dimensions,
	}, nil
}

func riskPolicyBypassGrantSelector(row repo.RiskPolicyBypassRequest) (authz.Selector, error) {
	dimensions, err := riskPolicyBypassDimensions(row.TargetDimensions)
	if err != nil {
		return nil, err
	}

	targetKind := conv.FromPGTextOrEmpty[string](row.TargetKind)
	switch targetKind {
	case "":
	case PolicyBypassTargetKindShadowMCPServer:
		if dimensions[authz.SelectorKeyServerURL] == "" && dimensions[authz.SelectorKeyServerIdentity] == "" {
			return nil, fmt.Errorf("shadow mcp server bypass target missing server_url or server_identity dimension")
		}
	default:
		return nil, fmt.Errorf("unsupported risk policy bypass target kind %q", targetKind)
	}

	selector := authz.NewSelector(authz.ScopeRiskPolicyBypass, row.RiskPolicyID.String())
	maps.Copy(selector, dimensions)

	return selector, nil
}

func riskPolicyBypassGrantPrincipals(requesterUserID string, principalURNs []string) ([]urn.Principal, []string, error) {
	if len(principalURNs) == 0 {
		principalURNs = []string{urn.NewPrincipal(urn.PrincipalTypeUser, requesterUserID).String()}
	}

	principals := make([]urn.Principal, 0, len(principalURNs))
	grantedPrincipalURNs := make([]string, 0, len(principalURNs))
	seen := make(map[string]struct{}, len(principalURNs))
	hasAllUsers := false

	for _, rawPrincipalURN := range principalURNs {
		principalURN := strings.TrimSpace(rawPrincipalURN)
		if principalURN == "" {
			continue
		}

		principal, err := urn.ParsePrincipal(principalURN)
		if err != nil {
			return nil, nil, fmt.Errorf("parse principal urn %q: %w", principalURN, err)
		}
		switch principal.Type {
		case urn.PrincipalTypeUser, urn.PrincipalTypeRole:
		default:
			return nil, nil, fmt.Errorf("unsupported principal type %q", principal.Type)
		}

		key := principal.String()
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		principals = append(principals, principal)
		grantedPrincipalURNs = append(grantedPrincipalURNs, key)
		hasAllUsers = hasAllUsers || key == authz.AllUsersPrincipal().String()
	}

	if len(principals) == 0 {
		return nil, nil, fmt.Errorf("at least one principal is required")
	}
	if hasAllUsers && len(principals) > 1 {
		return nil, nil, fmt.Errorf("user:all cannot be combined with narrower principals")
	}

	return principals, grantedPrincipalURNs, nil
}

func validateRiskPolicyBypassGrantPrincipals(ctx context.Context, db accessrepo.DBTX, organizationID string, principals []urn.Principal) error {
	for _, principal := range principals {
		switch principal.Type {
		case urn.PrincipalTypeUser:
			if principal.String() == authz.AllUsersPrincipal().String() {
				continue
			}
			if _, err := authz.ResolveUserPrincipals(ctx, db, organizationID, principal.ID); err != nil {
				return fmt.Errorf("validate user principal %q: %w", principal.String(), err)
			}
		case urn.PrincipalTypeRole:
			if err := validateRiskPolicyBypassRolePrincipal(ctx, db, organizationID, principal); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported principal type %q", principal.Type)
		}
	}

	return nil
}

func validateRiskPolicyBypassRolePrincipal(ctx context.Context, db accessrepo.DBTX, organizationID string, principal urn.Principal) error {
	roleKind, rawRoleID, ok := strings.Cut(principal.ID, ":")
	if !ok {
		return fmt.Errorf("invalid role principal %q", principal.String())
	}
	if roleKind != "organization" && roleKind != "global" {
		return fmt.Errorf("invalid role principal %q", principal.String())
	}
	roleID, err := uuid.Parse(rawRoleID)
	if err != nil {
		return fmt.Errorf("invalid role principal %q: %w", principal.String(), err)
	}

	row, err := accessrepo.New(db).GetOrganizationRoleByID(ctx, accessrepo.GetOrganizationRoleByIDParams{
		OrganizationID: organizationID,
		ID:             roleID,
	})
	if err != nil {
		return fmt.Errorf("validate role principal %q: %w", principal.String(), err)
	}
	if row.RoleUrn != principal.String() {
		return fmt.Errorf("role principal %q does not match active role %q", principal.String(), row.RoleUrn)
	}

	return nil
}

func riskPolicyBypassGrantPrincipalDifference(currentPrincipals []urn.Principal, nextPrincipals []urn.Principal) []urn.Principal {
	next := make(map[string]struct{}, len(nextPrincipals))
	for _, principal := range nextPrincipals {
		next[principal.String()] = struct{}{}
	}

	diff := make([]urn.Principal, 0, len(currentPrincipals))
	for _, principal := range currentPrincipals {
		if _, ok := next[principal.String()]; ok {
			continue
		}
		diff = append(diff, principal)
	}

	return diff
}

func riskPolicyBypassRequestView(row repo.RiskPolicyBypassRequest) (*gen.RiskPolicyBypassRequest, error) {
	dimensions, err := riskPolicyBypassDimensions(row.TargetDimensions)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "decode risk policy bypass target dimensions")
	}

	decidedAt := conv.FromPGTimestamptz(row.DecidedAt)
	var decidedAtPtr *string
	if decidedAt != "" {
		decidedAtPtr = &decidedAt
	}

	return &gen.RiskPolicyBypassRequest{
		ID:                   row.ID.String(),
		PolicyID:             row.RiskPolicyID.String(),
		TargetKind:           conv.FromPGText[string](row.TargetKind),
		TargetLabel:          conv.FromPGText[string](row.TargetLabel),
		TargetKey:            conv.FromPGText[string](row.TargetKey),
		TargetDimensions:     dimensions,
		RequesterUserID:      row.RequesterUserID,
		RequesterEmail:       conv.FromPGText[string](row.RequesterEmail),
		Note:                 conv.FromPGText[string](row.Note),
		Status:               row.Status,
		DecidedBy:            conv.FromPGText[string](row.DecidedBy),
		GrantedPrincipalUrns: slices.Clone(row.GrantedPrincipalUrns),
		DecidedAt:            decidedAtPtr,
		CreatedAt:            conv.FromPGTimestamptz(row.CreatedAt),
		UpdatedAt:            conv.FromPGTimestamptz(row.UpdatedAt),
	}, nil
}

func riskPolicyBypassDimensions(raw []byte) (map[string]string, error) {
	if len(raw) == 0 {
		return map[string]string{}, nil
	}

	var dimensions map[string]string
	if err := json.Unmarshal(raw, &dimensions); err != nil {
		return nil, fmt.Errorf("unmarshal dimensions: %w", err)
	}

	if dimensions == nil {
		return map[string]string{}, nil
	}

	return dimensions, nil
}
