package spendrules

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/server/gen/http/spend_rules/server"
	gen "github.com/speakeasy-api/gram/server/gen/spend_rules"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsRepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/spendrules/celenv"
	chrepo "github.com/speakeasy-api/gram/server/internal/spendrules/chrepo"
	"github.com/speakeasy-api/gram/server/internal/spendrules/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

const (
	defaultEventsPageLimit = 50
	// previewActorCap bounds the per-actor breakdown returned by the preview
	// endpoint. The dashboard shows the top few and lets admins search the rest
	// of this list, so it holds enough of a large org's spenders to be useful.
	previewActorCap = 100
)

// EvaluationSignaler triggers an immediate evaluation cycle for an
// organization after a rule mutation so circuits open and close quickly
// instead of waiting for the next scheduled cycle. Best-effort: a failed
// signal is logged, not fatal.
type EvaluationSignaler interface {
	Signal(ctx context.Context, organizationID string) error
}

type Service struct {
	tracer trace.Tracer
	logger *slog.Logger
	db     *pgxpool.Pool
	chConn chrepo.CHTX
	auth   *auth.Auth
	authz  *authz.Engine
	audit  *audit.Logger
	celEng *celenv.Engine
	// signaler is optional; nil disables mutation-triggered re-evaluation.
	signaler EvaluationSignaler
}

func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	chConn chrepo.CHTX,
	sessionManager *sessions.Manager,
	authzEngine *authz.Engine,
	auditLogger *audit.Logger,
	celEng *celenv.Engine,
	signaler EvaluationSignaler,
) *Service {
	logger = logger.With(attr.SlogComponent("spendrules"))

	return &Service{
		tracer:   tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/spendrules"),
		logger:   logger,
		db:       db,
		chConn:   chConn,
		auth:     auth.New(logger, db, sessionManager, authzEngine),
		authz:    authzEngine,
		audit:    auditLogger,
		celEng:   celEng,
		signaler: signaler,
	}
}

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

func (s *Service) CreateSpendRule(ctx context.Context, payload *gen.CreateSpendRulePayload) (*types.SpendRule, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	if payload.Name == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "rule name is required")
	}
	if payload.LimitUsd <= 0 {
		return nil, oops.E(oops.CodeBadRequest, nil, "limit must be greater than zero")
	}
	if payload.Target == nil {
		return nil, oops.E(oops.CodeBadRequest, nil, "target is required")
	}
	targetExpr, err := targetConditionExpr(payload.Target.Attribute, payload.Target.Operator, payload.Target.Value)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid target")
	}
	if _, err := s.celEng.Compile(targetExpr); err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid target expression: %s", err.Error())
	}
	ruleExpr := DefaultRuleExpr
	if _, err := s.celEng.Compile(ruleExpr); err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid rule expression: %s", err.Error())
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	queries := repo.New(dbtx)

	slug, err := ruleSlug(ctx, queries, authCtx.ActiveOrganizationID, payload.Name)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "derive spend rule slug").LogError(ctx, s.logger)
	}

	row, err := queries.CreateSpendRule(ctx, repo.CreateSpendRuleParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Name:           payload.Name,
		Slug:           slug,
		Description:    payload.Description,
		TargetExpr:     targetExpr,
		RuleExpr:       ruleExpr,
		LimitUsd:       payload.LimitUsd,
		WindowKind:     payload.WindowKind,
		WarnAtPct:      int32(payload.WarnAtPct), //nolint:gosec // design constrains warn_at_pct to 1..100
		Action:         payload.Action,
		Enabled:        payload.Enabled,
	})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		// Lost a race with a concurrent create picking the same slug.
		return nil, oops.E(oops.CodeConflict, err, "a rule with a conflicting name was just created, try again")
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "create spend rule").LogError(ctx, s.logger)
	}

	if err := queries.InsertSpendRuleVersion(ctx, repo.InsertSpendRuleVersionParams{
		OrganizationID: row.OrganizationID,
		SpendRuleID:    row.ID,
		Version:        row.Version,
		TargetExpr:     row.TargetExpr,
		RuleExpr:       row.RuleExpr,
		LimitUsd:       row.LimitUsd,
		WindowKind:     row.WindowKind,
		WarnAtPct:      row.WarnAtPct,
		Action:         row.Action,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "record spend rule version").LogError(ctx, s.logger)
	}

	if err := s.audit.LogSpendRuleCreate(ctx, dbtx, audit.LogSpendRuleCreateEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		SpendRuleURN:     urn.NewSpendRule(row.Slug, row.Version),
		SpendRuleName:    row.Name,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log spend rule create").LogError(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit spend rule create").LogError(ctx, s.logger)
	}

	s.signalEvaluation(ctx, authCtx.ActiveOrganizationID)

	return buildSpendRuleView(row), nil
}

