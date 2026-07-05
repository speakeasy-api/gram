package spendrules

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	redisCache "github.com/go-redis/cache/v9"
	"github.com/google/cel-go/cel"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/spendrules/celenv"
)

// EvaluationInterval is how often the background evaluator recomputes spend
// against every enabled rule and rewrites the spend gate snapshot.
const EvaluationInterval = 5 * time.Minute

// spendGateSnapshotTTL bounds how long a gate snapshot survives without the
// evaluator rewriting it: roughly two evaluation cycles, so a stalled
// evaluator fails open instead of blocking users on stale data forever.
const spendGateSnapshotTTL = 2 * EvaluationInterval

// Block describes why an actor is currently blocked by a spend rule.
type Block struct {
	RuleURN  string `json:"rule_urn"`
	RuleName string `json:"rule_name"`
	// WindowEnd is when the budget window resets and the block lifts.
	WindowEnd time.Time `json:"window_end"`
}

// GateRule is the request-path view of a spend rule. The evaluator refreshes
// these alongside actor usage so the hook gate can evaluate the same CEL
// predicates as background evaluation without touching Postgres or ClickHouse.
type GateRule struct {
	RuleURN    string    `json:"rule_urn"`
	RuleName   string    `json:"rule_name"`
	Action     string    `json:"action"`
	TargetExpr string    `json:"target_expr"`
	RuleExpr   string    `json:"rule_expr"`
	LimitUSD   float64   `json:"limit_usd"`
	WarnAtPct  int32     `json:"warn_at_pct"`
	WindowEnd  time.Time `json:"window_end"`
}

type GateUsage struct {
	SpendUSD float64 `json:"spend_usd"`
	LimitUSD float64 `json:"limit_usd"`
	UsedPct  float64 `json:"used_pct"`
}

type GateActor struct {
	UserID      string               `json:"user_id"`
	Email       string               `json:"email"`
	DisplayName string               `json:"display_name"`
	Attrs       celenv.Actor         `json:"attrs"`
	UsageByRule map[string]GateUsage `json:"usage_by_rule"`
}

// GateState is the full request-path state for one organization. Actors are
// keyed by Gram user id and normalized email, so a hook event can resolve its
// actor without scanning the organization.
type GateState struct {
	Rules  []GateRule           `json:"rules"`
	Actors map[string]GateActor `json:"actors"`
}

func spendGateSnapshotKey(organizationID string) string {
	return "spend_gate:" + organizationID
}

// Gate is the hot-path spend check consulted by the Claude hooks handlers
// before risk-policy scans. Reads are a single cache GET; every failure mode
// resolves to "not blocked" (fail-open) so a cache outage never denies
// traffic.
type Gate struct {
	logger   *slog.Logger
	cache    cache.Cache
	celEng   *celenv.Engine
	programs sync.Map
}

func NewGate(logger *slog.Logger, cacheImpl cache.Cache) *Gate {
	celEng, err := celenv.New()
	if err != nil {
		logger.ErrorContext(context.Background(), "build spend gate CEL engine", attr.SlogError(err))
	}

	return &Gate{
		logger:   logger.With(attr.SlogComponent("spendrules_gate")),
		cache:    cacheImpl,
		celEng:   celEng,
		programs: sync.Map{},
	}
}

