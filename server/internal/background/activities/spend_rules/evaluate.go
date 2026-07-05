// Package spend_rules holds the background activities that evaluate spend
// control rules: per-organization budget evaluation against ClickHouse spend,
// warning/breach event writes, and spend gate snapshot publication for the
// hooks gate.
package spend_rules

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/conv"
	projectsRepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/spendrules"
	"github.com/speakeasy-api/gram/server/internal/spendrules/celenv"
	spendrepo "github.com/speakeasy-api/gram/server/internal/spendrules/repo"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// ListOrgs returns the organizations that currently have enabled spend rules.
type ListOrgs struct {
	logger *slog.Logger
	db     *pgxpool.Pool
}

func NewListOrgs(logger *slog.Logger, db *pgxpool.Pool) *ListOrgs {
	return &ListOrgs{
		logger: logger.With(attr.SlogComponent("spend_rules_list_orgs")),
		db:     db,
	}
}

func (a *ListOrgs) Do(ctx context.Context) ([]string, error) {
	orgs, err := spendrepo.New(a.db).ListOrganizationsWithEnabledSpendRules(ctx)
	if err != nil {
		return nil, fmt.Errorf("list organizations with enabled spend rules: %w", err)
	}
	return orgs, nil
}

// EvaluateOrg evaluates every enabled spend rule for one organization:
// CEL-matches directory actors, sums their window spend from ClickHouse,
// records warning/breach events (deduped by the unique index), and replaces
// the organization's spend gate snapshot.
type EvaluateOrg struct {
	logger    *slog.Logger
	tracer    trace.Tracer
	db        *pgxpool.Pool
	chQueries *telemetryrepo.Queries
	cacheImpl cache.Cache
	celEng    *celenv.Engine
}

func NewEvaluateOrg(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	chQueries *telemetryrepo.Queries,
	cacheImpl cache.Cache,
) *EvaluateOrg {
	logger = logger.With(attr.SlogComponent("spend_rules_evaluate_org"))

	// The CEL environment is deterministic to build; an error here is a code
	// bug. Keep the nil engine and fail evaluation loudly rather than
	// panicking worker startup.
	celEng, err := celenv.New()
	if err != nil {
		logger.ErrorContext(context.Background(), "build spend rules CEL engine", attr.SlogError(err))
	}

	return &EvaluateOrg{
		logger:    logger,
		tracer:    tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/background/activities/spend_rules"),
		db:        db,
		chQueries: chQueries,
		cacheImpl: cacheImpl,
		celEng:    celEng,
	}
}

type EvaluateOrgArgs struct {
	OrganizationID string
}