func (s *Service) ListSpendRules(ctx context.Context, payload *gen.ListSpendRulesPayload) (*gen.ListSpendRulesResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	rows, err := repo.New(s.db).ListSpendRules(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list spend rules").LogError(ctx, s.logger)
	}

	rules := make([]*types.SpendRule, 0, len(rows))
	for _, row := range rows {
		rules = append(rules, buildSpendRuleView(row))
	}

	return &gen.ListSpendRulesResult{Rules: rules}, nil
}

func (s *Service) GetSpendRule(ctx context.Context, payload *gen.GetSpendRulePayload) (*types.SpendRule, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid rule id")
	}

	row, err := repo.New(s.db).GetSpendRule(ctx, repo.GetSpendRuleParams{
		ID:             id,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.E(oops.CodeNotFound, err, "spend rule not found")
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "get spend rule").LogError(ctx, s.logger)
	}

	return buildSpendRuleView(row), nil
}

func (s *Service) UpdateSpendRule(ctx context.Context, payload *gen.UpdateSpendRulePayload) (*types.SpendRule, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid rule id")
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	queries := repo.New(dbtx)

	existing, err := queries.GetSpendRuleForUpdate(ctx, repo.GetSpendRuleForUpdateParams{
		ID:             id,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.E(oops.CodeNotFound, err, "spend rule not found")
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load spend rule").LogError(ctx, s.logger)
	}

	name := conv.PtrValOr(payload.Name, existing.Name)
	if name == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "rule name is required")
	}
	description := existing.Description
	if payload.Description != nil {
		description = *payload.Description
	}
	targetExpr := existing.TargetExpr
	if payload.Target != nil {
		var err error
		targetExpr, err = targetConditionExpr(payload.Target.Attribute, payload.Target.Operator, payload.Target.Value)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid target")
		}
	}
	ruleExpr := existing.RuleExpr
	limitUSD := conv.PtrValOr(payload.LimitUsd, existing.LimitUsd)
	if limitUSD <= 0 {
		return nil, oops.E(oops.CodeBadRequest, nil, "limit must be greater than zero")
	}
	windowKind := conv.PtrValOr(payload.WindowKind, existing.WindowKind)
	warnAtPct := existing.WarnAtPct
	if payload.WarnAtPct != nil {
		warnAtPct = int32(*payload.WarnAtPct) //nolint:gosec // design constrains warn_at_pct to 1..100
	}
	action := conv.PtrValOr(payload.Action, existing.Action)
	enabled := existing.Enabled
	if payload.Enabled != nil {
		enabled = *payload.Enabled
	}

	if targetExpr != existing.TargetExpr {
		if _, err := s.celEng.Compile(targetExpr); err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid target expression: %s", err.Error())
		}
	}
	if ruleExpr != existing.RuleExpr {
		if _, err := s.celEng.Compile(ruleExpr); err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid rule expression: %s", err.Error())
		}
	}

	// Material changes alter what the rule measures or does, so the version
	// bumps and future events pin the new configuration.
	material := targetExpr != existing.TargetExpr ||
		ruleExpr != existing.RuleExpr ||
		limitUSD != existing.LimitUsd ||
		windowKind != existing.WindowKind ||
		warnAtPct != existing.WarnAtPct ||
		action != existing.Action

	version := existing.Version
	if material {
		version++
	}

	row, err := queries.UpdateSpendRule(ctx, repo.UpdateSpendRuleParams{
		ID:             id,
		OrganizationID: authCtx.ActiveOrganizationID,
		Name:           name,
		Description:    description,
		TargetExpr:     targetExpr,
		RuleExpr:       ruleExpr,
		LimitUsd:       limitUSD,
		WindowKind:     windowKind,
		WarnAtPct:      warnAtPct,
		Action:         action,
		Enabled:        enabled,
		Version:        version,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "update spend rule").LogError(ctx, s.logger)
	}

	if material {
		if err := queries.InsertSpendRuleVersion(ctx, repo.InsertSpendRuleVersionParams{
			OrganizationID: row.OrganizationID,
			SpendRuleID:    row.ID,
			Version:        row.Version,
			TargetExpr:     row.TargetExpr,
			RuleExpr:       row.RuleExpr,
			LimitUsd:       row.LimitUsd,
			WindowKind:     row.WindowKind,
			WarnAtPct:      row.WarnAtPct,
			Action:         row.Action,
		}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "record spend rule version").LogError(ctx, s.logger)
		}
	}

	if err := s.audit.LogSpendRuleUpdate(ctx, dbtx, audit.LogSpendRuleUpdateEvent{
		OrganizationID:          authCtx.ActiveOrganizationID,
		Actor:                   urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:        authCtx.Email,
		ActorSlug:               nil,
		SpendRuleURN:            urn.NewSpendRule(row.Slug, row.Version),
		SpendRuleName:           row.Name,
		SpendRuleSnapshotBefore: buildSpendRuleView(existing),
		SpendRuleSnapshotAfter:  buildSpendRuleView(row),
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log spend rule update").LogError(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit spend rule update").LogError(ctx, s.logger)
	}

	s.signalEvaluation(ctx, authCtx.ActiveOrganizationID)

	return buildSpendRuleView(row), nil
}

