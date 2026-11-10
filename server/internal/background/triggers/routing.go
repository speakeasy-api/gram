package triggers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	triggerrepo "github.com/speakeasy-api/gram/server/internal/triggers/repo"
)

// RoutingPolicy selects which conversation events reach a trigger target
// when no thread route decides them.
type RoutingPolicy string

const (
	// RoutingPolicyMentions delivers events that address the target. The
	// target does not follow the thread afterwards.
	RoutingPolicyMentions RoutingPolicy = "mentions"

	// RoutingPolicyThreads delivers events that address the target and
	// subscribes it to their thread, then delivers every event on threads it
	// is subscribed to.
	RoutingPolicyThreads RoutingPolicy = "threads"

	// RoutingPolicyChannels delivers every event that passes the trigger
	// filter.
	RoutingPolicyChannels RoutingPolicy = "channels"
)

// ThreadRouteState is whether a trigger target receives events from one
// conversation.
type ThreadRouteState string

const (
	ThreadRouteSubscribed   ThreadRouteState = "subscribed"
	ThreadRouteUnsubscribed ThreadRouteState = "unsubscribed"
)

// RoutingFacts are the source-specific properties of an event that thread
// routing reads.
type RoutingFacts struct {
	// Addressed reports that the event is explicitly directed at the target,
	// such as a mention, a direct message, or a click on the target's button.
	Addressed bool

	// SelfAuthored reports that the target produced the event itself.
	SelfAuthored bool

	// InThread reports that the event is a reply inside a conversation that
	// has earlier messages.
	InThread bool

	// Cursor is the event's position in its conversation. Cursors of one
	// conversation must sort lexically in delivery order.
	Cursor string

	// Conversation reports that the event is a conversation message, mention,
	// or button click. Other events (reactions, joins) skip the reply rules and
	// are delivered whenever they pass the trigger filter.
	Conversation bool

	// DedupKey identifies the underlying message when the source delivers it
	// as more than one event (Slack sends both app_mention and message for a
	// mention). Empty uses the event id.
	DedupKey string
}

// RoutedConfig is a trigger config whose events belong to conversations that
// support thread routing.
type RoutedConfig interface {
	Config

	RoutingPolicy() RoutingPolicy

	// RoutingFacts returns false for events that do not belong to a
	// conversation.
	RoutingFacts(event any) (RoutingFacts, bool)
}

// addressedEventKey is the event JSON key set to true on events directed at
// the target.
const addressedEventKey = "addressed"

// setEventJSONField sets one top-level field of an event JSON object.
func setEventJSONField(eventJSON []byte, key string, value any) ([]byte, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(eventJSON, &fields); err != nil {
		return nil, fmt.Errorf("decode event fields: %w", err)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode event field %s: %w", key, err)
	}
	fields[key] = encoded
	out, err := json.Marshal(fields)
	if err != nil {
		return nil, fmt.Errorf("encode event: %w", err)
	}
	return out, nil
}

// ThreadBackfill asks the dispatcher to add conversation messages the target
// has not seen to the event before delivery.
type ThreadBackfill struct {
	// After is the cursor of the newest message the target has seen. Empty
	// reads the conversation from its start.
	After string
}

type routeAction string

const (
	routeDispatch routeAction = "dispatch"
	routeSkip     routeAction = "skip"
)

type routeInput struct {
	policy RoutingPolicy
	facts  RoutingFacts

	// route is nil when no thread route exists for the conversation.
	route *triggerrepo.TriggerThreadRoute
}

type routeDecision struct {
	action routeAction
	reason string

	// record, when set, is the thread route state to store for the
	// conversation, advancing its cursor to the event.
	record *ThreadRouteState

	// advance moves the conversation's cursor to the event without changing
	// its state.
	advance bool

	// backfill is set when the target may have missed earlier messages.
	backfill *ThreadBackfill
}

// undecided is what a rule returns alongside false when it defers.
var undecided = routeDecision{action: "", reason: "", record: nil, advance: false, backfill: nil}

// routeRule decides an event or returns false to defer to the next rule.
type routeRule func(routeInput) (routeDecision, bool)

// routeRules run in order; the first rule that decides wins. The last rule
// always decides.
var routeRules = []routeRule{
	skipSelfAuthored,
	dispatchOutsideConversation,
	dispatchAddressed,
	skipUnsubscribed,
	dispatchSubscribed,
	applyRoutingPolicy,
}

func decideRoute(in routeInput) routeDecision {
	for _, rule := range routeRules {
		if decision, ok := rule(in); ok {
			return decision
		}
	}
	return routeDecision{action: routeSkip, reason: "no routing rule matched", record: nil, advance: false, backfill: nil}
}

func skipSelfAuthored(in routeInput) (routeDecision, bool) {
	if !in.facts.SelfAuthored {
		return undecided, false
	}
	return routeDecision{action: routeSkip, reason: "event authored by trigger target", record: nil, advance: false, backfill: nil}, true
}

func dispatchOutsideConversation(in routeInput) (routeDecision, bool) {
	if in.facts.Conversation {
		return undecided, false
	}
	return routeDecision{action: routeDispatch, reason: "event is not a conversation message", record: nil, advance: false, backfill: nil}, true
}

