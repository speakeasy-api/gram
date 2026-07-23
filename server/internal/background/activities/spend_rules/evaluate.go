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
	"github.com/speakeasy-api/gram/server/internal/feature"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsRepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/spendrules"
	"github.com/speakeasy-api/gram/server/internal/spendrules/celenv"
	chrepo "github.com/speakeasy-api/gram/server/internal/spendrules/chrepo"
	spendrepo "github.com/speakeasy-api/gram/server/internal/spendrules/repo"
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
	chQueries *chrepo.Queries
	cacheImpl cache.Cache
	celEng    *celenv.Engine
	flags     feature.Provider
}

func NewEvaluateOrg(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	chQueries *chrepo.Queries,
	cacheImpl cache.Cache,
	flags feature.Provider,
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
		flags:     flags,
	}
}

type EvaluateOrgArgs struct {
	OrganizationID string
}

func (a *EvaluateOrg) Do(ctx context.Context, args EvaluateOrgArgs) (err error) {
	ctx, span := a.tracer.Start(ctx, "spendrules.evaluateOrg")
	defer func() {
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	logger := a.logger.With(attr.SlogOrganizationID(args.OrganizationID))
	queries := spendrepo.New(a.db)

	// The Budgets rollout flag (feature.FlagBudgets) gates enforcement org by
	// org — the same key hides the dashboard surface. When the flag is off
	// the org's rules stay dormant: no warning/breach events are recorded and
	// the gate snapshot is cleared, so an already-open circuit lifts on this
	// cycle instead of blocking users on a feature they cannot see.
	if !a.budgetsEnabled(ctx, logger, args.OrganizationID) {
		if err := spendrules.WriteGateState(ctx, a.cacheImpl, args.OrganizationID, spendrules.GateState{Rules: nil, Actors: nil}); err != nil {
			return fmt.Errorf("clear spend gate snapshot: %w", err)
		}
		return nil
	}

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
	actorWindowSpend, err := chrepo.LoadActorWindowSpend(ctx, a.chQueries, projectIDs, now)
	if err != nil {
		return fmt.Errorf("load actor window spend: %w", err)
	}

	gateState := spendrules.NewGateState(args.OrganizationID, actors)
	for _, actor := range actors {
		gateState.SetActorWindowSpend(args.OrganizationID, actor, actorWindowSpend[conv.NormalizeEmail(actor.Email)])
	}

	for _, rule := range rules {
		if err := a.evaluateRule(ctx, logger, queries, rule, actors, actorWindowSpend, now, &gateState); err != nil {
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

// budgetsEnabled reports whether the Budgets rollout flag is on for the
// organization. The flag is targeted by PostHog organization group (org
// slug), the same way the dashboard evaluates it, so the org slug is
// forwarded as the group key. A nil provider or a failed lookup degrades to
// disabled — enforcement stays off rather than blocking users on an
// unresolved flag. (The PostHog provider itself returns enabled when PostHog
// is disabled outright, e.g. in local development.)
func (a *EvaluateOrg) budgetsEnabled(ctx context.Context, logger *slog.Logger, organizationID string) bool {
	if a.flags == nil {
		return false
	}

	var groups map[string]string
	org, err := orgRepo.New(a.db).GetOrganizationMetadata(ctx, organizationID)
	if err != nil {
		// Group targeting degrades to distinct-id-only evaluation; a flag
		// released purely by org group will read as off for this org until
		// the metadata lookup recovers.
		logger.WarnContext(ctx, "resolve organization slug for budgets flag", attr.SlogError(err))
	} else {
		groups = feature.OrgProjectGroups(org.Slug, "")
	}

	on, err := a.flags.IsFlagEnabled(ctx, feature.FlagBudgets, organizationID, groups)
	if err != nil {
		logger.WarnContext(ctx, "budgets flag check failed; treating as disabled", attr.SlogError(err))
		return false
	}
	return on
}

func (a *EvaluateOrg) evaluateRule(
	ctx context.Context,
	logger *slog.Logger,
	queries *spendrepo.Queries,
	rule spendrepo.SpendRule,
	actors []spendrules.Actor,
	actorWindowSpend map[string]chrepo.ActorWindowSpendRow,
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
		WindowKind: rule.WindowKind,
		WindowEnd:  windowEnd,
	})
	if len(matched) == 0 {
		return nil
	}

	usages, err := spendrules.BuildActorWindowUsages(matched, actorWindowSpend, rule.WindowKind, rule.LimitUsd)
	if err != nil {
		return fmt.Errorf("select actor spend for rule %s: %w", rule.ID, err)
	}

	usages, err = spendrules.EvalRuleUsages(a.celEng, rule.RuleExpr, rule.WarnAtPct, usages)
	if err != nil {
		return fmt.Errorf("evaluate rule expression for rule %s: %w", rule.ID, err)
	}

	for _, usage := range usages {
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