func (s *Service) DeleteSpendRule(ctx context.Context, payload *gen.DeleteSpendRulePayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return err
	}

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid rule id")
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	row, err := repo.New(dbtx).DeleteSpendRule(ctx, repo.DeleteSpendRuleParams{
		ID:             id,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return oops.E(oops.CodeNotFound, err, "spend rule not found")
	}
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "delete spend rule").LogError(ctx, s.logger)
	}

	if err := s.audit.LogSpendRuleDelete(ctx, dbtx, audit.LogSpendRuleDeleteEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		SpendRuleURN:     urn.NewSpendRule(row.Slug, row.Version),
		SpendRuleName:    row.Name,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "log spend rule delete").LogError(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit spend rule delete").LogError(ctx, s.logger)
	}

	s.signalEvaluation(ctx, authCtx.ActiveOrganizationID)

	return nil
}

func (s *Service) PreviewSpendRule(ctx context.Context, payload *gen.PreviewSpendRulePayload) (*gen.PreviewSpendRuleResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	if payload.LimitUsd <= 0 {
		return nil, oops.E(oops.CodeBadRequest, nil, "limit must be greater than zero")
	}
	if payload.Target == nil {
		return nil, oops.E(oops.CodeBadRequest, nil, "target is required")
	}
	ruleExpr := DefaultRuleExpr
	if _, err := s.celEng.Compile(ruleExpr); err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid rule expression: %s", err.Error())
	}

	now := time.Now().UTC()
	windowStart, windowEnd, err := WindowBounds(payload.WindowKind, now)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid window kind")
	}

	actors, err := LoadActors(ctx, repo.New(s.db), authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load org actors").LogError(ctx, s.logger)
	}

	targetExpr, err := targetConditionExpr(payload.Target.Attribute, payload.Target.Operator, payload.Target.Value)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid target")
	}

	matched, err := MatchActors(s.celEng, targetExpr, actors)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid target expression: %s", err.Error())
	}

	spendByEmail := map[string]float64{}
	if len(matched) > 0 {
		spendByEmail, err = s.actorSpendByEmail(ctx, authCtx.ActiveOrganizationID, windowStart, now)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "load actor spend").LogError(ctx, s.logger)
		}
	}

	usages := BuildActorUsages(matched, spendByEmail, payload.LimitUsd)
	usages, err = EvalRuleUsages(s.celEng, ruleExpr, int32(payload.WarnAtPct), usages) //nolint:gosec // design constrains warn_at_pct to 1..100
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid rule expression: %s", err.Error())
	}
	capped := usages
	if len(capped) > previewActorCap {
		capped = capped[:previewActorCap]
	}

	actorViews := make([]*gen.SpendRuleActorUsage, 0, len(capped))
	for _, u := range capped {
		actorViews = append(actorViews, &gen.SpendRuleActorUsage{
			Email:       u.Actor.Email,
			DisplayName: conv.PtrEmpty(u.Actor.DisplayName),
			UserID:      conv.PtrEmpty(u.Actor.UserID),
			SpendUsd:    u.SpendUSD,
			LimitUsd:    u.LimitUSD,
			UsedPct:     u.UsedPct,
			Breached:    u.Breached,
		})
	}

	return &gen.PreviewSpendRuleResult{
		MatchedCount: len(matched),
		WindowStart:  windowStart.Format(time.RFC3339),
		WindowEnd:    windowEnd.Format(time.RFC3339),
		Actors:       actorViews,
	}, nil
}