// dispatchAddressed delivers an event directed at the target.
//
// Under the threads policy, and for a conversation the target left, it
// subscribes the target to the conversation. Under the mentions policy it
// records the conversation without subscribing, so the next mention only
// backfills what arrived since. Under the channels policy an untracked
// conversation stays untracked, since every event on it is delivered anyway.
func dispatchAddressed(in routeInput) (routeDecision, bool) {
	if !in.facts.Addressed {
		return undecided, false
	}
	decision := routeDecision{action: routeDispatch, reason: "event addresses trigger target", record: nil, advance: false, backfill: nil}

	subscribed, unsubscribed := ThreadRouteSubscribed, ThreadRouteUnsubscribed
	switch {
	case in.route != nil && ThreadRouteState(in.route.State) == ThreadRouteSubscribed:
		decision.record = &subscribed
	case in.policy == RoutingPolicyMentions:
		decision.record = &unsubscribed
	case in.policy == RoutingPolicyThreads || in.route != nil:
		decision.record = &subscribed
	}

	// The target missed earlier messages on a conversation it has not been
	// following: one it left, or one it has never seen outside the channels
	// policy.
	missedMessages := (in.route == nil && in.policy != RoutingPolicyChannels) ||
		(in.route != nil && ThreadRouteState(in.route.State) == ThreadRouteUnsubscribed)
	if in.facts.InThread && missedMessages {
		decision.backfill = &ThreadBackfill{After: ""}
		if in.route != nil {
			decision.backfill.After = conv.FromPGTextOrEmpty[string](in.route.LastSeenCursor)
		}
	}
	return decision, true
}

func skipUnsubscribed(in routeInput) (routeDecision, bool) {
	if in.route == nil || ThreadRouteState(in.route.State) != ThreadRouteUnsubscribed {
		return undecided, false
	}
	return routeDecision{action: routeSkip, reason: "trigger target not following conversation", record: nil, advance: false, backfill: nil}, true
}

func dispatchSubscribed(in routeInput) (routeDecision, bool) {
	if in.route == nil || ThreadRouteState(in.route.State) != ThreadRouteSubscribed {
		return undecided, false
	}
	return routeDecision{action: routeDispatch, reason: "trigger target subscribed to conversation", record: nil, advance: true, backfill: nil}, true
}

func applyRoutingPolicy(in routeInput) (routeDecision, bool) {
	if in.policy == RoutingPolicyChannels {
		return routeDecision{action: routeDispatch, reason: "routing policy delivers all events", record: nil, advance: false, backfill: nil}, true
	}
	return routeDecision{action: routeSkip, reason: "trigger target not addressed or subscribed", record: nil, advance: false, backfill: nil}, true
}

// routedEvent is the outcome of routing one event that is delivered.
type routedEvent struct {
	eventID       string
	correlationID string
	backfill      *ThreadBackfill

	// addressed reports that the event is directed at the target.
	addressed bool
}

