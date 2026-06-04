package policyaccess

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/server/gen/http/policyaccess/server"
	gen "github.com/speakeasy-api/gram/server/gen/policyaccess"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/policyaccess/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type Service struct {
	tracer trace.Tracer
	logger *slog.Logger
	db     *pgxpool.Pool
	auth   *auth.Auth
	authz  *authz.Engine
}

func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	sessions *sessions.Manager,
	authz *authz.Engine,
) *Service {
	logger = logger.With(attr.SlogComponent("policyaccess"))
	return &Service{
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/policyaccess"),
		logger: logger,
		db:     db,
		auth:   auth.New(logger, db, sessions, authz),
		authz:  authz,
	}
}

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

func Attach(mux goahttp.Muxer, service *Service) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	srv.Mount(
		mux,
		srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil),
	)
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

func (s *Service) ListRequests(ctx context.Context, payload *gen.ListRequestsPayload) (*gen.ListPolicyAccessRequestsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	var status pgtype.Text
	if payload.Status != nil && *payload.Status != "" {
		status = pgtype.Text{String: *payload.Status, Valid: true}
	}

	rows, err := repo.New(s.db).ListPolicyAccessRequests(ctx, repo.ListPolicyAccessRequestsParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Status:         status,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list policy access requests").Log(ctx, s.logger)
	}

	requests := make([]*gen.PolicyAccessRequest, 0, len(rows))
	for _, r := range rows {
		requests = append(requests, policyAccessRequestListRowToGen(r))
	}
	return &gen.ListPolicyAccessRequestsResult{Requests: requests}, nil
}

func (s *Service) DecideRequest(ctx context.Context, payload *gen.DecideRequestPayload) (*gen.PolicyAccessRequest, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	decision, err := DecideRequest(ctx, s.db, Decision{
		OrganizationID: authCtx.ActiveOrganizationID,
		RequestID:      payload.ID,
		Status:         payload.Status,
		GrantType:      conv.PtrValOrEmpty(payload.GrantType, ""),
		RoleSlugs:      payload.RoleSlugs,
		DecidedBy:      "user:" + authCtx.UserID,
	})
	if err != nil {
		if errors.Is(err, ErrGrantRecipientRequired) {
			return nil, oops.E(oops.CodeBadRequest, err, "grant recipient is required when approving").Log(ctx, s.logger)
		}
		if errors.Is(err, ErrInvalidRequestID) {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid request id").Log(ctx, s.logger)
		}
		if errors.Is(err, ErrRequestNotFound) {
			return nil, oops.E(oops.CodeBadRequest, err, "request not found or already decided").Log(ctx, s.logger)
		}
		if errors.Is(err, ErrInvalidGrantType) {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid grant type").Log(ctx, s.logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "decide policy access request").Log(ctx, s.logger)
	}

	return policyAccessRequestToGen(decision), nil
}

func (s *Service) ListBypasses(ctx context.Context, payload *gen.ListBypassesPayload) (*gen.ListPolicyBypassesResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	rows, err := repo.New(s.db).ListPolicyBypasses(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list policy bypasses").Log(ctx, s.logger)
	}
	bypasses := make([]*gen.PolicyBypassGrant, 0, len(rows))
	for _, row := range rows {
		bypasses = append(bypasses, policyBypassToGen(row))
	}

	return &gen.ListPolicyBypassesResult{Bypasses: bypasses}, nil
}

func (s *Service) RevokeBypass(ctx context.Context, payload *gen.RevokeBypassPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return err
	}

	grantID, err := uuid.Parse(payload.GrantID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid grant id").Log(ctx, s.logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin revoke policy bypass").Log(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	queries := repo.New(dbtx)
	deletedGrant, err := queries.DeletePolicyBypass(ctx, repo.DeletePolicyBypassParams{
		GrantID:        grantID,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeBadRequest, err, "bypass not found").Log(ctx, s.logger)
		}
		return oops.E(oops.CodeUnexpected, err, "revoke policy bypass").Log(ctx, s.logger)
	}

	selector, err := authz.SelectorFromRow(deletedGrant.Selectors)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "decode revoked policy bypass selector").Log(ctx, s.logger)
	}
	policyID, err := uuid.Parse(selector[authz.SelectorKeyResourceID])
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "decode revoked policy bypass policy id").Log(ctx, s.logger)
	}
	target := targetFromSelector(selector)
	if _, err := queries.UpdatePolicyAccessRequestAfterBypassRevoked(ctx, repo.UpdatePolicyAccessRequestAfterBypassRevokedParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		PolicyID:       policyID,
		TargetKey:      target.Key,
		PrincipalUrn:   deletedGrant.PrincipalUrn.String(),
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "update policy access request after revoke").Log(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit revoke policy bypass").Log(ctx, s.logger)
	}

	return nil
}

