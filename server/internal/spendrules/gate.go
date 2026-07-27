package spendrules

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	redisCache "github.com/go-redis/cache/v9"
	"github.com/google/cel-go/cel"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/spendrules/celenv"
	"github.com/speakeasy-api/gram/server/internal/spendrules/chrepo"
)

// EvaluationInterval is how often the background reconciler recomputes spend
// against every enabled rule and repairs actor spend cache entries.
const EvaluationInterval = 30 * time.Second

// spendGateTTL bounds how long request-path gate data survives without an API
// write or evaluator refresh. It must comfortably exceed one scheduled sweep so
// a slow-but-progressing reconciler does not let entries expire and enforcement
// fail open between cycles.
const spendGateTTL = 30 * time.Minute

// maxGatePrograms bounds the compiled-CEL cache. Rule edits mint new target
// and rule expressions over a long-lived gate process, so the cache is flushed
// wholesale once it grows past this cap rather than retaining every historical
// expression forever. Recompilation after a flush is cheap next to the
// hot-path cache hit.
const maxGatePrograms = 1024

// Block describes why an actor is currently blocked by a spend rule.
type Block struct {
	RuleURN  string `json:"rule_urn"`
	RuleName string `json:"rule_name"`
	// WindowEnd is when the budget window resets and the block lifts.
	WindowEnd time.Time `json:"window_end"`
}

// GateRule is the request-path view of a spend rule. The API refreshes these
// on rule mutations so the hook gate can evaluate the same CEL predicates as
// background evaluation without touching Postgres or ClickHouse.
type GateRule struct {
	RuleURN     string    `json:"rule_urn"`
	RuleName    string    `json:"rule_name"`
	Action      string    `json:"action"`
	TargetExpr  string    `json:"target_expr"`
	RuleExpr    string    `json:"rule_expr"`
	LimitUSD    float64   `json:"limit_usd"`
	WarnAtPct   int32     `json:"warn_at_pct"`
	WindowKind  string    `json:"window_kind"`
	WindowStart time.Time `json:"window_start"`
	WindowEnd   time.Time `json:"window_end"`
}

type GateRules struct {
	SourceUpdatedAt time.Time  `json:"source_updated_at"`
	Rules           []GateRule `json:"rules"`
}

type GateActor struct {
	ComputedAt  time.Time                  `json:"computed_at"`
	UserID      string                     `json:"user_id"`
	Email       string                     `json:"email"`
	DisplayName string                     `json:"display_name"`
	Attrs       celenv.Actor               `json:"attrs"`
	Spend       chrepo.ActorWindowSpendRow `json:"spend"`
}

type mutatingCache interface {
	Mutate(ctx context.Context, key string, value any, ttl time.Duration, fn func(exists bool) error) error
}

func spendGateRulesKey(organizationID string) string {
	return "spend_gate:rules:" + organizationID
}

func spendGateActorKey(organizationID, userID string) string {
	if organizationID == "" || userID == "" {
		return ""
	}
	return "spend_gate:actor:" + organizationID + ":" + userID
}

// Gate is the hot-path spend check consulted by the Claude hooks handlers
// before risk-policy scans. Reads stay Redis-only; every failure mode resolves
// to "not blocked" (fail-open) so a cache outage never denies traffic.
type Gate struct {
	logger       *slog.Logger
	cache        cache.Cache
	celEng       *celenv.Engine
	programs     sync.Map
	programCount atomic.Int64
}

func NewGate(logger *slog.Logger, cacheImpl cache.Cache, celEng *celenv.Engine) (*Gate, error) {
	if celEng == nil {
		return nil, fmt.Errorf("spend gate CEL engine is required")
	}
	if cacheImpl == nil {
		// The hot-path CheckBlocked dereferences the cache on every call;
		// a nil cache would panic there rather than failing open, so reject
		// it at construction.
		return nil, fmt.Errorf("spend gate cache is required")
	}

	return &Gate{
		logger:       logger.With(attr.SlogComponent("spendrules_gate")),
		cache:        cacheImpl,
		celEng:       celEng,
		programs:     sync.Map{},
		programCount: atomic.Int64{},
	}, nil
}