func (s *Service) ListSpendRuleEvents(ctx context.Context, payload *gen.ListSpendRuleEventsPayload) (*gen.ListSpendRuleEventsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	limit := defaultEventsPageLimit
	if payload.Limit != nil {
		limit = *payload.Limit
	}

	var ruleID uuid.NullUUID
	if payload.RuleID != nil && *payload.RuleID != "" {
		parsed, err := uuid.Parse(*payload.RuleID)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid rule id")
		}
		ruleID = uuid.NullUUID{UUID: parsed, Valid: true}
	}

	var cursor uuid.NullUUID
	if payload.Cursor != nil && *payload.Cursor != "" {
		parsed, err := uuid.Parse(*payload.Cursor)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid cursor")
		}
		cursor = uuid.NullUUID{UUID: parsed, Valid: true}
	}

	rows, err := repo.New(s.db).ListSpendRuleEvents(ctx, repo.ListSpendRuleEventsParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		SpendRuleID:    ruleID,
		EventType:      conv.PtrToPGTextEmpty(payload.EventType),
		CursorID:       cursor,
		PageLimit:      int32(limit),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list spend rule events").LogError(ctx, s.logger)
	}

	events := make([]*gen.SpendRuleEvent, 0, len(rows))
	for _, row := range rows {
		events = append(events, &gen.SpendRuleEvent{
			ID:          row.ID.String(),
			RuleID:      row.SpendRuleID.String(),
			RuleUrn:     row.RuleUrn,
			RuleName:    row.RuleName,
			EventType:   row.EventType,
			UserID:      conv.FromPGText[string](row.UserID),
			Email:       row.Email,
			DisplayName: conv.FromPGText[string](row.DisplayName),
			SpendUsd:    row.SpendUsd,
			LimitUsd:    row.LimitUsd,
			WindowStart: row.WindowStart.Time.UTC().Format(time.RFC3339),
			WindowEnd:   row.WindowEnd.Time.UTC().Format(time.RFC3339),
			CreatedAt:   row.CreatedAt.Time.UTC().Format(time.RFC3339),
		})
	}

	var nextCursor *string
	if len(rows) == limit && len(rows) > 0 {
		lastID := rows[len(rows)-1].ID.String()
		nextCursor = &lastID
	}

	return &gen.ListSpendRuleEventsResult{
		Events:     events,
		NextCursor: nextCursor,
	}, nil
}