type RecordRequestParams struct {
	OrganizationID  string
	ProjectID       string
	PolicyID        string
	Target          Target
	RequesterUserID string
	RequesterEmail  string
	Note            string
}

type Target struct {
	Kind       string
	Label      string
	Key        string
	Dimensions map[string]string
}

func WholePolicyTarget() Target {
	return Target{
		Kind:       "",
		Label:      "",
		Key:        "policy",
		Dimensions: map[string]string{},
	}
}

func ShadowMCPServerTarget(serverURL string) Target {
	if serverURL == "" {
		return WholePolicyTarget()
	}
	return Target{
		Kind:       "shadow_mcp_server",
		Label:      serverURL,
		Key:        "shadow_mcp_server:" + canonicalTargetKey(map[string]string{authz.SelectorKeyServerURL: serverURL}),
		Dimensions: map[string]string{authz.SelectorKeyServerURL: serverURL},
	}
}

func RecordRequest(ctx context.Context, db *pgxpool.Pool, params RecordRequestParams) (repo.PolicyAccessRequest, error) {
	projectID, err := uuid.Parse(params.ProjectID)
	if err != nil {
		return repo.PolicyAccessRequest{}, fmt.Errorf("parse project id: %w", err)
	}
	policyID, err := uuid.Parse(params.PolicyID)
	if err != nil {
		return repo.PolicyAccessRequest{}, fmt.Errorf("parse policy id: %w", err)
	}
	target := normalizeTarget(params.Target)
	targetDimensions, err := json.Marshal(target.Dimensions)
	if err != nil {
		return repo.PolicyAccessRequest{}, fmt.Errorf("marshal target dimensions: %w", err)
	}
	row, err := repo.New(db).CreatePolicyAccessRequest(ctx, repo.CreatePolicyAccessRequestParams{
		OrganizationID:   params.OrganizationID,
		ProjectID:        projectID,
		PolicyID:         policyID,
		TargetKind:       target.Kind,
		TargetLabel:      target.Label,
		TargetKey:        target.Key,
		TargetDimensions: targetDimensions,
		RequesterUserID:  params.RequesterUserID,
		RequesterEmail:   params.RequesterEmail,
		Note:             params.Note,
	})
	if err != nil {
		return repo.PolicyAccessRequest{}, fmt.Errorf("create policy access request: %w", err)
	}
	return row, nil
}

var (
	ErrInvalidRequestID       = errors.New("invalid request id")
	ErrRequestNotFound        = errors.New("request not found or already decided")
	ErrGrantRecipientRequired = errors.New("grant recipient is required when approving")
	ErrInvalidGrantType       = errors.New("invalid grant type")
)

type Decision struct {
	OrganizationID string
	RequestID      string
	Status         string
	GrantType      string
	RoleSlugs      []string
	DecidedBy      string
}

const (
	GrantTypeRequester      = "requester"
	GrantTypeRequesterRoles = "requester_roles"
	GrantTypeRoles          = "roles"
)

