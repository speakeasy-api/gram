package enforcereply

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/metric"
	"golang.org/x/sync/errgroup"
	"google.golang.org/protobuf/proto"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/scanners"
	"github.com/speakeasy-api/gram/server/internal/stokens"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/redisinbox"
	"github.com/speakeasy-api/gram/server/internal/requestreply"
)

const (
	// DefaultWaitTimeout prevents deadline-free callers from retaining waiters.
	DefaultWaitTimeout = 30 * time.Second

	// DefaultLLMAnalyzerWaitTimeout is the LLM analyzer lane's wait budget: the
	// consumer's 15 s model timeout plus slack for transport retries. It stays
	// below the LLMEnforcer subscription's 30 s ack deadline.
	DefaultLLMAnalyzerWaitTimeout = 20 * time.Second

	// DefaultPromptInjectionWaitTimeout covers the cascade's 55-second model
	// budget and stays below the subscription's 90-second ack deadline.
	DefaultPromptInjectionWaitTimeout = 60 * time.Second

	// DefaultPromptPolicyWaitTimeout covers the judge's 10-second model budget
	// and stays below the subscription's 30-second ack deadline.
	DefaultPromptPolicyWaitTimeout = 15 * time.Second

	// DefaultCustomRulesWaitTimeout bounds deterministic evaluation below the
	// subscription's 10-second ack deadline.
	DefaultCustomRulesWaitTimeout = 5 * time.Second

	// DefaultMaxContentBytes is the combined budget for distinct enforcement
	// payload values when callers do not provide one.
	DefaultMaxContentBytes = 1 * 1024 * 1024

	// MaxContentBytes is the hard ceiling for distinct enforcement payload
	// values. Equal Content, Body, and tool-call arguments consume the budget
	// once so every lane receives the same prefix.
	MaxContentBytes               = 1 * 1024 * 1024
	presidioEngineMaxContentBytes = 50 * 1024

	// contentLimitDistinctID keys the global content limit flag; the flag
	// carries no targeting so any stable id resolves the same payload.
	contentLimitDistinctID = "gram-server"
)

// EnforcementLane is the non-generic request seam used by enforcement fan-out.
type EnforcementLane interface {
	Request(ctx context.Context, req proto.Message) (*riskv1.EnforcementReply, error)
}

type typedEnforcementLane[Req proto.Message] struct {
	broker requestreply.RequestBroker[Req, *riskv1.EnforcementReply]
}

func (l *typedEnforcementLane[Req]) Request(ctx context.Context, req proto.Message) (*riskv1.EnforcementReply, error) {
	typed, ok := req.(Req)
	if !ok {
		return nil, fmt.Errorf("unexpected enforcement request type %T", req)
	}
	reply, err := l.broker.Request(ctx, typed)
	if err != nil {
		return nil, fmt.Errorf("request typed enforcement lane: %w", err)
	}
	return reply, nil
}

type publisherStopper interface {
	Stop(context.Context) error
}

func stopPublishers(ctx context.Context, publishers []publisherStopper) {
	stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	for _, publisher := range publishers {
		_ = publisher.Stop(stopCtx)
	}
}

type namedCloser struct {
	name  string
	close func(context.Context) error
}

