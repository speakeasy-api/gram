package service

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"

	srv "github.com/speakeasy-api/gram/dev-idp/gen/http/xaa_trust_rules/server"
	gen "github.com/speakeasy-api/gram/dev-idp/gen/xaa_trust_rules"
	"github.com/speakeasy-api/gram/dev-idp/internal/conv"
	"github.com/speakeasy-api/gram/dev-idp/internal/database/repo"
	"github.com/speakeasy-api/gram/dev-idp/internal/middleware"
	"github.com/speakeasy-api/gram/dev-idp/internal/oops"
)

// XaaTrustRulesService is the dev-idp /rpc/xaaTrustRules.* implementation:
// the trust domain rules, plus a read-only view of the issued-grant ledger.
type XaaTrustRulesService struct {
	tracer trace.Tracer
	logger *slog.Logger
	db     *sql.DB
}

var _ gen.Service = (*XaaTrustRulesService)(nil)

func NewXaaTrustRulesService(logger *slog.Logger, tracerProvider trace.TracerProvider, db *sql.DB) *XaaTrustRulesService {
	return &XaaTrustRulesService{
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/dev-idp/internal/service/xaa_trust_rules"),
		logger: logger.With(slog.String("component", "devidp.xaaTrustRules")),
		db:     db,
	}
}

func AttachXaaTrustRules(mux goahttp.Muxer, service *XaaTrustRulesService) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	srv.Mount(
		mux,
		srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil),
	)
}

func (s *XaaTrustRulesService) Create(ctx context.Context, p *gen.CreatePayload) (*gen.XaaTrustRule, error) {
	resourceID, err := uuid.Parse(p.ResourceID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid resource id")
	}

	allowedClientIDs := conv.PtrValOrEmpty(p.AllowedClientIds)
	if allowedClientIDs == "" {
		allowedClientIDs = "[]"
	}

	row, err := repo.New(s.db).CreateXaaTrustRule(ctx, repo.CreateXaaTrustRuleParams{
		ID:               uuid.New(),
		ResourceID:       resourceID,
		TrustedIssuer:    p.TrustedIssuer,
		AllowedClientIds: allowedClientIDs,
		AllowedScopes:    conv.PtrValOrEmpty(p.AllowedScopes),
		Enabled:          conv.PtrBool(p.Enabled, true),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "create xaa trust rule").Log(ctx, s.logger)
	}

	return xaaTrustRuleView(row), nil
}

func (s *XaaTrustRulesService) Update(ctx context.Context, p *gen.UpdatePayload) (*gen.XaaTrustRule, error) {
	id, err := uuid.Parse(p.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid trust rule id")
	}

	queries := repo.New(s.db)

	// The query always rewrites `enabled`, so an absent one has to be read
	// off the current row rather than defaulted to true -- otherwise editing
	// a rule's scopes would silently re-enable it.
	enabled := p.Enabled
	if enabled == nil {
		current, gerr := queries.GetXaaTrustRule(ctx, id)
		if gerr != nil {
			if errors.Is(gerr, sql.ErrNoRows) {
				return nil, oops.E(oops.CodeNotFound, nil, "xaa trust rule not found")
			}
			return nil, oops.E(oops.CodeUnexpected, gerr, "load xaa trust rule").Log(ctx, s.logger)
		}
		enabled = &current.Enabled
	}

	row, err := queries.UpdateXaaTrustRule(ctx, repo.UpdateXaaTrustRuleParams{
		TrustedIssuer:    conv.PtrToNullString(p.TrustedIssuer),
		AllowedClientIds: conv.PtrToNullString(p.AllowedClientIds),
		AllowedScopes:    conv.PtrToNullString(p.AllowedScopes),
		Enabled:          *enabled,
		Ts:               time.Now(),
		ID:               id,
	})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, nil, "xaa trust rule not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "update xaa trust rule").Log(ctx, s.logger)
	}

	return xaaTrustRuleView(row), nil
}

func (s *XaaTrustRulesService) List(ctx context.Context, p *gen.ListPayload) (*gen.ListXaaTrustRulesResult, error) {
	after, err := cursorUUID(p.Cursor)
	if err != nil {
		return nil, err
	}
	resourceID, err := optionalUUID(p.ResourceID, "resource_id")
	if err != nil {
		return nil, err
	}

	rows, err := repo.New(s.db).ListXaaTrustRules(ctx, repo.ListXaaTrustRulesParams{
		After:      after,
		ResourceID: resourceID,
		MaxRows:    int64(p.Limit) + 1,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list xaa trust rules").Log(ctx, s.logger)
	}

	page, nextCursor := paginate(rows, p.Limit, func(r repo.XaaTrustRule) string { return r.ID.String() })

	items := make([]*gen.XaaTrustRule, 0, len(page))
	for _, r := range page {
		items = append(items, xaaTrustRuleView(r))
	}

	return &gen.ListXaaTrustRulesResult{Items: items, NextCursor: nextCursor}, nil
}

func (s *XaaTrustRulesService) Delete(ctx context.Context, p *gen.DeletePayload) error {
	id, err := uuid.Parse(p.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid trust rule id")
	}

	if err := repo.New(s.db).DeleteXaaTrustRule(ctx, id); err != nil {
		return oops.E(oops.CodeUnexpected, err, "delete xaa trust rule").Log(ctx, s.logger)
	}

	return nil
}

func (s *XaaTrustRulesService) ListIssuedGrants(ctx context.Context, p *gen.ListIssuedGrantsPayload) (*gen.ListXaaIssuedJagsResult, error) {
	userID, err := optionalUUID(p.UserID, "user_id")
	if err != nil {
		return nil, err
	}
	resourceID, err := optionalUUID(p.ResourceID, "resource_id")
	if err != nil {
		return nil, err
	}

	rows, err := repo.New(s.db).ListXaaIssuedJags(ctx, repo.ListXaaIssuedJagsParams{
		UserID:     userID,
		ResourceID: resourceID,
		MaxRows:    int64(p.Limit),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list xaa issued grants").Log(ctx, s.logger)
	}

	items := make([]*gen.XaaIssuedJag, 0, len(rows))
	for _, r := range rows {
		items = append(items, &gen.XaaIssuedJag{
			Jti:        r.Jti,
			AppID:      r.AppID.String(),
			UserID:     r.UserID.String(),
			ResourceID: r.ResourceID.String(),
			Scope:      r.Scope,
			ExpiresAt:  r.ExpiresAt.UTC().Format(timeFormat),
			CreatedAt:  r.CreatedAt.UTC().Format(timeFormat),
		})
	}

	return &gen.ListXaaIssuedJagsResult{Items: items}, nil
}

func xaaTrustRuleView(r repo.XaaTrustRule) *gen.XaaTrustRule {
	return &gen.XaaTrustRule{
		ID:               r.ID.String(),
		ResourceID:       r.ResourceID.String(),
		TrustedIssuer:    r.TrustedIssuer,
		AllowedClientIds: r.AllowedClientIds,
		AllowedScopes:    r.AllowedScopes,
		Enabled:          r.Enabled,
		CreatedAt:        r.CreatedAt.UTC().Format(timeFormat),
		UpdatedAt:        r.UpdatedAt.UTC().Format(timeFormat),
	}
}