// CheckBlocked reports whether the given actor is currently blocked by a spend
// rule. The actor is looked up only by stable Gram user ID. A nil Block means
// the actor is not blocked. Errors are cache infrastructure failures — callers
// should treat them as "not blocked" (fail-open); they are returned for logging.
func (g *Gate) CheckBlocked(ctx context.Context, organizationID, userID string) (*Block, error) {
	actorKey := spendGateActorKey(organizationID, userID)
	if actorKey == "" || g.celEng == nil {
		return nil, nil
	}

	var rules GateRules
	err := g.cache.Get(ctx, spendGateRulesKey(organizationID), &rules)
	if errors.Is(err, redisCache.ErrCacheMiss) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read spend gate rules: %w", err)
	}
	if len(rules.Rules) == 0 {
		return nil, nil
	}

	var actor GateActor
	err = g.cache.Get(ctx, actorKey, &actor)
	if errors.Is(err, redisCache.ErrCacheMiss) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read spend gate actor: %w", err)
	}

	now := time.Now().UTC()
	for _, rule := range rules.Rules {
		if rule.Action != ActionBlock {
			continue
		}

		// The actor entry holds spend for the window that was current when the
		// evaluator last wrote it. If rules have advanced to a newer window,
		// ignore the stale actor entry until the next refresh.
		if !rule.WindowStart.IsZero() && actor.ComputedAt.Before(rule.WindowStart) {
			continue
		}

		// Once the rule window has ended, the block must lift immediately
		// rather than keep denying on stale spend until the next refresh.
		if !rule.WindowEnd.IsZero() && !now.Before(rule.WindowEnd) {
			continue
		}

		spendUSD, err := actor.Spend.SpendUSD(rule.WindowKind)
		if err != nil {
			g.logger.WarnContext(ctx, "read spend gate window spend",
				attr.SlogError(err),
				attr.SlogOrganizationID(organizationID),
			)
			continue
		}
		usedPct := 0.0
		if rule.LimitUSD > 0 {
			usedPct = spendUSD / rule.LimitUSD * 100
		}
		attrs := actor.Attrs
		attrs.SpendUSD = spendUSD
		attrs.LimitUSD = rule.LimitUSD
		attrs.UsedPct = usedPct
		attrs.WarnAtPct = float64(rule.WarnAtPct)

		matched, err := g.eval(ctx, organizationID, rule.TargetExpr, attrs)
		if err != nil || !matched {
			continue
		}

		ruleExpr := conv.Default(rule.RuleExpr, DefaultRuleExpr)
		breached, err := g.eval(ctx, organizationID, ruleExpr, attrs)
		if err != nil || !breached {
			continue
		}

		return &Block{
			RuleURN:   rule.RuleURN,
			RuleName:  rule.RuleName,
			WindowEnd: rule.WindowEnd,
		}, nil
	}

	return nil, nil
}

func (g *Gate) eval(ctx context.Context, organizationID, expr string, actor celenv.Actor) (bool, error) {
	prg, err := g.compile(expr)
	if err != nil {
		g.logger.WarnContext(ctx, "compile spend gate expression",
			attr.SlogError(err),
			attr.SlogOrganizationID(organizationID),
		)
		return false, err
	}

	ok, err := g.celEng.Eval(prg, actor)
	if err != nil {
		g.logger.WarnContext(ctx, "evaluate spend gate expression",
			attr.SlogError(err),
			attr.SlogOrganizationID(organizationID),
		)
		return false, fmt.Errorf("evaluate spend gate expression: %w", err)
	}
	return ok, nil
}

func (g *Gate) compile(expr string) (cel.Program, error) {
	if prg, ok := g.programs.Load(expr); ok {
		typed, ok := prg.(cel.Program)
		if !ok {
			return nil, fmt.Errorf("cached spend gate program has unexpected type %T", prg)
		}
		return typed, nil
	}
	prg, err := g.celEng.Compile(expr)
	if err != nil {
		return nil, fmt.Errorf("compile spend gate expression: %w", err)
	}
	actual, loaded := g.programs.LoadOrStore(expr, prg)
	if !loaded && g.programCount.Add(1) > maxGatePrograms {
		// Soft bound: flush wholesale once the cache outgrows the cap so
		// repeated rule edits cannot grow program memory without limit. The
		// count can drift slightly under concurrency; that only affects when
		// the next flush happens, not correctness.
		g.programs.Clear()
		g.programCount.Store(0)
	}
	typed, ok := actual.(cel.Program)
	if !ok {
		return nil, fmt.Errorf("cached spend gate program has unexpected type %T", actual)
	}
	return typed, nil
}

