package risk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/audit"
	auditrepo "github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	riskPolicyBypassRequestStatusRequested = "requested"
	riskPolicyBypassRequestStatusApproved  = "approved"
	riskPolicyBypassRequestStatusDenied    = "denied"
	riskPolicyBypassRequestStatusRevoked   = "revoked"
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
		TargetKind:       conv.ToPGText(target.kind),
		TargetLabel:      conv.ToPGTextEmpty(target.label),
		TargetKey:        conv.ToPGText(target.key),
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
	policyName, err := riskPolicyNameForAudit(ctx, q, current.RiskPolicyID, current.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "get risk policy name for audit").Log(ctx, s.logger)
	}
	principals := riskPolicyBypassApprovalPrincipals(current, conv.PtrValOr(payload.GrantToAllUsers, false))
	selector, err := riskPolicyBypassGrantSelector(current)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "build risk policy bypass selector").Log(ctx, s.logger)
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

	grantedPrincipalURNs := principalURNs(principals)
	row, err := q.UpdateRiskPolicyBypassRequestStatus(ctx, repo.UpdateRiskPolicyBypassRequestStatusParams{
		Status:               riskPolicyBypassRequestStatusApproved,
		DecidedBy:            conv.ToPGText(authCtx.UserID),
		GrantedPrincipalUrns: grantedPrincipalURNs,
		ID:                   requestID,
		ProjectID:            *authCtx.ProjectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "approve risk policy bypass request").Log(ctx, s.logger)
	}

	if err := s.logRiskPolicyBypassRequestAudit(ctx, dbtx, audit.ActionRiskPolicyBypassRequestApprove, authCtx, current.RiskPolicyID, current.ProjectID, policyName, &current, &row); err != nil {
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
	policyName, err := riskPolicyNameForAudit(ctx, q, current.RiskPolicyID, current.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "get risk policy name for audit").Log(ctx, s.logger)
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

	if err := s.logRiskPolicyBypassRequestAudit(ctx, dbtx, audit.ActionRiskPolicyBypassRequestDeny, authCtx, current.RiskPolicyID, current.ProjectID, policyName, &current, &row); err != nil {
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
	policyName, err := riskPolicyNameForAudit(ctx, q, current.RiskPolicyID, current.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "get risk policy name for audit").Log(ctx, s.logger)
	}
	selector, err := riskPolicyBypassGrantSelector(current)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "build risk policy bypass selector").Log(ctx, s.logger)
	}
	principals, err := riskPolicyBypassGrantedPrincipals(current)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "parse risk policy bypass granted principals").Log(ctx, s.logger)
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

	if err := s.logRiskPolicyBypassRequestAudit(ctx, dbtx, audit.ActionRiskPolicyBypassRequestRevoke, authCtx, current.RiskPolicyID, current.ProjectID, policyName, &current, &row); err != nil {
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

func riskPolicyNameForAudit(ctx context.Context, q *repo.Queries, policyID uuid.UUID, projectID uuid.UUID) (string, error) {
	name, err := q.GetRiskPolicyNameIncludingDeleted(ctx, repo.GetRiskPolicyNameIncludingDeletedParams{
		ID:        policyID,
		ProjectID: projectID,
	})
	switch {
	case err == nil:
		return name, nil
	case errors.Is(err, pgx.ErrNoRows):
		return "", nil
	default:
		return "", fmt.Errorf("get risk policy name including deleted: %w", err)
	}
}

func riskPolicyBypassApprovalPrincipals(row repo.RiskPolicyBypassRequest, grantToAllUsers bool) []urn.Principal {
	if grantToAllUsers {
		return []urn.Principal{authz.AllUsersPrincipal()}
	}

	return []urn.Principal{urn.NewPrincipal(urn.PrincipalTypeUser, row.RequesterUserID)}
}

func riskPolicyBypassGrantedPrincipals(row repo.RiskPolicyBypassRequest) ([]urn.Principal, error) {
	if len(row.GrantedPrincipalUrns) == 0 {
		return []urn.Principal{urn.NewPrincipal(urn.PrincipalTypeUser, row.RequesterUserID)}, nil
	}

	principals := make([]urn.Principal, 0, len(row.GrantedPrincipalUrns))
	for _, raw := range row.GrantedPrincipalUrns {
		principal, err := urn.ParsePrincipal(raw)
		if err != nil {
			return nil, fmt.Errorf("parse granted principal %q: %w", raw, err)
		}
		principals = append(principals, principal)
	}

	return principals, nil
}

func principalURNs(principals []urn.Principal) []string {
	urns := make([]string, 0, len(principals))
	for _, principal := range principals {
		urns = append(urns, principal.String())
	}
	return urns
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
	kind       string
	label      string
	key        string
	dimensions []byte
}

func riskPolicyBypassTargetFromClaims(claims *policyBypassRequestClaims) (riskPolicyBypassRequestTarget, error) {
	key := strings.TrimSpace(conv.PtrValOr(claims.ObservedFullURL, ""))
	if key == "" {
		return riskPolicyBypassRequestTarget{}, fmt.Errorf("observed_full_url is required")
	}

	dimensions, err := json.Marshal(map[string]string{
		authz.SelectorKeyServerURL: key,
	})
	if err != nil {
		return riskPolicyBypassRequestTarget{}, fmt.Errorf("marshal dimensions: %w", err)
	}

	label := strings.TrimSpace(conv.PtrValOr(claims.ObservedName, ""))
	if label == "" {
		label = key
	}

	return riskPolicyBypassRequestTarget{
		kind:       authz.SelectorKeyServerURL,
		label:      label,
		key:        key,
		dimensions: dimensions,
	}, nil
}

func riskPolicyBypassGrantSelector(row repo.RiskPolicyBypassRequest) (authz.Selector, error) {
	dimensions, err := riskPolicyBypassDimensions(row.TargetDimensions)
	if err != nil {
		return nil, err
	}

	targetKind := conv.FromPGTextOrEmpty[string](row.TargetKind)
	if targetKind != "" && targetKind != authz.SelectorKeyServerURL && dimensions[authz.SelectorKeyServerURL] == "" {
		return nil, fmt.Errorf("unsupported risk policy bypass target kind %q", targetKind)
	}

	selector := authz.NewSelector(authz.ScopeRiskPolicyBypass, row.RiskPolicyID.String())
	maps.Copy(selector, dimensions)

	return selector, nil
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
