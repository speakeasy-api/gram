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
	"github.com/speakeasy-api/gram/server/internal/spendrules/chrepo"
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
	WindowKind string    `json:"window_kind"`
	WindowEnd  time.Time `json:"window_end"`
}

type GateActor struct {
	UserID      string                     `json:"user_id"`
	Email       string                     `json:"email"`
	DisplayName string                     `json:"display_name"`
	Attrs       celenv.Actor               `json:"attrs"`
	Spend       chrepo.ActorWindowSpendRow `json:"spend"`
}

// GateState is the full request-path state for one organization. Actors are
// keyed by "{org_id}:{user_email}" with normalized email so a hook event can
// resolve its actor without scanning the organization.
type GateState struct {
	Rules  []GateRule           `json:"rules"`
	Actors map[string]GateActor `json:"actors"`
}

func spendGateSnapshotKey(organizationID string) string {
	return "spend_gate:" + organizationID
}

func spendGateActorKey(organizationID, email string) string {
	if organizationID == "" || email == "" {
		return ""
	}
	return organizationID + ":" + conv.NormalizeEmail(email)
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

// CheckBlocked reports whether the given actor is currently blocked by a spend
// rule. The actor is looked up only by "{org_id}:{user_email}". A nil Block
// means the actor is not blocked. Errors are cache infrastructure failures —
// callers should treat them as "not blocked" (fail-open); they are returned for
// logging.
func (g *Gate) CheckBlocked(ctx context.Context, organizationID, email string) (*Block, error) {
	actorKey := spendGateActorKey(organizationID, email)
	if actorKey == "" || g.celEng == nil {
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

	actor, ok := state.Actors[actorKey]
	if !ok {
		return nil, nil
	}

	now := time.Now().UTC()
	for _, rule := range state.Rules {
		if rule.Action != ActionBlock {
			continue
		}

		// The snapshot holds the spend for the window that was current when the
		// evaluator last wrote it. Once that window has ended, the block must
		// lift immediately (spend resets to zero for the new window) rather than
		// keep denying on stale spend until the next evaluation cycle rewrites
		// the snapshot.
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
	actual, _ := g.programs.LoadOrStore(expr, prg)
	typed, ok := actual.(cel.Program)
	if !ok {
		return nil, fmt.Errorf("cached spend gate program has unexpected type %T", actual)
	}
	return typed, nil
}

func NewGateState(organizationID string, actors []Actor) GateState {
	state := GateState{
		Rules:  []GateRule{},
		Actors: map[string]GateActor{},
	}
	for _, actor := range actors {
		state.SetActor(organizationID, actor)
	}
	return state
}

func (s *GateState) SetActor(organizationID string, actor Actor) {
	actorKey := spendGateActorKey(organizationID, actor.Email)
	if actorKey == "" {
		return
	}

	gateActor := GateActor{
		UserID:      actor.UserID,
		Email:       actor.Email,
		DisplayName: actor.DisplayName,
		Attrs:       actor.Attrs,
		Spend: chrepo.ActorWindowSpendRow{
			Email:       "",
			DailyCost:   0,
			WeeklyCost:  0,
			MonthlyCost: 0,
		},
	}
	s.Actors[actorKey] = gateActor
}

func (s *GateState) SetActorWindowSpend(organizationID string, actor Actor, spend chrepo.ActorWindowSpendRow) {
	actorKey := spendGateActorKey(organizationID, actor.Email)
	if actorKey == "" {
		return
	}
	gateActor, ok := s.Actors[actorKey]
	if !ok {
		gateActor = GateActor{
			UserID:      actor.UserID,
			Email:       actor.Email,
			DisplayName: actor.DisplayName,
			Attrs:       actor.Attrs,
			Spend: chrepo.ActorWindowSpendRow{
				Email:       "",
				DailyCost:   0,
				WeeklyCost:  0,
				MonthlyCost: 0,
			},
		}
	}
	gateActor.Spend = spend
	s.Actors[actorKey] = gateActor
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