// CheckBlocked reports whether the given actor is currently blocked by a
// spend rule. Either identifier may be empty. A nil Block means the actor is
// not blocked. Errors are cache infrastructure failures — callers should
// treat them as "not blocked" (fail-open); they are returned for logging.
func (g *Gate) CheckBlocked(ctx context.Context, organizationID, userID, email string) (*Block, error) {
	if organizationID == "" || (userID == "" && email == "") || g.celEng == nil {
		return nil, nil
	}

	var state GateState
	err := g.cache.Get(ctx, spendGateSnapshotKey(organizationID), &state)
	if errors.Is(err, redisCache.ErrCacheMiss) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read spend gate state: %w", err)
	}

	actor, ok := lookupGateActor(state.Actors, userID, email)
	if !ok {
		return nil, nil
	}

	for _, rule := range state.Rules {
		if rule.Action != ActionBlock {
			continue
		}

		usage := actor.UsageByRule[rule.RuleURN]
		if usage.LimitUSD == 0 {
			usage.LimitUSD = rule.LimitUSD
		}
		attrs := actor.Attrs
		attrs.SpendUSD = usage.SpendUSD
		attrs.LimitUSD = usage.LimitUSD
		attrs.UsedPct = usage.UsedPct
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

func lookupGateActor(actors map[string]GateActor, userID, email string) (GateActor, bool) {
	if userID != "" {
		if actor, ok := actors[userID]; ok {
			return actor, true
		}
	}
	if email != "" {
		if actor, ok := actors[conv.NormalizeEmail(email)]; ok {
			return actor, true
		}
	}
	return GateActor{
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
		UsageByRule: nil,
	}, false
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
	actual, _ := g.programs.LoadOrStore(expr, prg)
	typed, ok := actual.(cel.Program)
	if !ok {
		return nil, fmt.Errorf("cached spend gate program has unexpected type %T", actual)
	}
	return typed, nil
}

func NewGateState(actors []Actor) GateState {
	state := GateState{
		Rules:  []GateRule{},
		Actors: map[string]GateActor{},
	}
	for _, actor := range actors {
		state.SetActor(actor)
	}
	return state
}

func (s *GateState) SetActor(actor Actor) {
	gateActor := GateActor{
		UserID:      actor.UserID,
		Email:       actor.Email,
		DisplayName: actor.DisplayName,
		Attrs:       actor.Attrs,
		UsageByRule: map[string]GateUsage{},
	}
	if actor.UserID != "" {
		s.Actors[actor.UserID] = gateActor
	}
	if actor.Email != "" {
		s.Actors[conv.NormalizeEmail(actor.Email)] = gateActor
	}
}

func (s *GateState) SetUsage(ruleURN string, usage ActorUsage) {
	gateUsage := GateUsage{
		SpendUSD: usage.SpendUSD,
		LimitUSD: usage.LimitUSD,
		UsedPct:  usage.UsedPct,
	}
	if usage.Actor.UserID != "" {
		s.setUsageForIdentifier(usage.Actor.UserID, usage.Actor, ruleURN, gateUsage)
	}
	if usage.Actor.Email != "" {
		s.setUsageForIdentifier(conv.NormalizeEmail(usage.Actor.Email), usage.Actor, ruleURN, gateUsage)
	}
}

func (s *GateState) setUsageForIdentifier(identifier string, actor Actor, ruleURN string, usage GateUsage) {
	gateActor, ok := s.Actors[identifier]
	if !ok {
		gateActor = GateActor{
			UserID:      actor.UserID,
			Email:       actor.Email,
			DisplayName: actor.DisplayName,
			Attrs:       actor.Attrs,
			UsageByRule: map[string]GateUsage{},
		}
	}
	if gateActor.UsageByRule == nil {
		gateActor.UsageByRule = map[string]GateUsage{}
	}
	gateActor.UsageByRule[ruleURN] = usage
	s.Actors[identifier] = gateActor
}

// WriteGateState replaces the organization's request-path state. An empty set
// deletes the key so the gate's common case stays a cheap miss.
func WriteGateState(ctx context.Context, cacheImpl cache.Cache, organizationID string, state GateState) error {
	key := spendGateSnapshotKey(organizationID)
	if len(state.Rules) == 0 {
		if err := cacheImpl.Delete(ctx, key); err != nil && !errors.Is(err, redisCache.ErrCacheMiss) {
			return fmt.Errorf("clear spend gate state: %w", err)
		}
		return nil
	}
	if err := cacheImpl.Set(ctx, key, state, spendGateSnapshotTTL); err != nil {
		return fmt.Errorf("write spend gate state: %w", err)
	}
	return nil
}