func (s *Service) GetSpendRulesOverview(ctx context.Context, payload *gen.GetSpendRulesOverviewPayload) (*gen.SpendRulesOverviewResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	rules, err := repo.New(s.db).ListEnabledSpendRules(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list enabled spend rules").LogError(ctx, s.logger)
	}

	result := &gen.SpendRulesOverviewResult{
		TotalSpendUsd:      0,
		TotalBudgetUsd:     0,
		UsersBreached:      0,
		UsersTotal:         0,
		RulesUnhealthy:     0,
		RulesTotal:         len(rules),
		SpendOverBudgetUsd: 0,
		Rules:              []*gen.SpendRuleUsage{},
	}
	if len(rules) == 0 {
		return result, nil
	}

	actors, err := LoadActors(ctx, repo.New(s.db), authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load org actors").LogError(ctx, s.logger)
	}

	now := time.Now().UTC()
	projects, err := projectsRepo.New(s.db).ListProjectsByOrganization(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list organization projects").LogError(ctx, s.logger)
	}
	projectIDs := make([]string, 0, len(projects))
	for _, p := range projects {
		projectIDs = append(projectIDs, p.ID.String())
	}
	actorWindowSpend, err := chrepo.LoadActorWindowSpend(ctx, chrepo.New(s.chConn), projectIDs, now)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load actor window spend").LogError(ctx, s.logger)
	}

	matchedEmails := map[string]struct{}{}
	breachedEmails := map[string]struct{}{}

	for _, rule := range rules {
		matched, err := MatchActors(s.celEng, rule.TargetExpr, actors)
		if err != nil {
			s.logger.ErrorContext(ctx, "match spend rule actors", attr.SlogError(err), attr.SlogOrganizationID(rule.OrganizationID))
			continue
		}

		_, _, err = WindowBounds(rule.WindowKind, now)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "compute window bounds").LogError(ctx, s.logger)
		}

		usages := []ActorUsage{}
		if len(matched) > 0 {
			usages, err = BuildActorWindowUsages(matched, actorWindowSpend, rule.WindowKind, rule.LimitUsd)
			if err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "load actor spend").LogError(ctx, s.logger)
			}
		}

		usages, err = EvalRuleUsages(s.celEng, rule.RuleExpr, rule.WarnAtPct, usages)
		if err != nil {
			s.logger.ErrorContext(ctx, "evaluate spend rule expression", attr.SlogError(err), attr.SlogOrganizationID(rule.OrganizationID))
			continue
		}

		ruleSpend := 0.0
		usersWarned := 0
		usersBreached := 0
		worstPct := 0.0
		for _, u := range usages {
			ruleSpend += u.SpendUSD
			email := conv.NormalizeEmail(u.Actor.Email)
			matchedEmails[email] = struct{}{}
			if u.Breached {
				usersBreached++
				breachedEmails[email] = struct{}{}
			} else if u.UsedPct >= float64(rule.WarnAtPct) {
				usersWarned++
			}
			if u.SpendUSD > u.LimitUSD {
				result.SpendOverBudgetUsd += u.SpendUSD - u.LimitUSD
			}
			if u.UsedPct > worstPct {
				worstPct = u.UsedPct
			}
		}

		budget := rule.LimitUsd * float64(len(matched))
		status := RuleStatus(rule.Action, rule.WarnAtPct, usages)

		result.TotalSpendUsd += ruleSpend
		result.TotalBudgetUsd += budget
		if status != StatusHealthy {
			result.RulesUnhealthy++
		}

		result.Rules = append(result.Rules, &gen.SpendRuleUsage{
			RuleID:        rule.ID.String(),
			MatchedUsers:  len(matched),
			UsersWarned:   usersWarned,
			UsersBreached: usersBreached,
			SpendUsd:      ruleSpend,
			BudgetUsd:     budget,
			WorstUsedPct:  worstPct,
			Status:        status,
		})
	}

	result.UsersTotal = len(matchedEmails)
	result.UsersBreached = len(breachedEmails)

	return result, nil
}