func closeRequestBrokers(ctx context.Context, closers ...namedCloser) error {
	var closeErrs []error
	var mu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(len(closers))
	for _, closer := range closers {
		go func() {
			defer wg.Done()
			if err := closer.close(ctx); err != nil {
				mu.Lock()
				closeErrs = append(closeErrs, fmt.Errorf("close %s enforcement lane: %w", closer.name, err))
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return errors.Join(closeErrs...)
}

// DispatcherConfig controls bounded request publication and reply waiting.
type DispatcherConfig struct {
	// WaitTimeout caps each lane's publication and reply wait unless
	// LaneWaitTimeout overrides it for that lane's scanner.
	WaitTimeout time.Duration

	// LaneWaitTimeout overrides WaitTimeout per scanner. Scanners without an
	// entry, or with a non-positive entry, use WaitTimeout.
	LaneWaitTimeout map[riskv1.EnforcementScanner]time.Duration

	// Flags resolves the global content limit override. Nil applies
	// DefaultMaxContentBytes.
	Flags feature.Provider
}

// ToolCall is one tool invocation carried to enforcement judge lanes.
type ToolCall struct {
	// ID is the harness-assigned tool call id, or a synthetic one when the
	// harness supplied none.
	ID string

	// Name is the tool the agent invoked.
	Name string

	// Arguments is the serialized tool input, typically JSON.
	Arguments string
}

// PromptPolicyConfig is the policy-owned input for one keyed judge lane.
type PromptPolicyConfig struct {
	// Prompt is the operator-authored policy instruction.
	Prompt string

	// ModelConfig carries the policy's judge options, including fail_open.
	ModelConfig []byte
}

// DispatchRequest contains tenant context, content, and requested lanes.
type DispatchRequest struct {
	// OrganizationID is the tenant used for fingerprint isolation.
	OrganizationID string

	// OrganizationSlug is carried by the LLM lane for telemetry dimensions only.
	OrganizationSlug string

	// ProjectID identifies the project whose policy configuration applies.
	ProjectID string

	// Content is raw scanner input and consumes the shared publish budget first.
	Content string

	// Body is judge prompt input and consumes the shared publish budget second,
	// unless it duplicates an earlier field.
	Body string

	// ToolName is the tool the LLM lane attributes the message to. When empty,
	// the lane falls back to the origin's ToolName.
	ToolName string

	// MessageType is the message kind the LLM lane evaluates. When empty, the
	// lane falls back to the origin's MessageType.
	MessageType string

	// Tool-call arguments consume the remaining shared publish budget in order.
	// Arguments that duplicate an earlier field reuse that field's budgeted value.
	ToolCalls []ToolCall

	// PresidioEntities is an optional scanner superset; empty requests all entities.
	PresidioEntities []string

	// PresidioScoreThreshold is an optional scanner minimum.
	PresidioScoreThreshold *float64

	// CustomRuleIDs is the distinct rule set evaluated by the custom-rules lane.
	CustomRuleIDs []string

	// PromptPolicies carries one configuration per policy-keyed judge lane.
	PromptPolicies map[string]PromptPolicyConfig

	// Lanes is the distinct set of scanner and policy results required.
	Lanes []Lane

	// Origins carries immutable per-lane attribution. Lane.PolicyID remains
	// reserved for reply correlation and is not policy provenance.
	Origins map[Lane]metering.RiskProvenance
}

// Dispatcher fans enforcement work out over independent request brokers.
type Dispatcher struct {
	gitleaks        EnforcementLane
	presidio        EnforcementLane
	promptInjection EnforcementLane
	customRules     EnforcementLane
	judge           EnforcementLane
	llm             EnforcementLane
	close           func(context.Context) error
	waitTimeout     time.Duration
	flags           feature.Provider
	// laneWaitTimeout overrides waitTimeout for the scanners it names.
	laneWaitTimeout map[riskv1.EnforcementScanner]time.Duration
	logger          *slog.Logger
	truncations     metric.Int64Counter
	// stokenCodec counts prepared Presidio input before dispatch.
	stokenCodec *stokens.Codec
}

// NewDispatcher resolves request brokers for the supported enforcement lanes.
func NewDispatcher(ctx context.Context, logger *slog.Logger, meterProvider metric.MeterProvider, broker gcp.PublisherBroker, inbox *Inbox, cfg DispatcherConfig) (*Dispatcher, error) {
	if inbox == nil {
		return nil, errors.New("enforcement reply inbox is required")
	}
	if cfg.WaitTimeout <= 0 {
		cfg.WaitTimeout = DefaultWaitTimeout
	}

	var publishers []publisherStopper
	gitleaksPub, err := gcp.PubSubPublisherForMessage(ctx, broker, &riskv1.GitleaksEnforcement{})
	if err != nil {
		return nil, fmt.Errorf("create gitleaks enforcement publisher: %w", err)
	}
	publishers = append(publishers, gitleaksPub)
	presidioPub, err := gcp.PubSubPublisherForMessage(ctx, broker, &riskv1.PresidioEnforcement{})
	if err != nil {
		stopPublishers(ctx, publishers)
		return nil, fmt.Errorf("create presidio enforcement publisher: %w", err)
	}
	publishers = append(publishers, presidioPub)
	promptInjectionPub, err := gcp.PubSubPublisherForMessage(ctx, broker, &riskv1.PromptInjectionEnforcement{})
	if err != nil {
		stopPublishers(ctx, publishers)
		return nil, fmt.Errorf("create prompt injection enforcement publisher: %w", err)
	}
	publishers = append(publishers, promptInjectionPub)
	customRulesPub, err := gcp.PubSubPublisherForMessage(ctx, broker, &riskv1.CustomRulesEnforcement{})
	if err != nil {
		stopPublishers(ctx, publishers)
		return nil, fmt.Errorf("create custom rules enforcement publisher: %w", err)
	}
	publishers = append(publishers, customRulesPub)
	judgePub, err := gcp.PubSubPublisherForMessage(ctx, broker, &riskv1.PromptPolicyEnforcement{})
	if err != nil {
		stopPublishers(ctx, publishers)
		return nil, fmt.Errorf("create prompt policy enforcement publisher: %w", err)
	}
	publishers = append(publishers, judgePub)
	llmPub, err := gcp.PubSubPublisherForMessage(ctx, broker, &riskv1.LLMEnforcement{})
	if err != nil {
		stopPublishers(ctx, publishers)
		return nil, fmt.Errorf("create llm enforcement publisher: %w", err)
	}

	gitleaksReq := redisinbox.NewRequestBroker(inbox, gitleaksPub)
	presidioReq := redisinbox.NewRequestBroker(inbox, presidioPub)
	promptInjectionReq := redisinbox.NewRequestBroker(inbox, promptInjectionPub)
	customRulesReq := redisinbox.NewRequestBroker(inbox, customRulesPub)
	judgeReq := redisinbox.NewRequestBroker(inbox, judgePub)
	llmReq := redisinbox.NewRequestBroker(inbox, llmPub)
	return &Dispatcher{
		flags:           cfg.Flags,
		gitleaks:        &typedEnforcementLane[*riskv1.GitleaksEnforcement]{broker: gitleaksReq},
		presidio:        &typedEnforcementLane[*riskv1.PresidioEnforcement]{broker: presidioReq},
		promptInjection: &typedEnforcementLane[*riskv1.PromptInjectionEnforcement]{broker: promptInjectionReq},
		customRules:     &typedEnforcementLane[*riskv1.CustomRulesEnforcement]{broker: customRulesReq},
		judge:           &typedEnforcementLane[*riskv1.PromptPolicyEnforcement]{broker: judgeReq},
		llm:             &typedEnforcementLane[*riskv1.LLMEnforcement]{broker: llmReq},
		close: func(ctx context.Context) error {
			return closeRequestBrokers(ctx,
				namedCloser{name: "gitleaks", close: gitleaksReq.Close},
				namedCloser{name: "presidio", close: presidioReq.Close},
				namedCloser{name: "prompt injection", close: promptInjectionReq.Close},
				namedCloser{name: "custom rules", close: customRulesReq.Close},
				namedCloser{name: "prompt policy", close: judgeReq.Close},
				namedCloser{name: "llm", close: llmReq.Close},
			)
		},
		waitTimeout:     cfg.WaitTimeout,
		laneWaitTimeout: cfg.LaneWaitTimeout,
		logger:          logger,
		truncations:     newTruncationCounter(meterProvider),
		stokenCodec:     stokens.NewCodec(),
	}, nil
}

func newTruncationCounter(meterProvider metric.MeterProvider) metric.Int64Counter {
	truncations, _ := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/risk/enforcereply").Int64Counter(
		"risk.enforcement.truncations",
		metric.WithDescription("Number of enforcement requests truncated to the configured byte limit before publication"),
		metric.WithUnit("{message}"),
	)
	return truncations
}

// Dispatch fans content out to distinct lanes and folds replies by lane.
// contentLimit reads the global truncation limit from the flag payload,
// falling back to DefaultMaxContentBytes when the flag carries none.
func (d *Dispatcher) contentLimit(ctx context.Context) int {
	if d.flags == nil {
		return DefaultMaxContentBytes
	}
	payload, err := d.flags.FlagPayload(ctx, feature.FlagRiskEnforcementMaxContentBytes, contentLimitDistinctID, nil)
	if err != nil {
		d.logger.WarnContext(ctx, "resolve enforcement content limit", attr.SlogError(err))
		return DefaultMaxContentBytes
	}
	var config struct {
		MaxContentBytes int `json:"max_content_bytes"`
	}
	if payload == nil || json.Unmarshal(payload, &config) != nil || config.MaxContentBytes <= 0 {
		return DefaultMaxContentBytes
	}
	return min(config.MaxContentBytes, MaxContentBytes)
}

func (d *Dispatcher) Dispatch(ctx context.Context, request DispatchRequest) (Outcome, error) {
	if request.OrganizationID == "" {
		return Outcome{}, errors.New("enforcement organization id is required")
	}
	if request.ProjectID == "" {
		return Outcome{}, errors.New("enforcement project id is required")
	}
	if len(request.Lanes) == 0 {
		return Outcome{ByLane: map[Lane]*riskv1.EnforcementReply{}, Failed: map[Lane]error{}, Complete: true, Deadline: false, Truncated: false}, nil
	}
	limit := d.contentLimit(ctx)
	remaining := limit
	truncated := false
	truncatedValues := make(map[string]string, 2+len(request.ToolCalls))
	truncate := func(field string, value string) string {
		original := value
		if cached, ok := truncatedValues[value]; ok {
			return cached
		}
		if len(value) <= remaining {
			remaining -= len(value)
			truncatedValues[value] = value
			return value
		}
		truncated = true
		d.logger.WarnContext(ctx, "truncating oversized enforcement "+field, attr.SlogRiskScanTextSize(len(value)), attr.SlogRiskScanLimitBytes(remaining))
		value = truncateAtRuneBoundary(value, remaining)
		remaining -= len(value)
		truncatedValues[original] = value
		return value
	}
	request.Content = truncate("content", request.Content)
	request.Body = truncate("body", request.Body)
	// Copy before truncating so the caller's tool calls stay intact.
	request.ToolCalls = slices.Clone(request.ToolCalls)
	for i := range request.ToolCalls {
		request.ToolCalls[i].Arguments = truncate("tool call arguments", request.ToolCalls[i].Arguments)
	}
	if truncated && d.truncations != nil {
		d.truncations.Add(ctx, 1)
	}
	seen := make(map[Lane]struct{}, len(request.Lanes))
	for _, lane := range request.Lanes {
		if _, duplicate := seen[lane]; duplicate {
			return Outcome{}, fmt.Errorf("duplicate enforcement lane %s", lane.String())
		}
		seen[lane] = struct{}{}
		if _, ok := request.Origins[lane]; !ok {
			return Outcome{}, fmt.Errorf("missing enforcement origin for lane %s", lane.String())
		}
		switch lane.Scanner {
		case riskv1.EnforcementScanner_ENFORCEMENT_SCANNER_JUDGE:
			if lane.PolicyID == "" {
				return Outcome{}, fmt.Errorf("unsupported enforcement lane %s", lane.String())
			}
			if _, ok := request.PromptPolicies[lane.PolicyID]; !ok {
				return Outcome{}, fmt.Errorf("missing prompt policy config for lane %s", lane.String())
			}
		case riskv1.EnforcementScanner_ENFORCEMENT_SCANNER_GITLEAKS,
			riskv1.EnforcementScanner_ENFORCEMENT_SCANNER_PRESIDIO,
			riskv1.EnforcementScanner_ENFORCEMENT_SCANNER_PROMPT_INJECTION,
			riskv1.EnforcementScanner_ENFORCEMENT_SCANNER_CUSTOM_RULES,
			riskv1.EnforcementScanner_ENFORCEMENT_SCANNER_LLM_ANALYZER:
			if lane.PolicyID != "" {
				return Outcome{}, fmt.Errorf("unsupported enforcement lane %s", lane.String())
			}
		default:
			return Outcome{}, fmt.Errorf("unsupported enforcement lane %s", lane.String())
		}
	}

	requestID := request.Origins[request.Lanes[0]].OperationID
	if strings.TrimSpace(requestID) == "" {
		return Outcome{}, errors.New("enforcement operation id is required")
	}
	createdAt := time.Now().UTC()
	createdAtText := createdAt.Format(time.RFC3339Nano)
	byLane := make(map[Lane]*riskv1.EnforcementReply, len(request.Lanes))
	failed := make(map[Lane]error, len(request.Lanes))
	deadline := false
	var mu sync.Mutex
	var handlerTruncated atomic.Bool
	group, groupCtx := errgroup.WithContext(ctx)
	for _, lane := range request.Lanes {
		group.Go(func() error {
			waitTimeout := d.waitTimeout
			if override := d.laneWaitTimeout[lane.Scanner]; override > 0 {
				waitTimeout = override
			}
			laneCtx, cancel := context.WithTimeout(groupCtx, waitTimeout)
			defer cancel()
			var laneBroker EnforcementLane
			var enforcement proto.Message
			switch lane.Scanner {
			case riskv1.EnforcementScanner_ENFORCEMENT_SCANNER_PROMPT_INJECTION:
				laneBroker = d.promptInjection
				enforcement = promptInjectionEnforcement(request, request.Origins[lane], requestID, createdAtText, truncated)
			case riskv1.EnforcementScanner_ENFORCEMENT_SCANNER_CUSTOM_RULES:
				laneBroker = d.customRules
				enforcement = customRulesEnforcement(request, request.Origins[lane], requestID, createdAtText, truncated)
			case riskv1.EnforcementScanner_ENFORCEMENT_SCANNER_JUDGE:
				laneBroker = d.judge
				enforcement = promptPolicyEnforcement(request, request.Origins[lane], lane.PolicyID, requestID, createdAtText, truncated)
			case riskv1.EnforcementScanner_ENFORCEMENT_SCANNER_LLM_ANALYZER:
				laneBroker = d.llm
				origin := request.Origins[lane]
				toolCalls := make([]*riskv1.LLMEnforcement_ToolCall, 0, len(request.ToolCalls))
				for _, call := range request.ToolCalls {
					toolCalls = append(toolCalls, riskv1.LLMEnforcement_ToolCall_builder{
						Id:        new(call.ID),
						Name:      new(call.Name),
						Arguments: new(call.Arguments),
					}.Build())
				}
				enforcement = riskv1.LLMEnforcement_builder{
					RequestId:               new(requestID),
					ChatMessageId:           stringPointer(origin.ChatMessageID),
					ProjectId:               new(request.ProjectID),
					OrganizationId:          new(request.OrganizationID),
					OrganizationSlug:        new(request.OrganizationSlug),
					CreatedAt:               new(createdAtText),
					Content:                 new(request.Content),
					Body:                    new(request.Body),
					ToolCalls:               toolCalls,
					ContentPartId:           stringPointer(origin.ContentPartID),
					ChatId:                  stringPointer(origin.ChatID),
					ExternalConversationId:  new(origin.ExternalConversationID),
					OriginRiskPolicyId:      stringPointer(origin.RiskPolicyID),
					OriginRiskPolicyVersion: new(origin.RiskPolicyVersion),
					MessageLinkReason:       new(origin.MessageLinkReason),
					PolicyLinkReason:        new(origin.PolicyLinkReason),
					ExecutionPath:           new(origin.ExecutionPath),
					ToolCallId:              new(origin.ToolCallID),
					ToolName:                new(conv.Default(request.ToolName, origin.ToolName)),
					HookSource:              new(origin.HookSource),
					UserId:                  new(origin.UserID),
					MessageType:             new(conv.Default(request.MessageType, origin.MessageType)),
					ContentTruncated:        new(truncated),
				}.Build()
			case riskv1.EnforcementScanner_ENFORCEMENT_SCANNER_PRESIDIO:
				laneBroker = d.presidio
				origin := request.Origins[lane]
				origin.RequestID = requestID
				origin.OperationID = scanners.AsyncRiskOperationID(
					origin.ExecutionPath, origin.RiskPolicyID.String(), origin.RiskPolicyVersion,
					"", "", origin.OperationID,
				)
				var meterReading []byte
				stokenCount, countErr := d.stokenCodec.Count(laneCtx, truncateAtRuneBoundary(request.Content, presidioEngineMaxContentBytes))
				if countErr == nil {
					reading, prepareErr := metering.PrepareRiskReading(metering.RiskPresidio(), origin, int64(stokenCount), createdAt)
					if prepareErr == nil && reading != nil {
						meterReading, _ = proto.Marshal(reading)
					}
				}
				enforcement = riskv1.PresidioEnforcement_builder{
					RequestId:               new(requestID),
					ChatMessageId:           stringPointer(origin.ChatMessageID),
					ProjectId:               new(request.ProjectID),
					OrganizationId:          new(request.OrganizationID),
					CreatedAt:               new(createdAtText),
					Content:                 new(request.Content),
					ContentPartId:           stringPointer(origin.ContentPartID),
					Entities:                request.PresidioEntities,
					ScoreThreshold:          request.PresidioScoreThreshold,
					ChatId:                  stringPointer(origin.ChatID),
					ExternalConversationId:  new(origin.ExternalConversationID),
					OriginRiskPolicyId:      stringPointer(origin.RiskPolicyID),
					OriginRiskPolicyVersion: new(origin.RiskPolicyVersion),
					MessageLinkReason:       new(origin.MessageLinkReason),
					PolicyLinkReason:        new(origin.PolicyLinkReason),
					ExecutionPath:           new(origin.ExecutionPath),
					ToolCallId:              new(origin.ToolCallID),
					ToolName:                new(origin.ToolName),
					HookSource:              new(origin.HookSource),
					UserId:                  new(origin.UserID),
					MessageType:             new(origin.MessageType),
					MeterReading:            meterReading,
					ContentTruncated:        new(truncated),
				}.Build()
			default:
				laneBroker = d.gitleaks
				origin := request.Origins[lane]
				enforcement = riskv1.GitleaksEnforcement_builder{
					RequestId:               new(requestID),
					ChatMessageId:           stringPointer(origin.ChatMessageID),
					ProjectId:               new(request.ProjectID),
					OrganizationId:          new(request.OrganizationID),
					CreatedAt:               new(createdAtText),
					Content:                 new(request.Content),
					ContentPartId:           stringPointer(origin.ContentPartID),
					ChatId:                  stringPointer(origin.ChatID),
					ExternalConversationId:  new(origin.ExternalConversationID),
					OriginRiskPolicyId:      stringPointer(origin.RiskPolicyID),
					OriginRiskPolicyVersion: new(origin.RiskPolicyVersion),
					MessageLinkReason:       new(origin.MessageLinkReason),
					PolicyLinkReason:        new(origin.PolicyLinkReason),
					ExecutionPath:           new(origin.ExecutionPath),
					ToolCallId:              new(origin.ToolCallID),
					ToolName:                new(origin.ToolName),
					HookSource:              new(origin.HookSource),
					UserId:                  new(origin.UserID),
					MessageType:             new(origin.MessageType),
					ContentTruncated:        new(truncated),
				}.Build()
			}
			reply, requestErr := laneBroker.Request(laneCtx, enforcement)
			if requestErr != nil {
				mu.Lock()
				failed[lane] = requestErr
				deadline = deadline || errors.Is(requestErr, context.DeadlineExceeded)
				mu.Unlock()
				return nil
			}
			mu.Lock()
			byLane[lane] = reply
			if reply.GetContentTruncated() {
				handlerTruncated.Store(true)
			}
			mu.Unlock()
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return Outcome{}, fmt.Errorf("dispatch enforcement lanes: %w", err)
	}
	return Outcome{ByLane: byLane, Failed: failed, Complete: len(byLane) == len(request.Lanes), Deadline: deadline, Truncated: truncated || handlerTruncated.Load()}, nil
}

func promptInjectionEnforcement(request DispatchRequest, origin metering.RiskProvenance, requestID, createdAt string, truncated bool) *riskv1.PromptInjectionEnforcement {
	toolCalls := make([]*riskv1.PromptInjectionEnforcement_ToolCall, 0, len(request.ToolCalls))
	for _, call := range request.ToolCalls {
		toolCalls = append(toolCalls, riskv1.PromptInjectionEnforcement_ToolCall_builder{
			Id: new(call.ID), Name: new(call.Name), Arguments: new(call.Arguments),
		}.Build())
	}
	return riskv1.PromptInjectionEnforcement_builder{
		RequestId:               new(requestID),
		ChatMessageId:           stringPointer(origin.ChatMessageID),
		ProjectId:               new(request.ProjectID),
		OrganizationId:          new(request.OrganizationID),
		RiskPolicyId:            stringPointer(origin.RiskPolicyID),
		RiskPolicyVersion:       new(origin.RiskPolicyVersion),
		CreatedAt:               new(createdAt),
		Content:                 new(request.Content),
		ContentPartId:           stringPointer(origin.ContentPartID),
		ChatId:                  stringPointer(origin.ChatID),
		OriginRiskPolicyId:      stringPointer(origin.RiskPolicyID),
		OriginRiskPolicyVersion: new(origin.RiskPolicyVersion),
		MessageLinkReason:       new(origin.MessageLinkReason),
		ExecutionPath:           new(origin.ExecutionPath),
		ToolCallId:              new(origin.ToolCallID),
		ToolName:                new(conv.Default(request.ToolName, origin.ToolName)),
		HookSource:              new(origin.HookSource),
		UserId:                  new(origin.UserID),
		MessageType:             new(conv.Default(request.MessageType, origin.MessageType)),
		PolicyLinkReason:        new(origin.PolicyLinkReason),
		ExternalConversationId:  new(origin.ExternalConversationID),
		ContentTruncated:        new(truncated),
		Body:                    new(request.Body),
		ToolCalls:               toolCalls,
	}.Build()
}

func customRulesEnforcement(request DispatchRequest, origin metering.RiskProvenance, requestID, createdAt string, truncated bool) *riskv1.CustomRulesEnforcement {
	toolCalls := make([]*riskv1.CustomRulesEnforcement_ToolCall, 0, len(request.ToolCalls))
	for _, call := range request.ToolCalls {
		toolCalls = append(toolCalls, riskv1.CustomRulesEnforcement_ToolCall_builder{
			Id: new(call.ID), Name: new(call.Name), Arguments: new(call.Arguments),
		}.Build())
	}
	return riskv1.CustomRulesEnforcement_builder{
		RequestId:               new(requestID),
		ChatMessageId:           stringPointer(origin.ChatMessageID),
		ProjectId:               new(request.ProjectID),
		OrganizationId:          new(request.OrganizationID),
		RiskPolicyId:            stringPointer(origin.RiskPolicyID),
		RiskPolicyVersion:       new(origin.RiskPolicyVersion),
		CreatedAt:               new(createdAt),
		Content:                 new(request.Content),
		ContentPartId:           stringPointer(origin.ContentPartID),
		ChatId:                  stringPointer(origin.ChatID),
		OriginRiskPolicyId:      stringPointer(origin.RiskPolicyID),
		OriginRiskPolicyVersion: new(origin.RiskPolicyVersion),
		MessageLinkReason:       new(origin.MessageLinkReason),
		ExecutionPath:           new(origin.ExecutionPath),
		ToolCallId:              new(origin.ToolCallID),
		ToolName:                new(conv.Default(request.ToolName, origin.ToolName)),
		HookSource:              new(origin.HookSource),
		UserId:                  new(origin.UserID),
		MessageType:             new(conv.Default(request.MessageType, origin.MessageType)),
		PolicyLinkReason:        new(origin.PolicyLinkReason),
		ExternalConversationId:  new(origin.ExternalConversationID),
		ContentTruncated:        new(truncated),
		Body:                    new(request.Body),
		ToolCalls:               toolCalls,
		CustomRuleIds:           slices.Clone(request.CustomRuleIDs),
	}.Build()
}

func promptPolicyEnforcement(request DispatchRequest, origin metering.RiskProvenance, policyID, requestID, createdAt string, truncated bool) *riskv1.PromptPolicyEnforcement {
	toolCalls := make([]*riskv1.PromptPolicyEnforcement_ToolCall, 0, len(request.ToolCalls))
	for _, call := range request.ToolCalls {
		toolCalls = append(toolCalls, riskv1.PromptPolicyEnforcement_ToolCall_builder{
			Id: new(call.ID), Name: new(call.Name), Arguments: new(call.Arguments),
		}.Build())
	}
	config := request.PromptPolicies[policyID]
	return riskv1.PromptPolicyEnforcement_builder{
		RequestId:               new(requestID),
		ChatMessageId:           stringPointer(origin.ChatMessageID),
		ProjectId:               new(request.ProjectID),
		OrganizationId:          new(request.OrganizationID),
		RiskPolicyId:            new(policyID),
		RiskPolicyVersion:       new(origin.RiskPolicyVersion),
		CreatedAt:               new(createdAt),
		Content:                 new(request.Content),
		ContentPartId:           stringPointer(origin.ContentPartID),
		ChatId:                  stringPointer(origin.ChatID),
		OriginRiskPolicyId:      stringPointer(origin.RiskPolicyID),
		OriginRiskPolicyVersion: new(origin.RiskPolicyVersion),
		MessageLinkReason:       new(origin.MessageLinkReason),
		ExecutionPath:           new(origin.ExecutionPath),
		ToolCallId:              new(origin.ToolCallID),
		ToolName:                new(conv.Default(request.ToolName, origin.ToolName)),
		HookSource:              new(origin.HookSource),
		UserId:                  new(origin.UserID),
		MessageType:             new(conv.Default(request.MessageType, origin.MessageType)),
		PolicyLinkReason:        new(origin.PolicyLinkReason),
		ExternalConversationId:  new(origin.ExternalConversationID),
		ContentTruncated:        new(truncated),
		Body:                    new(request.Body),
		ToolCalls:               toolCalls,
		Prompt:                  new(config.Prompt),
		ModelConfig:             slices.Clone(config.ModelConfig),
	}.Build()
}

func truncateAtRuneBoundary(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

func stringPointer(id uuid.UUID) *string {
	if id == uuid.Nil {
		return nil
	}
	return new(id.String())
}

// Close flushes and stops the dispatcher's publishers.
func (d *Dispatcher) Close(ctx context.Context) error {
	if err := d.close(ctx); err != nil {
		return fmt.Errorf("close enforcement dispatcher: %w", err)
	}
	return nil
}

var (
	_ EnforcementLane = (*typedEnforcementLane[*riskv1.GitleaksEnforcement])(nil)
	_ EnforcementLane = (*typedEnforcementLane[*riskv1.PresidioEnforcement])(nil)
	_ EnforcementLane = (*typedEnforcementLane[*riskv1.PromptInjectionEnforcement])(nil)
	_ EnforcementLane = (*typedEnforcementLane[*riskv1.CustomRulesEnforcement])(nil)
	_ EnforcementLane = (*typedEnforcementLane[*riskv1.PromptPolicyEnforcement])(nil)
	_ EnforcementLane = (*typedEnforcementLane[*riskv1.LLMEnforcement])(nil)
)