func NewGateActor(actor Actor, spend chrepo.ActorWindowSpendRow, computedAt time.Time) GateActor {
	return GateActor{
		ComputedAt:  computedAt.UTC(),
		UserID:      actor.UserID,
		Email:       actor.Email,
		DisplayName: actor.DisplayName,
		Attrs:       actor.Attrs,
		Spend:       spend,
	}
}

func EmptyActorSpend(email string) chrepo.ActorWindowSpendRow {
	return chrepo.ActorWindowSpendRow{
		Email:       conv.NormalizeEmail(email),
		DailyCost:   0,
		WeeklyCost:  0,
		MonthlyCost: 0,
	}
}

// WriteGateRules replaces the organization's request-path rules only when the
// payload was built from at least as fresh a DB source as the existing value.
func WriteGateRules(ctx context.Context, cacheImpl cache.Cache, organizationID string, rules GateRules) error {
	key := spendGateRulesKey(organizationID)
	rules.SourceUpdatedAt = rules.SourceUpdatedAt.UTC()
	if rules.Rules == nil {
		rules.Rules = []GateRule{}
	}
	if mutable, ok := cacheImpl.(mutatingCache); ok {
		current := GateRules{SourceUpdatedAt: time.Time{}, Rules: nil}
		if err := mutable.Mutate(ctx, key, &current, spendGateTTL, func(exists bool) error {
			if exists && current.SourceUpdatedAt.After(rules.SourceUpdatedAt) {
				return nil
			}
			current = rules
			return nil
		}); err != nil {
			return fmt.Errorf("write spend gate rules: %w", err)
		}
		return nil
	}
	var current GateRules
	err := cacheImpl.Get(ctx, key, &current)
	if err == nil && current.SourceUpdatedAt.After(rules.SourceUpdatedAt) {
		return nil
	}
	if err != nil && !errors.Is(err, redisCache.ErrCacheMiss) {
		if cacheImpl == cache.NoopCache {
			return nil
		}
		return fmt.Errorf("read spend gate rules for write: %w", err)
	}
	if err := cacheImpl.Set(ctx, key, rules, spendGateTTL); err != nil {
		return fmt.Errorf("write spend gate rules: %w", err)
	}
	return nil
}

// WriteGateActor stores one actor's ClickHouse spend snapshot. Concurrent
// refreshers use write-if-newer semantics so an older org reconciler run cannot
// overwrite a fresher per-actor usage-triggered refresh.
func WriteGateActor(ctx context.Context, cacheImpl cache.Cache, organizationID string, actor GateActor) error {
	key := spendGateActorKey(organizationID, actor.UserID)
	if key == "" {
		return nil
	}
	actor.ComputedAt = actor.ComputedAt.UTC()
	if mutable, ok := cacheImpl.(mutatingCache); ok {
		current := GateActor{
			ComputedAt:  time.Time{},
			UserID:      "",
			Email:       "",
			DisplayName: "",
			Attrs: celenv.Actor{
				Email:          "",
				DepartmentName: "",
				JobTitle:       "",
				EmployeeType:   "",
				DivisionName:   "",
				CostCenterName: "",
				Groups:         nil,
				Roles:          nil,
				SpendUSD:       0,
				LimitUSD:       0,
				UsedPct:        0,
				WarnAtPct:      0,
			},
			Spend: chrepo.ActorWindowSpendRow{Email: "", DailyCost: 0, WeeklyCost: 0, MonthlyCost: 0},
		}
		if err := mutable.Mutate(ctx, key, &current, spendGateTTL, func(exists bool) error {
			if exists && current.ComputedAt.After(actor.ComputedAt) {
				return nil
			}
			current = actor
			return nil
		}); err != nil {
			return fmt.Errorf("write spend gate actor: %w", err)
		}
		return nil
	}
	var current GateActor
	err := cacheImpl.Get(ctx, key, &current)
	if err == nil && current.ComputedAt.After(actor.ComputedAt) {
		return nil
	}
	if err != nil && !errors.Is(err, redisCache.ErrCacheMiss) {
		if cacheImpl == cache.NoopCache {
			return nil
		}
		return fmt.Errorf("read spend gate actor for write: %w", err)
	}
	if err := cacheImpl.Set(ctx, key, actor, spendGateTTL); err != nil {
		return fmt.Errorf("write spend gate actor: %w", err)
	}
	return nil
}