func DecideRequest(ctx context.Context, db *pgxpool.Pool, decision Decision) (repo.PolicyAccessRequest, error) {
	id, err := uuid.Parse(decision.RequestID)
	if err != nil {
		return repo.PolicyAccessRequest{}, ErrInvalidRequestID
	}

	dbtx, err := db.Begin(ctx)
	if err != nil {
		return repo.PolicyAccessRequest{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	q := repo.New(dbtx)
	request, err := q.GetRequestedPolicyAccessRequestForUpdate(ctx, repo.GetRequestedPolicyAccessRequestForUpdateParams{
		OrganizationID: decision.OrganizationID,
		ID:             id,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return repo.PolicyAccessRequest{}, ErrRequestNotFound
		}
		return repo.PolicyAccessRequest{}, fmt.Errorf("load policy access request: %w", err)
	}

	grantedPrincipalURNs := []string{}
	if decision.Status == "approved" {
		principals, err := grantPrincipalsForDecision(ctx, dbtx, decision, request)
		if err != nil {
			return repo.PolicyAccessRequest{}, err
		}

		selector, err := selectorForRequest(request)
		if err != nil {
			return repo.PolicyAccessRequest{}, err
		}
		for _, principal := range principals {
			if err := authz.GrantResourceToPrincipalWithSelectorTx(ctx, dbtx, decision.OrganizationID, principal, authz.ScopeRiskPolicyBypass, selector); err != nil {
				return repo.PolicyAccessRequest{}, fmt.Errorf("grant policy bypass to principal: %w", err)
			}
			grantedPrincipalURNs = append(grantedPrincipalURNs, principal.String())
		}
	}

	decided, err := repo.New(dbtx).DecidePolicyAccessRequest(ctx, repo.DecidePolicyAccessRequestParams{
		Status:               decision.Status,
		DecidedBy:            decision.DecidedBy,
		GrantedPrincipalUrns: grantedPrincipalURNs,
		OrganizationID:       decision.OrganizationID,
		ID:                   id,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return repo.PolicyAccessRequest{}, ErrRequestNotFound
		}
		return repo.PolicyAccessRequest{}, fmt.Errorf("decide policy access request: %w", err)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return repo.PolicyAccessRequest{}, fmt.Errorf("commit transaction: %w", err)
	}

	return decided, nil
}

func grantPrincipalsForDecision(ctx context.Context, dbtx repo.DBTX, decision Decision, request repo.PolicyAccessRequest) ([]urn.Principal, error) {
	grantType := decision.GrantType
	if grantType == "" {
		if len(decision.RoleSlugs) > 0 {
			grantType = GrantTypeRoles
		} else {
			grantType = GrantTypeRequester
		}
	}

	switch grantType {
	case GrantTypeRequester:
		if request.RequesterUserID == "" {
			return nil, ErrGrantRecipientRequired
		}
		return []urn.Principal{urn.NewPrincipal(urn.PrincipalTypeUser, request.RequesterUserID)}, nil
	case GrantTypeRequesterRoles:
		if request.RequesterUserID == "" {
			return nil, ErrGrantRecipientRequired
		}
		rows, err := accessrepo.New(dbtx).ListMemberRolePrincipalsByUser(ctx, accessrepo.ListMemberRolePrincipalsByUserParams{
			OrganizationID: decision.OrganizationID,
			UserID:         request.RequesterUserID,
		})
		if err != nil {
			return nil, fmt.Errorf("list requester role principals: %w", err)
		}
		principals := make([]urn.Principal, 0, len(rows))
		seen := make(map[string]struct{}, len(rows))
		for _, row := range rows {
			principal, err := urn.ParsePrincipal(row.PrincipalUrn)
			if err != nil {
				return nil, fmt.Errorf("parse requester role principal: %w", err)
			}
			if _, ok := seen[principal.String()]; ok {
				continue
			}
			seen[principal.String()] = struct{}{}
			principals = append(principals, principal)
		}
		if len(principals) == 0 {
			return nil, ErrGrantRecipientRequired
		}
		return principals, nil
	case GrantTypeRoles:
		if len(decision.RoleSlugs) == 0 {
			return nil, ErrGrantRecipientRequired
		}
		principals := make([]urn.Principal, 0, len(decision.RoleSlugs))
		seen := make(map[string]struct{}, len(decision.RoleSlugs))
		for _, slug := range decision.RoleSlugs {
			if slug == "" {
				continue
			}
			role, err := accessrepo.New(dbtx).GetActiveOrganizationRoleBySlug(ctx, accessrepo.GetActiveOrganizationRoleBySlugParams{
				OrganizationID: decision.OrganizationID,
				WorkosSlug:     slug,
			})
			if err != nil {
				return nil, fmt.Errorf("resolve role principal for %q: %w", slug, err)
			}
			principal, err := urn.ParsePrincipal(role.RoleUrn)
			if err != nil {
				return nil, fmt.Errorf("parse role principal for %q: %w", slug, err)
			}
			if _, ok := seen[principal.String()]; ok {
				continue
			}
			seen[principal.String()] = struct{}{}
			principals = append(principals, principal)
		}
		if len(principals) == 0 {
			return nil, ErrGrantRecipientRequired
		}
		return principals, nil
	default:
		return nil, ErrInvalidGrantType
	}
}

func selectorForRequest(request repo.PolicyAccessRequest) (authz.Selector, error) {
	selector := authz.Selector{
		authz.SelectorKeyResourceKind: authz.ResourceKindRiskPolicy,
		authz.SelectorKeyResourceID:   request.PolicyID.String(),
	}
	dimensions, err := dimensionsFromJSON(request.TargetDimensions)
	if err != nil {
		return nil, err
	}
	for key, value := range dimensions {
		if value != "" {
			selector[key] = value
		}
	}
	if err := authz.ValidateSelector(authz.ScopeRiskPolicyBypass, selector); err != nil {
		return nil, fmt.Errorf("validate selector: %w", err)
	}
	return selector, nil
}

func policyAccessRequestToGen(r repo.PolicyAccessRequest) *gen.PolicyAccessRequest {
	return policyAccessRequestRowToGen(r, "")
}

func policyAccessRequestRowToGen(r repo.PolicyAccessRequest, policyName string) *gen.PolicyAccessRequest {
	var decidedAt *string
	if r.DecidedAt.Valid {
		s := r.DecidedAt.Time.Format(time.RFC3339)
		decidedAt = &s
	}
	return &gen.PolicyAccessRequest{
		ID:                   r.ID.String(),
		OrganizationID:       r.OrganizationID,
		ProjectID:            r.ProjectID.String(),
		PolicyID:             r.PolicyID.String(),
		PolicyName:           conv.PtrEmpty(policyName),
		Target:               targetToGen(targetFromRow(r.TargetKind, r.TargetLabel, r.TargetKey, r.TargetDimensions)),
		RequesterUserID:      conv.PtrEmpty(r.RequesterUserID),
		RequesterEmail:       conv.PtrEmpty(r.RequesterEmail),
		Note:                 conv.PtrEmpty(r.Note),
		Status:               r.Status,
		DecidedBy:            conv.PtrEmpty(r.DecidedBy),
		GrantedPrincipalUrns: r.GrantedPrincipalUrns,
		DecidedAt:            decidedAt,
		CreatedAt:            r.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:            r.UpdatedAt.Time.Format(time.RFC3339),
	}
}

func policyAccessRequestListRowToGen(r repo.ListPolicyAccessRequestsRow) *gen.PolicyAccessRequest {
	base := repo.PolicyAccessRequest{
		ID:                   r.ID,
		OrganizationID:       r.OrganizationID,
		ProjectID:            r.ProjectID,
		PolicyID:             r.PolicyID,
		TargetKind:           r.TargetKind,
		TargetLabel:          r.TargetLabel,
		TargetKey:            r.TargetKey,
		TargetDimensions:     r.TargetDimensions,
		RequesterUserID:      r.RequesterUserID,
		RequesterEmail:       r.RequesterEmail,
		Note:                 r.Note,
		Status:               r.Status,
		DecidedBy:            r.DecidedBy,
		GrantedPrincipalUrns: r.GrantedPrincipalUrns,
		DecidedAt:            r.DecidedAt,
		CreatedAt:            r.CreatedAt,
		UpdatedAt:            r.UpdatedAt,
		DeletedAt:            r.DeletedAt,
		Deleted:              r.Deleted,
	}
	return policyAccessRequestRowToGen(base, r.PolicyName)
}

func policyBypassToGen(r repo.ListPolicyBypassesRow) *gen.PolicyBypassGrant {
	return &gen.PolicyBypassGrant{
		ID:            r.ID.String(),
		PolicyID:      r.PolicyID.String(),
		PolicyName:    conv.PtrEmpty(r.PolicyName),
		PrincipalUrn:  r.PrincipalUrn.String(),
		PrincipalType: r.PrincipalType,
		Target:        targetToGen(targetFromSelectorBytes(r.Selectors)),
		CreatedAt:     r.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:     r.UpdatedAt.Time.Format(time.RFC3339),
	}
}

func targetToGen(target Target) *gen.PolicyAccessTarget {
	target = normalizeTarget(target)
	return &gen.PolicyAccessTarget{
		Kind:       target.Kind,
		Label:      target.Label,
		Key:        target.Key,
		Dimensions: target.Dimensions,
	}
}

func normalizeTarget(target Target) Target {
	if target.Dimensions == nil {
		target.Dimensions = map[string]string{}
	}
	if target.Key == "" {
		if len(target.Dimensions) == 0 {
			target.Key = "policy"
		} else {
			target.Key = canonicalTargetKey(target.Dimensions)
		}
	}
	return target
}

func canonicalTargetKey(dimensions map[string]string) string {
	if len(dimensions) == 0 {
		return "policy"
	}
	keys := make([]string, 0, len(dimensions))
	for key := range dimensions {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+dimensions[key])
	}
	return strings.Join(parts, ";")
}

func targetFromRow(kind, label, key string, raw []byte) Target {
	dimensions, _ := dimensionsFromJSON(raw)
	return normalizeTarget(Target{Kind: kind, Label: label, Key: key, Dimensions: dimensions})
}

func targetFromSelectorBytes(raw []byte) Target {
	selector, err := authz.SelectorFromRow(raw)
	if err != nil {
		return WholePolicyTarget()
	}
	return targetFromSelector(selector)
}

func targetFromSelector(selector authz.Selector) Target {
	dimensions := map[string]string{}
	for key, value := range selector {
		if key == authz.SelectorKeyResourceKind || key == authz.SelectorKeyResourceID {
			continue
		}
		dimensions[key] = value
	}
	if serverURL := dimensions[authz.SelectorKeyServerURL]; serverURL != "" {
		return ShadowMCPServerTarget(serverURL)
	}
	return normalizeTarget(Target{
		Kind:       "",
		Label:      "",
		Key:        "",
		Dimensions: dimensions,
	})
}

func dimensionsFromJSON(raw []byte) (map[string]string, error) {
	if len(raw) == 0 {
		return map[string]string{}, nil
	}
	var dimensions map[string]string
	if err := json.Unmarshal(raw, &dimensions); err != nil {
		return nil, fmt.Errorf("decode target dimensions: %w", err)
	}
	if dimensions == nil {
		dimensions = map[string]string{}
	}
	return dimensions, nil
}