func (a *EvaluateOrg) Do(ctx context.Context, args EvaluateOrgArgs) (err error) {
	if a.celEng == nil {
		return fmt.Errorf("spend rules CEL engine unavailable")
	}
	if a.chQueries == nil {
		return fmt.Errorf("clickhouse queries unavailable")
	}
	if a.cacheImpl == nil {
		return fmt.Errorf("cache unavailable")
	}

	ctx, span := a.tracer.Start(ctx, "spendrules.evaluateOrg")
	defer func() {
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	logger := a.logger.With(attr.SlogOrganizationID(args.OrganizationID))
	queries := spendrepo.New(a.db)

	rules, err := queries.ListEnabledSpendRules(ctx, args.OrganizationID)
	if err != nil {
		return fmt.Errorf("list enabled spend rules: %w", err)
	}

	if len(rules) == 0 {
		if err := spendrules.WriteGateState(ctx, a.cacheImpl, args.OrganizationID, spendrules.GateState{Rules: nil, Actors: nil}); err != nil {
			return fmt.Errorf("clear spend gate snapshot: %w", err)
		}
		return nil
	}

	actors, err := spendrules.LoadActors(ctx, queries, args.OrganizationID)
	if err != nil {
		return fmt.Errorf("load directory actors: %w", err)
	}

	projects, err := projectsRepo.New(a.db).ListProjectsByOrganization(ctx, args.OrganizationID)
	if err != nil {
		return fmt.Errorf("list organization projects: %w", err)
	}
	projectIDs := make([]string, 0, len(projects))
	for _, p := range projects {
		projectIDs = append(projectIDs, p.ID.String())
	}

	now := time.Now().UTC()
	gateState := spendrules.NewGateState(actors)

	for _, rule := range rules {
		if err := a.evaluateRule(ctx, logger, queries, rule, actors, projectIDs, now, &gateState); err != nil {
			// One broken rule (e.g. an expression that no longer compiles)
			// must not stall evaluation of the org's other rules.
			logger.ErrorContext(ctx, "evaluate spend rule", attr.SlogError(err))
		}
	}

	if err := spendrules.WriteGateState(ctx, a.cacheImpl, args.OrganizationID, gateState); err != nil {
		return fmt.Errorf("write spend gate snapshot: %w", err)
	}

	return nil
}

func (a *EvaluateOrg) evaluateRule(
	ctx context.Context,
	logger *slog.Logger,
	queries *spendrepo.Queries,
	rule spendrepo.SpendRule,
	actors []spendrules.Actor,
	projectIDs []string,
	now time.Time,
	gateState *spendrules.GateState,
) error {
	windowStart, windowEnd, err := spendrules.WindowBounds(rule.WindowKind, now)
	if err != nil {
		return fmt.Errorf("window bounds for rule %s: %w", rule.ID, err)
	}
	ruleURN := urn.NewSpendRule(rule.Slug, rule.Version)

	matched, err := spendrules.MatchActors(a.celEng, rule.TargetExpr, actors)
	if err != nil {
		return fmt.Errorf("match actors for rule %s: %w", rule.ID, err)
	}
	gateState.Rules = append(gateState.Rules, spendrules.GateRule{
		RuleURN:    ruleURN.String(),
		RuleName:   rule.Name,
		Action:     rule.Action,
		TargetExpr: rule.TargetExpr,
		RuleExpr:   rule.RuleExpr,
		LimitUSD:   rule.LimitUsd,
		WarnAtPct:  rule.WarnAtPct,
		WindowEnd:  windowEnd,
	})
	if len(matched) == 0 {
		return nil
	}

	spendFrom := spendrules.SpendRangeStart(windowStart, rule.EvaluatedFrom.Time)

	spendRows, err := a.chQueries.ListActorSpendForRules(ctx, projectIDs, spendFrom.UnixNano(), now.UnixNano())
	if err != nil {
		return fmt.Errorf("list actor spend for rule %s: %w", rule.ID, err)
	}
	spendByEmail := make(map[string]float64, len(spendRows))
	for _, row := range spendRows {
		spendByEmail[conv.NormalizeEmail(row.Email)] += row.TotalCost
	}

	usages := spendrules.BuildActorUsages(matched, spendByEmail, rule.LimitUsd)
	usages, err = spendrules.EvalRuleUsages(a.celEng, rule.RuleExpr, rule.WarnAtPct, usages)
	if err != nil {
		return fmt.Errorf("evaluate rule expression for rule %s: %w", rule.ID, err)
	}

	for _, usage := range usages {
		gateState.SetUsage(ruleURN.String(), usage)

		breached := usage.Breached
		warned := !breached && usage.UsedPct >= float64(rule.WarnAtPct)

		if !breached && !warned {
			continue
		}

		eventType := spendrules.EventTypeWarning
		if breached {
			eventType = spendrules.EventTypeBreach
		}

		if _, err := queries.InsertSpendRuleEvent(ctx, spendrepo.InsertSpendRuleEventParams{
			OrganizationID: rule.OrganizationID,
			SpendRuleID:    rule.ID,
			RuleUrn:        ruleURN.String(),
			EventType:      eventType,
			UserID:         conv.ToPGTextEmpty(usage.Actor.UserID),
			Email:          conv.NormalizeEmail(usage.Actor.Email),
			DisplayName:    conv.ToPGTextEmpty(usage.Actor.DisplayName),
			SpendUsd:       usage.SpendUSD,
			LimitUsd:       usage.LimitUSD,
			WindowStart:    conv.ToPGTimestamptz(windowStart),
			WindowEnd:      conv.ToPGTimestamptz(windowEnd),
		}); err != nil {
			logger.ErrorContext(ctx, "record spend rule event", attr.SlogError(err))
		}

	}

	return nil
}
