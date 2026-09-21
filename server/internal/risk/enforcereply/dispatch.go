package enforcereply

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
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

	// MaxContentBytes bounds enforcement scan cost below Pub/Sub's transport
	// limit. It applies independently to Content and to the LLM lane's Body.
	MaxContentBytes = 1 * 1024 * 1024
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

// DispatcherConfig controls bounded request publication and reply waiting.
type DispatcherConfig struct {
	// WaitTimeout caps each lane's publication and reply wait unless
	// LaneWaitTimeout overrides it for that lane's scanner.
	WaitTimeout time.Duration

	// LaneWaitTimeout overrides WaitTimeout per scanner. Scanners without an
	// entry, or with a non-positive entry, use WaitTimeout.
	LaneWaitTimeout map[riskv1.EnforcementScanner]time.Duration
}

// ToolCall is one tool invocation carried to the LLM analyzer lane.
type ToolCall struct {
	// ID is the harness-assigned tool call id, or a synthetic one when the
	// harness supplied none.
	ID string

	// Name is the tool the agent invoked.
	Name string

	// Arguments is the serialized tool input, typically JSON.
	Arguments string
}

// DispatchRequest contains tenant context, content, and requested lanes.
type DispatchRequest struct {
	// OrganizationID is the tenant used for fingerprint isolation.
	OrganizationID string

	// OrganizationSlug is carried by the LLM lane for telemetry dimensions only.
	OrganizationSlug string

	// ProjectID identifies the project whose policy configuration applies.
	ProjectID string

	// Content is the raw text scanned by each lane.
	Content string

	// Body is the message text the LLM lane renders into its prompt. It is
	// truncated to MaxContentBytes independently of Content.
	Body string

	// ToolName is the tool the LLM lane attributes the message to. When empty,
	// the lane falls back to the origin's ToolName.
	ToolName string

	// MessageType is the message kind the LLM lane evaluates. When empty, the
	// lane falls back to the origin's MessageType.
	MessageType string

	// ToolCalls are the tool invocations the LLM lane evaluates alongside Body.
	ToolCalls []ToolCall

	// PresidioEntities is an optional scanner superset; empty requests all entities.
	PresidioEntities []string

	// PresidioScoreThreshold is an optional scanner minimum.
	PresidioScoreThreshold *float64

	// Lanes is the distinct set of scanner and policy results required.
	Lanes []Lane

	// Origins carries immutable per-lane attribution. Lane.PolicyID remains
	// reserved for reply correlation and is not policy provenance.
	Origins map[Lane]metering.RiskProvenance
}

// Dispatcher fans enforcement work out over independent request brokers.
type Dispatcher struct {
	gitleaks    EnforcementLane
	presidio    EnforcementLane
	llm         EnforcementLane
	close       func(context.Context) error
	waitTimeout time.Duration
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
	gitleaksPub, err := gcp.PubSubPublisherForMessage(ctx, broker, &riskv1.GitleaksEnforcement{})
	if err != nil {
		return nil, fmt.Errorf("create gitleaks enforcement publisher: %w", err)
	}
	presidioPub, err := gcp.PubSubPublisherForMessage(ctx, broker, &riskv1.PresidioEnforcement{})
	if err != nil {
		stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = gitleaksPub.Stop(stopCtx)
		return nil, fmt.Errorf("create presidio enforcement publisher: %w", err)
	}
	llmPub, err := gcp.PubSubPublisherForMessage(ctx, broker, &riskv1.LLMEnforcement{})
	if err != nil {
		stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = gitleaksPub.Stop(stopCtx)
		_ = presidioPub.Stop(stopCtx)
		return nil, fmt.Errorf("create llm enforcement publisher: %w", err)
	}
	gitleaksReq := redisinbox.NewRequestBroker(inbox, gitleaksPub)
	presidioReq := redisinbox.NewRequestBroker(inbox, presidioPub)
	llmReq := redisinbox.NewRequestBroker(inbox, llmPub)
	return &Dispatcher{
		gitleaks: &typedEnforcementLane[*riskv1.GitleaksEnforcement]{broker: gitleaksReq},
		presidio: &typedEnforcementLane[*riskv1.PresidioEnforcement]{broker: presidioReq},
		llm:      &typedEnforcementLane[*riskv1.LLMEnforcement]{broker: llmReq},
		close: func(ctx context.Context) error {
			// Stop the lanes concurrently so each flush gets the full shutdown
			// budget instead of whatever the previous lane left of it.
			var gitleaksErr, presidioErr, llmErr error
			var wg sync.WaitGroup
			wg.Add(3)
			go func() {
				defer wg.Done()
				gitleaksErr = gitleaksReq.Close(ctx)
			}()
			go func() {
				defer wg.Done()
				presidioErr = presidioReq.Close(ctx)
			}()
			go func() {
				defer wg.Done()
				llmErr = llmReq.Close(ctx)
			}()
			wg.Wait()
			var closeErrs []error
			if gitleaksErr != nil {
				closeErrs = append(closeErrs, fmt.Errorf("close gitleaks enforcement lane: %w", gitleaksErr))
			}
			if presidioErr != nil {
				closeErrs = append(closeErrs, fmt.Errorf("close presidio enforcement lane: %w", presidioErr))
			}
			if llmErr != nil {
				closeErrs = append(closeErrs, fmt.Errorf("close llm enforcement lane: %w", llmErr))
			}
			return errors.Join(closeErrs...)
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
		metric.WithDescription("Number of enforcement requests truncated to the 1 MiB limit before publication"),
		metric.WithUnit("{message}"),
	)
	return truncations
}

// Dispatch fans content out to distinct lanes and folds replies by lane.
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
	truncated := false
	if originalSize := len(request.Content); originalSize > MaxContentBytes {
		request.Content = truncateAtRuneBoundary(request.Content, MaxContentBytes)
		truncated = true
		d.logger.WarnContext(ctx, "truncating oversized enforcement content", attr.SlogRiskScanTextSize(originalSize))
	}
	if originalSize := len(request.Body); originalSize > MaxContentBytes {
		request.Body = truncateAtRuneBoundary(request.Body, MaxContentBytes)
		truncated = true
		d.logger.WarnContext(ctx, "truncating oversized enforcement body", attr.SlogRiskScanTextSize(originalSize))
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
		supported := lane.Scanner == riskv1.EnforcementScanner_ENFORCEMENT_SCANNER_GITLEAKS ||
			lane.Scanner == riskv1.EnforcementScanner_ENFORCEMENT_SCANNER_PRESIDIO ||
			lane.Scanner == riskv1.EnforcementScanner_ENFORCEMENT_SCANNER_LLM_ANALYZER
		if !supported || lane.PolicyID != "" {
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
				stokenCount, countErr := d.stokenCodec.Count(laneCtx, request.Content)
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
			mu.Unlock()
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return Outcome{}, fmt.Errorf("dispatch enforcement lanes: %w", err)
	}
	return Outcome{ByLane: byLane, Failed: failed, Complete: len(byLane) == len(request.Lanes), Deadline: deadline, Truncated: truncated}, nil
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
	_ EnforcementLane = (*typedEnforcementLane[*riskv1.LLMEnforcement])(nil)
)
