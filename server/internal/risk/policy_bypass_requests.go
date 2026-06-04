package risk

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
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
		req, err := riskPolicyBypassRequestToType(row)
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

	project, err := projectsrepo.New(s.db).GetProjectByID(ctx, projectID)
	if err != nil {
		return nil, oops.E(oops.CodeNotFound, err, "project not found").Log(ctx, s.logger)
	}
	if project.OrganizationID != claims.OrganizationID {
		return nil, oops.C(oops.CodeForbidden)
	}
	if _, err := s.repo.GetRiskPolicy(ctx, repo.GetRiskPolicyParams{
		ID:        policyID,
		ProjectID: projectID,
	}); err != nil {
		return nil, oops.E(oops.CodeNotFound, err, "risk policy not found").Log(ctx, s.logger)
	}

	target, err := riskPolicyBypassTargetFromClaims(claims)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid risk policy bypass request target")
	}
	row, err := s.repo.UpsertRiskPolicyBypassRequest(ctx, repo.UpsertRiskPolicyBypassRequestParams{
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
		Note:             conv.ToPGTextEmpty(optionalStringValue(claims.BlockReason)),
		Status:           riskPolicyBypassRequestStatusRequested,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "create risk policy bypass request").Log(ctx, s.logger)
	}

	return riskPolicyBypassRequestToType(row)
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

	principal := urn.NewPrincipal(urn.PrincipalTypeUser, current.RequesterUserID)
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
		Principals: []urn.Principal{principal},
		Selector:   selector,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "grant risk policy bypass").Log(ctx, s.logger)
	}

	row, err := q.UpdateRiskPolicyBypassRequestStatus(ctx, repo.UpdateRiskPolicyBypassRequestStatusParams{
		Status:               riskPolicyBypassRequestStatusApproved,
		DecidedBy:            conv.ToPGText(authCtx.UserID),
		GrantedPrincipalUrns: []string{principal.String()},
		ID:                   requestID,
		ProjectID:            *authCtx.ProjectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "approve risk policy bypass request").Log(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit risk policy bypass approval").Log(ctx, s.logger)
	}

	return riskPolicyBypassRequestToType(row)
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
	row, err := s.repo.UpdateRiskPolicyBypassRequestStatus(ctx, repo.UpdateRiskPolicyBypassRequestStatusParams{
		Status:               riskPolicyBypassRequestStatusDenied,
		DecidedBy:            conv.ToPGText(authCtx.UserID),
		GrantedPrincipalUrns: []string{},
		ID:                   requestID,
		ProjectID:            *authCtx.ProjectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeNotFound, err, "risk policy bypass request not found").Log(ctx, s.logger)
	}

	return riskPolicyBypassRequestToType(row)
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
		Principals: []urn.Principal{urn.NewPrincipal(urn.PrincipalTypeUser, current.RequesterUserID)},
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

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit risk policy bypass revocation").Log(ctx, s.logger)
	}

	return riskPolicyBypassRequestToType(row)
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
	switch {
	case optionalStringValue(claims.ObservedFullURL) != "":
		fullURL := optionalStringValue(claims.ObservedFullURL)
		return marshalRiskPolicyBypassTarget(authz.SelectorKeyServerURL, riskPolicyBypassTargetLabel(claims, fullURL), fullURL, map[string]string{
			authz.SelectorKeyServerURL: fullURL,
		})
	case optionalStringValue(claims.ObservedURLHost) != "":
		host := optionalStringValue(claims.ObservedURLHost)
		return marshalRiskPolicyBypassTarget("url_host", riskPolicyBypassTargetLabel(claims, host), host, map[string]string{})
	case optionalStringValue(claims.ObservedServerIdentity) != "":
		identity := optionalStringValue(claims.ObservedServerIdentity)
		return marshalRiskPolicyBypassTarget("server_identity", riskPolicyBypassTargetLabel(claims, identity), identity, map[string]string{})
	default:
		return riskPolicyBypassRequestTarget{}, fmt.Errorf("target evidence is required")
	}
}

func riskPolicyBypassTargetLabel(claims *policyBypassRequestClaims, fallback string) string {
	if label := optionalStringValue(claims.ObservedName); label != "" {
		return label
	}
	return fallback
}

func marshalRiskPolicyBypassTarget(kind, label, key string, dimensions map[string]string) (riskPolicyBypassRequestTarget, error) {
	rawDimensions, err := json.Marshal(dimensions)
	if err != nil {
		return riskPolicyBypassRequestTarget{}, fmt.Errorf("marshal dimensions: %w", err)
	}
	return riskPolicyBypassRequestTarget{
		kind:       kind,
		label:      label,
		key:        key,
		dimensions: rawDimensions,
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
	for k, v := range dimensions {
		selector[k] = v
	}

	return selector, nil
}

func riskPolicyBypassRequestToType(row repo.RiskPolicyBypassRequest) (*gen.RiskPolicyBypassRequest, error) {
	dimensions, err := riskPolicyBypassDimensions(row.TargetDimensions)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "decode risk policy bypass target dimensions")
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
		DecidedAt:            pgTimeStringPtr(row.DecidedAt),
		CreatedAt:            pgTimeString(row.CreatedAt),
		UpdatedAt:            pgTimeString(row.UpdatedAt),
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

func pgTimeString(ts pgtype.Timestamptz) string {
	if !ts.Valid {
		return ""
	}
	return ts.Time.UTC().Format(time.RFC3339)
}

func pgTimeStringPtr(ts pgtype.Timestamptz) *string {
	if !ts.Valid {
		return nil
	}
	value := pgTimeString(ts)
	return &value
}
