package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"

	gen "github.com/speakeasy-api/gram/dev-idp/gen/ema_trust_rules"
	srv "github.com/speakeasy-api/gram/dev-idp/gen/http/ema_trust_rules/server"
	"github.com/speakeasy-api/gram/dev-idp/internal/conv"
	"github.com/speakeasy-api/gram/dev-idp/internal/database/repo"
	"github.com/speakeasy-api/gram/dev-idp/internal/middleware"
	"github.com/speakeasy-api/gram/dev-idp/internal/oops"
)

// EmaTrustRulesService is the dev-idp /rpc/emaTrustRules.* implementation:
// the trust domain rules, plus a read-only view of the issued-grant ledger.
type EmaTrustRulesService struct {
	tracer trace.Tracer
	logger *slog.Logger
	db     *sql.DB
}

var _ gen.Service = (*EmaTrustRulesService)(nil)

func NewEmaTrustRulesService(logger *slog.Logger, tracerProvider trace.TracerProvider, db *sql.DB) *EmaTrustRulesService {
	return &EmaTrustRulesService{
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/dev-idp/internal/service/ema_trust_rules"),
		logger: logger.With(slog.String("component", "devidp.emaTrustRules")),
		db:     db,
	}
}

func AttachEmaTrustRules(mux goahttp.Muxer, service *EmaTrustRulesService) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	srv.Mount(
		mux,
		srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil),
	)
}

// validateAllowedClientIDs rejects an allowlist the redeem leg cannot read.
// Stored unchecked, a malformed value turns every redemption into a confusing
// "allowed_client_ids is not a JSON array" long after the mistake was made.
func validateAllowedClientIDs(raw string) error {
	if raw == "" {
		return nil
	}
	var ids []string
	if err := json.Unmarshal([]byte(raw), &ids); err != nil {
		return oops.E(oops.CodeBadRequest, err, "allowed_client_ids must be a JSON array of strings, e.g. [\"my-client\"]")
	}
	return nil
}

func (s *EmaTrustRulesService) Create(ctx context.Context, p *gen.CreatePayload) (*gen.EmaTrustRule, error) {
	if err := validateAllowedClientIDs(conv.PtrValOrEmpty(p.AllowedClientIds)); err != nil {
		return nil, err
	}

	resourceID, err := uuid.Parse(p.ResourceID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid resource id")
	}

	allowedClientIDs := conv.PtrValOrEmpty(p.AllowedClientIds)
	if allowedClientIDs == "" {
		allowedClientIDs = "[]"
	}

	row, err := repo.New(s.db).CreateEmaTrustRule(ctx, repo.CreateEmaTrustRuleParams{
		ID:               uuid.New(),
		ResourceID:       resourceID,
		TrustedIssuer:    p.TrustedIssuer,
		AllowedClientIds: allowedClientIDs,
		AllowedScopes:    conv.PtrValOrEmpty(p.AllowedScopes),
		Enabled:          conv.PtrBool(p.Enabled, true),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "create ema trust rule").Log(ctx, s.logger)
	}

	return emaTrustRuleView(row), nil
}

func (s *EmaTrustRulesService) Update(ctx context.Context, p *gen.UpdatePayload) (*gen.EmaTrustRule, error) {
	if p.AllowedClientIds != nil {
		if verr := validateAllowedClientIDs(*p.AllowedClientIds); verr != nil {
			return nil, verr
		}
	}

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
		current, gerr := queries.GetEmaTrustRule(ctx, id)
		if gerr != nil {
			if errors.Is(gerr, sql.ErrNoRows) {
				return nil, oops.E(oops.CodeNotFound, nil, "ema trust rule not found")
			}
			return nil, oops.E(oops.CodeUnexpected, gerr, "load ema trust rule").Log(ctx, s.logger)
		}
		enabled = &current.Enabled
	}

	row, err := queries.UpdateEmaTrustRule(ctx, repo.UpdateEmaTrustRuleParams{
		TrustedIssuer:    conv.PtrToNullString(p.TrustedIssuer),
		AllowedClientIds: conv.PtrToNullString(p.AllowedClientIds),
		AllowedScopes:    conv.PtrToNullString(p.AllowedScopes),
		Enabled:          *enabled,
		Ts:               time.Now(),
		ID:               id,
	})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, nil, "ema trust rule not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "update ema trust rule").Log(ctx, s.logger)
	}

	return emaTrustRuleView(row), nil
}

func (s *EmaTrustRulesService) List(ctx context.Context, p *gen.ListPayload) (*gen.ListEmaTrustRulesResult, error) {
	after, err := cursorUUID(p.Cursor)
	if err != nil {
		return nil, err
	}
	resourceID, err := optionalUUID(p.ResourceID, "resource_id")
	if err != nil {
		return nil, err
	}

	rows, err := repo.New(s.db).ListEmaTrustRules(ctx, repo.ListEmaTrustRulesParams{
		After:      after,
		ResourceID: resourceID,
		MaxRows:    int64(p.Limit) + 1,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list ema trust rules").Log(ctx, s.logger)
	}

	page, nextCursor := paginate(rows, p.Limit, func(r repo.EmaTrustRule) string { return r.ID.String() })

	items := make([]*gen.EmaTrustRule, 0, len(page))
	for _, r := range page {
		items = append(items, emaTrustRuleView(r))
	}

	return &gen.ListEmaTrustRulesResult{Items: items, NextCursor: nextCursor}, nil
}

func (s *EmaTrustRulesService) Delete(ctx context.Context, p *gen.DeletePayload) error {
	id, err := uuid.Parse(p.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid trust rule id")
	}

	if err := repo.New(s.db).DeleteEmaTrustRule(ctx, id); err != nil {
		return oops.E(oops.CodeUnexpected, err, "delete ema trust rule").Log(ctx, s.logger)
	}

	return nil
}

func (s *EmaTrustRulesService) ListIssuedGrants(ctx context.Context, p *gen.ListIssuedGrantsPayload) (*gen.ListEmaIssuedJagsResult, error) {
	userID, err := optionalUUID(p.UserID, "user_id")
	if err != nil {
		return nil, err
	}
	resourceID, err := optionalUUID(p.ResourceID, "resource_id")
	if err != nil {
		return nil, err
	}

	rows, err := repo.New(s.db).ListEmaIssuedJags(ctx, repo.ListEmaIssuedJagsParams{
		UserID:     userID,
		ResourceID: resourceID,
		MaxRows:    int64(p.Limit),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list ema issued grants").Log(ctx, s.logger)
	}

	items := make([]*gen.EmaIssuedJag, 0, len(rows))
	for _, r := range rows {
		items = append(items, &gen.EmaIssuedJag{
			Jti:        r.Jti,
			AppID:      r.AppID.String(),
			UserID:     r.UserID.String(),
			ResourceID: r.ResourceID.String(),
			Scope:      r.Scope,
			ExpiresAt:  r.ExpiresAt.UTC().Format(timeFormat),
			CreatedAt:  r.CreatedAt.UTC().Format(timeFormat),
		})
	}

	return &gen.ListEmaIssuedJagsResult{Items: items}, nil
}

func emaTrustRuleView(r repo.EmaTrustRule) *gen.EmaTrustRule {
	return &gen.EmaTrustRule{
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