// routeEvent applies thread routing to an event that passed the trigger
// filter. It returns nil with a reason when the event must not be delivered.
func (a *App) routeEvent(ctx context.Context, instance triggerrepo.TriggerInstance, config Config, envelope EventEnvelope) (*routedEvent, string, error) {
	passthrough := &routedEvent{eventID: envelope.EventID, correlationID: envelope.CorrelationID, backfill: nil, addressed: false}

	routed, ok := config.(RoutedConfig)
	if !ok {
		return passthrough, "", nil
	}
	facts, ok := routed.RoutingFacts(envelope.Event)
	if !ok {
		return passthrough, "", nil
	}
	policy := routed.RoutingPolicy()

	correlationID := boundAssistantKey(envelope.CorrelationID)
	var route *triggerrepo.TriggerThreadRoute
	row, err := a.repo.GetTriggerThreadRoute(ctx, triggerrepo.GetTriggerThreadRouteParams{
		ProjectID:     instance.ProjectID,
		TargetKind:    instance.TargetKind,
		TargetRef:     instance.TargetRef,
		CorrelationID: correlationID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return nil, "", fmt.Errorf("get thread route: %w", err)
	default:
		route = &row
	}

	decision := decideRoute(routeInput{policy: policy, facts: facts, route: route})
	if decision.action == routeSkip {
		return nil, decision.reason, nil
	}

	if decision.record != nil {
		row, err := a.repo.UpsertTriggerThreadRouteState(ctx, triggerrepo.UpsertTriggerThreadRouteStateParams{
			ProjectID:      instance.ProjectID,
			TargetKind:     instance.TargetKind,
			TargetRef:      instance.TargetRef,
			CorrelationID:  correlationID,
			State:          string(*decision.record),
			LastSeenCursor: conv.ToPGTextEmpty(facts.Cursor),
		})
		if err != nil {
			return nil, "", fmt.Errorf("record thread route: %w", err)
		}
		route = &row
	}
	if decision.advance && facts.Cursor != "" {
		if err := a.repo.AdvanceTriggerThreadRouteCursor(ctx, triggerrepo.AdvanceTriggerThreadRouteCursorParams{
			LastSeenCursor: facts.Cursor,
			ProjectID:      instance.ProjectID,
			TargetKind:     instance.TargetKind,
			TargetRef:      instance.TargetRef,
			CorrelationID:  correlationID,
		}); err != nil {
			return nil, "", fmt.Errorf("advance thread route cursor: %w", err)
		}
	}

	out := &routedEvent{eventID: envelope.EventID, correlationID: envelope.CorrelationID, backfill: decision.backfill, addressed: facts.Addressed}
	if route != nil && route.RouteToCorrelationID.Valid {
		out.correlationID = route.RouteToCorrelationID.String
	}
	if facts.DedupKey != "" {
		out.eventID = scopeWebhookEventID(instance.ID.String(), facts.DedupKey, nil)
	}
	return out, "", nil
}

// AssistantThread identifies one conversation of an assistant trigger target.
type AssistantThread struct {
	ProjectID   uuid.UUID
	AssistantID uuid.UUID

	// ThreadID is the assistant thread. Assistant tokens that omit it leave it
	// uuid.Nil, and ChatID identifies the thread instead.
	ThreadID uuid.UUID

	// ChatID is the thread's chat id as carried on a tool call, used when
	// ThreadID is uuid.Nil.
	ChatID string
}

func (a *App) assistantThreadCorrelation(ctx context.Context, thread AssistantThread) (string, error) {
	queries := assistantrepo.New(a.db)
	if thread.ThreadID == uuid.Nil {
		chatID, err := uuid.Parse(thread.ChatID)
		if err != nil {
			return "", fmt.Errorf("resolve thread correlation: no thread id and invalid chat id: %w", err)
		}
		row, err := queries.ResolveThreadCorrelationByChat(ctx, assistantrepo.ResolveThreadCorrelationByChatParams{
			ChatID:      chatID,
			AssistantID: thread.AssistantID,
			ProjectID:   thread.ProjectID,
		})
		if err != nil {
			return "", fmt.Errorf("resolve thread correlation by chat: %w", err)
		}
		return row.CorrelationID, nil
	}

	row, err := queries.ResolveThreadCorrelation(ctx, assistantrepo.ResolveThreadCorrelationParams{
		ThreadID:  thread.ThreadID,
		ProjectID: thread.ProjectID,
	})
	if err != nil {
		return "", fmt.Errorf("resolve thread correlation: %w", err)
	}
	if row.AssistantID != thread.AssistantID {
		return "", fmt.Errorf("thread does not belong to the assistant")
	}
	return row.CorrelationID, nil
}

// SetAssistantThreadRouteState subscribes or unsubscribes an assistant from
// the conversation behind one of its threads and from every conversation
// routed to that thread.
func (a *App) SetAssistantThreadRouteState(ctx context.Context, thread AssistantThread, state ThreadRouteState) error {
	correlationID, err := a.assistantThreadCorrelation(ctx, thread)
	if err != nil {
		return err
	}

	tx, err := a.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin thread route tx: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	queries := a.repo.WithTx(tx)
	if _, err := queries.UpsertTriggerThreadRouteState(ctx, triggerrepo.UpsertTriggerThreadRouteStateParams{
		ProjectID:      thread.ProjectID,
		TargetKind:     TargetKindAssistant,
		TargetRef:      thread.AssistantID.String(),
		CorrelationID:  correlationID,
		State:          string(state),
		LastSeenCursor: pgtype.Text{String: "", Valid: false},
	}); err != nil {
		return fmt.Errorf("set thread route state: %w", err)
	}
	if _, err := queries.SetTriggerThreadRoutesStateForTarget(ctx, triggerrepo.SetTriggerThreadRoutesStateForTargetParams{
		State:         string(state),
		ProjectID:     thread.ProjectID,
		TargetKind:    TargetKindAssistant,
		TargetRef:     thread.AssistantID.String(),
		CorrelationID: correlationID,
	}); err != nil {
		return fmt.Errorf("set routed thread states: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit thread route tx: %w", err)
	}
	return nil
}

// RouteSlackThreadToAssistant delivers future events on the Slack thread
// rooted at threadTS to an assistant thread and subscribes the assistant to
// it. The assistant has seen the thread up to threadTS.
func (a *App) RouteSlackThreadToAssistant(ctx context.Context, thread AssistantThread, channelID, threadTS string) error {
	toCorrelationID, err := a.assistantThreadCorrelation(ctx, thread)
	if err != nil {
		return err
	}
	if _, err := a.repo.RouteTriggerThread(ctx, triggerrepo.RouteTriggerThreadParams{
		ProjectID:            thread.ProjectID,
		TargetKind:           TargetKindAssistant,
		TargetRef:            thread.AssistantID.String(),
		CorrelationID:        boundAssistantKey(slackCorrelationID(channelID, threadTS, "", "")),
		RouteToCorrelationID: conv.ToPGText(toCorrelationID),
		State:                string(ThreadRouteSubscribed),
		LastSeenCursor:       conv.ToPGTextEmpty(threadTS),
	}); err != nil {
		return fmt.Errorf("route slack thread: %w", err)
	}
	return nil
}