// actorSpendByEmail sums each actor's LLM cost across the organization's
// projects over [from, now], keyed by normalized email.
func (s *Service) actorSpendByEmail(ctx context.Context, organizationID string, from, to time.Time) (map[string]float64, error) {
	projects, err := projectsRepo.New(s.db).ListProjectsByOrganization(ctx, organizationID)
	if err != nil {
		return nil, fmt.Errorf("list organization projects: %w", err)
	}
	projectIDs := make([]string, 0, len(projects))
	for _, p := range projects {
		projectIDs = append(projectIDs, p.ID.String())
	}

	rows, err := chrepo.New(s.chConn).ListActorSpendForRules(ctx, projectIDs, from.UnixNano(), to.UnixNano())
	if err != nil {
		return nil, fmt.Errorf("list actor spend: %w", err)
	}

	spend := make(map[string]float64, len(rows))
	for _, row := range rows {
		spend[conv.NormalizeEmail(row.Email)] += row.TotalCost
	}
	return spend, nil
}

func (s *Service) signalEvaluation(ctx context.Context, organizationID string) {
	if s.signaler == nil {
		return
	}
	if err := s.signaler.Signal(ctx, organizationID); err != nil {
		s.logger.ErrorContext(ctx, "signal spend rule evaluation", attr.SlogError(err), attr.SlogOrganizationID(organizationID))
	}
}

// ruleSlug derives the URN slug for a new rule from its name, appending a
// random suffix when the plain slug is already taken in the organization.
// Slugs are immutable after creation: the versioned rule URN embeds them, so
// a rename must not detach historical events from the rule.
func ruleSlug(ctx context.Context, queries *repo.Queries, organizationID, name string) (string, error) {
	slug := conv.URLToSlug(name)
	if len(slug) > 60 {
		slug = strings.TrimRight(slug[:60], "-")
	}
	if slug == "" {
		slug = "rule"
	}

	taken, err := queries.SpendRuleSlugExists(ctx, repo.SpendRuleSlugExistsParams{
		OrganizationID: organizationID,
		Slug:           slug,
	})
	if err != nil {
		return "", fmt.Errorf("check spend rule slug: %w", err)
	}
	if !taken {
		return slug, nil
	}

	suffix, err := conv.GenerateRandomSlug(5)
	if err != nil {
		return "", fmt.Errorf("generate spend rule slug suffix: %w", err)
	}
	return slug + "-" + suffix, nil
}

var (
	targetComparisonExpr = regexp.MustCompile(`^([A-Za-z_]\w*)\s*(==|!=)\s*("(?:\\.|[^"])*")$`)
	targetCallExpr       = regexp.MustCompile(`^([A-Za-z_]\w*)\.(startsWith|endsWith|contains|matches)\(("(?:\\.|[^"])*")\)$`)
	targetInExpr         = regexp.MustCompile(`^("(?:\\.|[^"])*")\s+in\s+([A-Za-z_]\w*)$`)
)

var targetAttributeTypes = map[string]string{
	"email":            "string",
	"department_name":  "string",
	"job_title":        "string",
	"employee_type":    "string",
	"division_name":    "string",
	"cost_center_name": "string",
	"groups":           "list",
	"roles":            "list",
}

func targetConditionExpr(attribute, operator, value string) (string, error) {
	attrType, ok := targetAttributeTypes[attribute]
	if !ok {
		return "", fmt.Errorf("unknown target attribute %q", attribute)
	}
	quoted := strconv.Quote(value)
	switch operator {
	case "equals":
		if attrType != "string" {
			return "", fmt.Errorf("operator %q requires a string attribute", operator)
		}
		return attribute + " == " + quoted, nil
	case "not_equals":
		if attrType != "string" {
			return "", fmt.Errorf("operator %q requires a string attribute", operator)
		}
		return attribute + " != " + quoted, nil
	case "starts_with":
		if attrType != "string" {
			return "", fmt.Errorf("operator %q requires a string attribute", operator)
		}
		return attribute + ".startsWith(" + quoted + ")", nil
	case "ends_with":
		if attrType != "string" {
			return "", fmt.Errorf("operator %q requires a string attribute", operator)
		}
		return attribute + ".endsWith(" + quoted + ")", nil
	case "contains":
		if attrType != "string" {
			return "", fmt.Errorf("operator %q requires a string attribute", operator)
		}
		return attribute + ".contains(" + quoted + ")", nil
	case "matches":
		if attrType != "string" {
			return "", fmt.Errorf("operator %q requires a string attribute", operator)
		}
		return attribute + ".matches(" + quoted + ")", nil
	case "includes":
		if attrType != "list" {
			return "", fmt.Errorf("operator %q requires a list attribute", operator)
		}
		return quoted + " in " + attribute, nil
	default:
		return "", fmt.Errorf("unknown target operator %q", operator)
	}
}

func targetConditionFromExpr(expr string) *types.SpendRuleTargetCondition {
	if match := targetComparisonExpr.FindStringSubmatch(expr); match != nil {
		value, err := strconv.Unquote(match[3])
		if err != nil {
			value = ""
		}
		operator := "equals"
		if match[2] == "!=" {
			operator = "not_equals"
		}
		return &types.SpendRuleTargetCondition{
			Attribute: match[1],
			Operator:  operator,
			Value:     value,
		}
	}
	if match := targetCallExpr.FindStringSubmatch(expr); match != nil {
		value, err := strconv.Unquote(match[3])
		if err != nil {
			value = ""
		}
		operator := map[string]string{
			"startsWith": "starts_with",
			"endsWith":   "ends_with",
			"contains":   "contains",
			"matches":    "matches",
		}[match[2]]
		return &types.SpendRuleTargetCondition{
			Attribute: match[1],
			Operator:  operator,
			Value:     value,
		}
	}
	if match := targetInExpr.FindStringSubmatch(expr); match != nil {
		value, err := strconv.Unquote(match[1])
		if err != nil {
			value = ""
		}
		return &types.SpendRuleTargetCondition{
			Attribute: match[2],
			Operator:  "includes",
			Value:     value,
		}
	}
	return &types.SpendRuleTargetCondition{
		Attribute: "email",
		Operator:  "contains",
		Value:     "",
	}
}

func buildSpendRuleView(row repo.SpendRule) *types.SpendRule {
	return &types.SpendRule{
		ID:             row.ID.String(),
		Urn:            urn.NewSpendRule(row.Slug, row.Version).String(),
		OrganizationID: row.OrganizationID,
		Name:           row.Name,
		Slug:           row.Slug,
		Description:    row.Description,
		Target:         targetConditionFromExpr(row.TargetExpr),
		TargetExpr:     row.TargetExpr,
		RuleExpr:       row.RuleExpr,
		LimitUsd:       row.LimitUsd,
		WindowKind:     row.WindowKind,
		WarnAtPct:      int(row.WarnAtPct),
		Action:         row.Action,
		Enabled:        row.Enabled,
		Version:        row.Version,
		CreatedAt:      row.CreatedAt.Time.UTC().Format(time.RFC3339),
		UpdatedAt:      row.UpdatedAt.Time.UTC().Format(time.RFC3339),
	}
}
