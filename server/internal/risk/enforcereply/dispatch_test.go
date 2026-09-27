package enforcereply

import (
	"context"
	"errors"
	"fmt"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"maps"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"google.golang.org/protobuf/proto"

	meteringv1 "github.com/speakeasy-api/gram/infra/gen/gram/metering/v1"
	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/redisinbox"
	"github.com/speakeasy-api/gram/server/internal/requestreply"
	"github.com/speakeasy-api/gram/server/internal/stokens"
)

type capturePublisher[T proto.Message] struct {
	messages   []T
	attributes []map[string]string
	onPublish  func(context.Context, T, map[string]string) error
}

func (p *capturePublisher[T]) Publish(ctx context.Context, message T, options ...gcp.PublishOption) gcp.PublishResult {
	var opts gcp.PublishOptions
	for _, option := range options {
		option(&opts)
	}
	attributes := maps.Clone(opts.Attributes)
	p.messages = append(p.messages, message)
	p.attributes = append(p.attributes, attributes)
	if p.onPublish != nil {
		if err := p.onPublish(ctx, message, attributes); err != nil {
			return gcp.NewErrPublishResult(err)
		}
	}
	return gcp.NewSuccessPublishResult()
}

func (p *capturePublisher[T]) Stop(context.Context) error {
	return nil
}

type (
	captureEnforcementPublisher = capturePublisher[*riskv1.GitleaksEnforcement]
	capturePresidioPublisher    = capturePublisher[*riskv1.PresidioEnforcement]
	captureLLMPublisher         = capturePublisher[*riskv1.LLMEnforcement]
)

// replyOK answers every publish on the lane with an OK reply so the dispatcher's
// waiter resolves immediately.
func replyOK[T proto.Message](te *inboxTestEnv, lane Lane) func(context.Context, T, map[string]string) error {
	return func(ctx context.Context, _ T, attributes map[string]string) error {
		replyURN := attributes[requestreply.ReplyURNAttribute]
		_, correlationID, err := ParseReplyURN(replyURN)
		if err != nil {
			return err
		}
		return te.writer.Reply(ctx, replyURN, testReply(correlationID, lane, riskv1.EnforcementStatus_ENFORCEMENT_STATUS_OK))
	}
}

func int64CounterValue(metrics metricdata.ResourceMetrics, name string) int64 {
	for _, scope := range metrics.ScopeMetrics {
		for _, candidate := range scope.Metrics {
			if candidate.Name != name {
				continue
			}
			sum, ok := candidate.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}
			var total int64
			for _, point := range sum.DataPoints {
				total += point.Value
			}
			return total
		}
	}
	return 0
}

func contentLimitFlags(t *testing.T, limit int) *feature.InMemory {
	t.Helper()
	flags := &feature.InMemory{}
	flags.SetFlagPayload(feature.FlagRiskEnforcementMaxContentBytes, contentLimitDistinctID, fmt.Appendf(nil, `{"max_content_bytes":%d}`, limit))
	return flags
}

func testDispatcherWithFlags(te *inboxTestEnv, publisher *captureEnforcementPublisher, flags feature.Provider) *Dispatcher {
	presidioPub := &capturePresidioPublisher{messages: nil, attributes: nil, onPublish: nil}
	llmPub := &captureLLMPublisher{messages: nil, attributes: nil, onPublish: nil}
	return testDispatcherWithLanes(te, publisher, presidioPub, llmPub, DispatcherConfig{WaitTimeout: time.Second, LaneWaitTimeout: nil, Flags: flags})
}

func testDispatcher(te *inboxTestEnv, publisher *captureEnforcementPublisher, waitTimeout time.Duration) *Dispatcher {
	return testDispatcherWithPresidio(te, publisher, &capturePresidioPublisher{messages: nil, attributes: nil, onPublish: nil}, waitTimeout)
}

func testDispatcherWithPresidio(te *inboxTestEnv, gitleaksPub *captureEnforcementPublisher, presidioPub *capturePresidioPublisher, waitTimeout time.Duration) *Dispatcher {
	llmPub := &captureLLMPublisher{messages: nil, attributes: nil, onPublish: nil}
	return testDispatcherWithLanes(te, gitleaksPub, presidioPub, llmPub, DispatcherConfig{WaitTimeout: waitTimeout, LaneWaitTimeout: nil, Flags: nil})
}

func testDispatcherWithLanes(te *inboxTestEnv, gitleaksPub *captureEnforcementPublisher, presidioPub *capturePresidioPublisher, llmPub *captureLLMPublisher, cfg DispatcherConfig) *Dispatcher {
	gitleaksReq := redisinbox.NewRequestBroker(te.inbox, gitleaksPub)
	presidioReq := redisinbox.NewRequestBroker(te.inbox, presidioPub)
	llmReq := redisinbox.NewRequestBroker(te.inbox, llmPub)
	return &Dispatcher{
		gitleaks: &typedEnforcementLane[*riskv1.GitleaksEnforcement]{broker: gitleaksReq},
		presidio: &typedEnforcementLane[*riskv1.PresidioEnforcement]{broker: presidioReq},
		llm:      &typedEnforcementLane[*riskv1.LLMEnforcement]{broker: llmReq},
		close: func(ctx context.Context) error {
			return errors.Join(gitleaksReq.Close(ctx), presidioReq.Close(ctx), llmReq.Close(ctx))
		},
		waitTimeout:     cfg.WaitTimeout,
		laneWaitTimeout: cfg.LaneWaitTimeout,
		flags:           cfg.Flags,
		logger:          newTestLogger(),
		truncations:     newTruncationCounter(te.meterProvider),
		stokenCodec:     stokens.NewCodec(),
	}
}

func testLLMDispatcher(te *inboxTestEnv, llmPub *captureLLMPublisher, cfg DispatcherConfig) *Dispatcher {
	gitleaksPub := &captureEnforcementPublisher{messages: nil, attributes: nil, onPublish: nil}
	presidioPub := &capturePresidioPublisher{messages: nil, attributes: nil, onPublish: nil}
	return testDispatcherWithLanes(te, gitleaksPub, presidioPub, llmPub, cfg)
}

func llmDispatchRequest(lanes []Lane, origins map[Lane]metering.RiskProvenance) DispatchRequest {
	return DispatchRequest{
		OrganizationID:   "org-llm",
		OrganizationSlug: "org-llm-slug",
		ProjectID:        "project-llm",
		Content:          "raw scanned content",
		Body:             "please run the deploy",
		ToolName:         "bash",
		MessageType:      "tool_request",
		ToolCalls: []ToolCall{
			{ID: "toolu_1", Name: "bash", Arguments: `{"command":"git reset --hard"}`},
			{ID: "toolu_2", Name: "read_file", Arguments: `{"path":".env"}`},
		},
		PresidioEntities:       nil,
		PresidioScoreThreshold: nil,
		Lanes:                  lanes,
		Origins:                origins,
	}
}

func testOrigins(lanes ...Lane) map[Lane]metering.RiskProvenance {
	origins := make(map[Lane]metering.RiskProvenance, len(lanes))
	operationID := uuid.NewString()
	for _, lane := range lanes {
		origins[lane] = metering.RiskProvenance{
			OrganizationID:         "org",
			ProjectID:              uuid.New(),
			RiskPolicyID:           uuid.New(),
			RiskPolicyVersion:      1,
			PolicyLinkReason:       "",
			ChatID:                 uuid.Nil,
			ExternalConversationID: "external/session:dispatch",
			ChatMessageID:          uuid.Nil,
			ContentPartID:          uuid.Nil,
			MessageLinkReason:      "realtime_not_persisted",
			OperationID:            operationID,
			ExecutionPath:          "realtime_streams",
			RequestID:              "",
			MessageType:            "user_message",
			HookSource:             "test",
			UserID:                 "user",
			ToolCallID:             "",
			ToolName:               "",
			Model:                  "",
			Provider:               "",
		}
	}
	return origins
}

func TestDispatchPublishesTenantContextAndReplyMetadata(t *testing.T) {
	t.Parallel()

	te := setupInboxTest(t, "replica-dispatch")
	publisher := &captureEnforcementPublisher{messages: nil, attributes: nil, onPublish: nil}
	publisher.onPublish = func(ctx context.Context, _ *riskv1.GitleaksEnforcement, attributes map[string]string) error {
		if te.inbox.Snapshot().Waiters != 1 {
			return errors.New("publisher observed request without a registered waiter")
		}
		replyURN := attributes[requestreply.ReplyURNAttribute]
		_, correlationID, err := ParseReplyURN(replyURN)
		if err != nil {
			return err
		}
		return te.writer.Reply(ctx, replyURN, testReply(correlationID, gitleaksLane, riskv1.EnforcementStatus_ENFORCEMENT_STATUS_OK))
	}
	dispatcher := testDispatcher(te, publisher, time.Second)

	outcome, err := dispatcher.Dispatch(t.Context(), DispatchRequest{
		OrganizationID: "org-dispatch",
		ProjectID:      "project-dispatch",
		Content:        "safe content",
		Lanes:          []Lane{gitleaksLane},
		Origins:        testOrigins(gitleaksLane),
	})
	require.NoError(t, err)
	require.True(t, outcome.Complete)
	require.False(t, outcome.Deadline)
	require.False(t, outcome.Truncated)
	require.NotNil(t, outcome.ByLane[gitleaksLane])
	require.Len(t, publisher.messages, 1)
	message := publisher.messages[0]
	require.Equal(t, "org-dispatch", message.GetOrganizationId())
	require.Equal(t, "project-dispatch", message.GetProjectId())
	require.Equal(t, "safe content", message.GetContent())
	require.False(t, message.GetContentTruncated())
	require.NotEmpty(t, message.GetCreatedAt())
	_, err = time.Parse(time.RFC3339Nano, message.GetCreatedAt())
	require.NoError(t, err)
	_, err = uuid.Parse(message.GetRequestId())
	require.NoError(t, err)
	require.Len(t, publisher.attributes, 1)
	replyURN := publisher.attributes[0][requestreply.ReplyURNAttribute]
	_, correlationID, err := ParseReplyURN(replyURN)
	require.NoError(t, err)
	parsedCorrelationID, err := uuid.Parse(correlationID)
	require.NoError(t, err)
	require.Equal(t, uuid.Version(7), parsedCorrelationID.Version())
	require.NotEqual(t, message.GetRequestId(), correlationID)
}

func TestDispatchAcceptsOpaqueOperationID(t *testing.T) {
	t.Parallel()

	te := setupInboxTest(t, "replica-dispatch-urn")
	publisher := &captureEnforcementPublisher{messages: nil, attributes: nil, onPublish: nil}
	publisher.onPublish = func(ctx context.Context, _ *riskv1.GitleaksEnforcement, attributes map[string]string) error {
		replyURN := attributes[requestreply.ReplyURNAttribute]
		_, correlationID, err := ParseReplyURN(replyURN)
		if err != nil {
			return err
		}
		return te.writer.Reply(ctx, replyURN, testReply(correlationID, gitleaksLane, riskv1.EnforcementStatus_ENFORCEMENT_STATUS_OK))
	}
	dispatcher := testDispatcher(te, publisher, time.Second)

	origins := testOrigins(gitleaksLane)
	origin := origins[gitleaksLane]
	origin.OperationID = "anthropic-inference:req_011CT4Xy9nqPbFjR2hQ7wK8L:3"
	origins[gitleaksLane] = origin

	outcome, err := dispatcher.Dispatch(t.Context(), DispatchRequest{
		OrganizationID: "org-dispatch",
		ProjectID:      "project-dispatch",
		Content:        "safe content",
		Lanes:          []Lane{gitleaksLane},
		Origins:        origins,
	})
	require.NoError(t, err)
	require.True(t, outcome.Complete)
	require.NotNil(t, outcome.ByLane[gitleaksLane])
	require.Len(t, publisher.messages, 1)
	require.Equal(t, origin.OperationID, publisher.messages[0].GetRequestId())
}

func TestDispatchRejectsEmptyOperationID(t *testing.T) {
	t.Parallel()

	te := setupInboxTest(t, "replica-dispatch-empty")
	dispatcher := testDispatcher(te, &captureEnforcementPublisher{messages: nil, attributes: nil, onPublish: nil}, time.Second)

	origins := testOrigins(gitleaksLane)
	origin := origins[gitleaksLane]
	origin.OperationID = " "
	origins[gitleaksLane] = origin

	_, err := dispatcher.Dispatch(t.Context(), DispatchRequest{
		OrganizationID: "org-dispatch",
		ProjectID:      "project-dispatch",
		Content:        "safe content",
		Lanes:          []Lane{gitleaksLane},
		Origins:        origins,
	})
	require.ErrorContains(t, err, "operation id is required")
}

func TestDispatchPreservesExplicitNoPolicyGitleaksOrigin(t *testing.T) {
	t.Parallel()

	te := setupInboxTest(t, "replica-dispatch-no-policy")
	publisher := &captureEnforcementPublisher{messages: nil, attributes: nil, onPublish: nil}
	publisher.onPublish = func(ctx context.Context, _ *riskv1.GitleaksEnforcement, attributes map[string]string) error {
		replyURN := attributes[requestreply.ReplyURNAttribute]
		_, correlationID, err := ParseReplyURN(replyURN)
		if err != nil {
			return err
		}
		return te.writer.Reply(ctx, replyURN, testReply(correlationID, gitleaksLane, riskv1.EnforcementStatus_ENFORCEMENT_STATUS_OK))
	}
	dispatcher := testDispatcher(te, publisher, time.Second)
	origins := testOrigins(gitleaksLane)
	origin := origins[gitleaksLane]
	origin.RiskPolicyID = uuid.Nil
	origin.RiskPolicyVersion = 0
	origin.PolicyLinkReason = "realtime_no_matching_policy"
	origins[gitleaksLane] = origin

	outcome, err := dispatcher.Dispatch(t.Context(), DispatchRequest{
		OrganizationID: "org-no-policy",
		ProjectID:      origin.ProjectID.String(),
		Content:        "safe content",
		Lanes:          []Lane{gitleaksLane},
		Origins:        origins,
	})
	require.NoError(t, err)
	require.True(t, outcome.Complete)
	require.Len(t, publisher.messages, 1)
	message := publisher.messages[0]
	require.Empty(t, message.GetOriginRiskPolicyId())
	require.Zero(t, message.GetOriginRiskPolicyVersion())
	require.Equal(t, origin.PolicyLinkReason, message.GetPolicyLinkReason())
}

func TestDispatchFansOutGitleaksAndPresidioLanes(t *testing.T) {
	t.Parallel()

	te := setupInboxTest(t, "replica-dispatch-presidio")
	gitleaksPub := &captureEnforcementPublisher{messages: nil, attributes: nil, onPublish: nil}
	gitleaksPub.onPublish = func(ctx context.Context, _ *riskv1.GitleaksEnforcement, attributes map[string]string) error {
		replyURN := attributes[requestreply.ReplyURNAttribute]
		_, correlationID, err := ParseReplyURN(replyURN)
		if err != nil {
			return err
		}
		return te.writer.Reply(ctx, replyURN, testReply(correlationID, gitleaksLane, riskv1.EnforcementStatus_ENFORCEMENT_STATUS_OK))
	}
	presidioPub := &capturePresidioPublisher{messages: nil, attributes: nil, onPublish: nil}
	presidioPub.onPublish = func(ctx context.Context, _ *riskv1.PresidioEnforcement, attributes map[string]string) error {
		replyURN := attributes[requestreply.ReplyURNAttribute]
		_, correlationID, err := ParseReplyURN(replyURN)
		if err != nil {
			return err
		}
		return te.writer.Reply(ctx, replyURN, testReply(correlationID, presidioLane, riskv1.EnforcementStatus_ENFORCEMENT_STATUS_OK))
	}
	dispatcher := testDispatcherWithPresidio(te, gitleaksPub, presidioPub, time.Second)

	origins := testOrigins(gitleaksLane, presidioLane)
	threshold := 0.25
	outcome, err := dispatcher.Dispatch(t.Context(), DispatchRequest{
		OrganizationID:         "org-presidio",
		ProjectID:              "project-presidio",
		Content:                "safe content",
		PresidioEntities:       []string{"EMAIL_ADDRESS", "PHONE_NUMBER"},
		PresidioScoreThreshold: &threshold,
		Lanes:                  []Lane{gitleaksLane, presidioLane},
		Origins:                origins,
	})
	require.NoError(t, err)
	require.True(t, outcome.Complete)
	require.NotNil(t, outcome.ByLane[gitleaksLane])
	require.NotNil(t, outcome.ByLane[presidioLane])
	require.Len(t, presidioPub.messages, 1)
	message := presidioPub.messages[0]
	require.Equal(t, "org-presidio", message.GetOrganizationId())
	require.Equal(t, "project-presidio", message.GetProjectId())
	require.Equal(t, "safe content", message.GetContent())
	require.Equal(t, origins[presidioLane].RiskPolicyID.String(), message.GetOriginRiskPolicyId())
	require.Equal(t, origins[presidioLane].RiskPolicyVersion, message.GetOriginRiskPolicyVersion())
	require.Equal(t, origins[presidioLane].ExecutionPath, message.GetExecutionPath())
	require.Equal(t, origins[presidioLane].MessageLinkReason, message.GetMessageLinkReason())
	require.Empty(t, message.GetRiskPolicyId(), "shared origin must not alter finding/reply policy correlation")
	var reading meteringv1.MeterReading
	require.NoError(t, proto.Unmarshal(message.GetMeterReading(), &reading))
	require.Equal(t, origins[presidioLane].OperationID, reading.GetAttributes()[metering.AttributeScanRequestID])
	require.Equal(t, origins[presidioLane].RiskPolicyID.String(), reading.GetAttributes()[metering.AttributeRiskPolicyID])
	require.Equal(t, "external/session:dispatch", reading.GetAttributes()[metering.AttributeExternalConversationID])
	require.NotContains(t, reading.GetAttributes(), metering.AttributeChatID)
	require.Positive(t, reading.GetValue())
	require.Equal(t, []string{"EMAIL_ADDRESS", "PHONE_NUMBER"}, message.GetEntities())
	require.True(t, message.HasScoreThreshold())
	require.InDelta(t, threshold, message.GetScoreThreshold(), 1e-9)
	_, err = time.Parse(time.RFC3339Nano, message.GetCreatedAt())
	require.NoError(t, err)
	_, err = uuid.Parse(message.GetRequestId())
	require.NoError(t, err)
	require.Len(t, presidioPub.attributes, 1)
	_, correlationID, err := ParseReplyURN(presidioPub.attributes[0][requestreply.ReplyURNAttribute])
	require.NoError(t, err)
	require.NotEmpty(t, correlationID)
	// Both lanes share one request id but each gets its own correlation id.
	require.Equal(t, origins[gitleaksLane].OperationID, gitleaksPub.messages[0].GetRequestId())
	require.Equal(t, origins[gitleaksLane].RiskPolicyID.String(), gitleaksPub.messages[0].GetOriginRiskPolicyId())
	require.Equal(t, "external/session:dispatch", gitleaksPub.messages[0].GetExternalConversationId())
	require.Empty(t, gitleaksPub.messages[0].GetChatId())
	require.Equal(t, gitleaksPub.messages[0].GetRequestId(), message.GetRequestId())
	_, gitleaksCorrelation, err := ParseReplyURN(gitleaksPub.attributes[0][requestreply.ReplyURNAttribute])
	require.NoError(t, err)
	require.NotEqual(t, gitleaksCorrelation, correlationID)
}

func TestDispatchPreservesSuccessfulSiblingOnLaneFailure(t *testing.T) {
	t.Parallel()

	te := setupInboxTest(t, "replica-dispatch-partial")
	gitleaksPub := &captureEnforcementPublisher{onPublish: func(context.Context, *riskv1.GitleaksEnforcement, map[string]string) error {
		return errors.New("publish failed")
	}}
	presidioPub := &capturePresidioPublisher{onPublish: func(ctx context.Context, _ *riskv1.PresidioEnforcement, attributes map[string]string) error {
		replyURN := attributes[requestreply.ReplyURNAttribute]
		_, correlationID, err := ParseReplyURN(replyURN)
		if err != nil {
			return err
		}
		return te.writer.Reply(ctx, replyURN, testReply(correlationID, presidioLane, riskv1.EnforcementStatus_ENFORCEMENT_STATUS_OK))
	}}
	dispatcher := testDispatcherWithPresidio(te, gitleaksPub, presidioPub, time.Second)

	outcome, err := dispatcher.Dispatch(t.Context(), DispatchRequest{
		OrganizationID: "org-partial",
		ProjectID:      "project-partial",
		Content:        "safe content",
		Lanes:          []Lane{gitleaksLane, presidioLane},
		Origins:        testOrigins(gitleaksLane, presidioLane),
	})
	require.NoError(t, err)
	require.False(t, outcome.Complete)
	require.ErrorContains(t, outcome.Failed[gitleaksLane], "publish failed")
	require.NotNil(t, outcome.ByLane[presidioLane])
}

func TestDispatchDeadlineIsNormalPartialOutcome(t *testing.T) {
	t.Parallel()

	te := setupInboxTest(t, "replica-dispatch-deadline")
	publisher := &captureEnforcementPublisher{messages: nil, attributes: nil, onPublish: nil}
	dispatcher := testDispatcher(te, publisher, 25*time.Millisecond)

	outcome, err := dispatcher.Dispatch(t.Context(), DispatchRequest{
		OrganizationID: "org-deadline",
		ProjectID:      "project-deadline",
		Content:        "safe content",
		Lanes:          []Lane{gitleaksLane},
		Origins:        testOrigins(gitleaksLane),
	})
	require.NoError(t, err)
	require.False(t, outcome.Complete)
	require.True(t, outcome.Deadline)
	require.Empty(t, outcome.ByLane)
	require.Zero(t, te.inbox.Snapshot().Waiters)
}

func TestDispatchUsesDefaultContentLimit(t *testing.T) {
	t.Parallel()

	te := setupInboxTest(t, "replica-dispatch-default-limit")
	publisher := &captureEnforcementPublisher{messages: nil, attributes: nil, onPublish: nil}
	publisher.onPublish = replyOK[*riskv1.GitleaksEnforcement](te, gitleaksLane)
	dispatcher := testDispatcher(te, publisher, time.Second)

	outcome, err := dispatcher.Dispatch(t.Context(), DispatchRequest{
		OrganizationID:         "org-default-limit",
		OrganizationSlug:       "",
		ProjectID:              "project-default-limit",
		Content:                strings.Repeat("x", DefaultMaxContentBytes+10),
		Body:                   "",
		ToolName:               "",
		MessageType:            "",
		ToolCalls:              nil,
		PresidioEntities:       nil,
		PresidioScoreThreshold: nil,
		Lanes:                  []Lane{gitleaksLane},
		Origins:                testOrigins(gitleaksLane),
	})
	require.NoError(t, err)
	require.True(t, outcome.Complete)
	require.True(t, outcome.Truncated)
	require.Len(t, publisher.messages, 1)
	require.Equal(t, strings.Repeat("x", DefaultMaxContentBytes), publisher.messages[0].GetContent())
	require.True(t, publisher.messages[0].GetContentTruncated())
	var metrics metricdata.ResourceMetrics
	require.NoError(t, te.reader.Collect(t.Context(), &metrics))
	require.Equal(t, int64(1), int64CounterValue(metrics, "risk.enforcement.truncations"))
}

func TestDispatchClampsContentLimitAboveCeiling(t *testing.T) {
	t.Parallel()

	te := setupInboxTest(t, "replica-dispatch-limit-ceiling")
	publisher := &captureEnforcementPublisher{messages: nil, attributes: nil, onPublish: nil}
	publisher.onPublish = replyOK[*riskv1.GitleaksEnforcement](te, gitleaksLane)
	dispatcher := testDispatcherWithFlags(te, publisher, contentLimitFlags(t, MaxContentBytes*2))

	outcome, err := dispatcher.Dispatch(t.Context(), DispatchRequest{
		OrganizationID:         "org-limit-ceiling",
		OrganizationSlug:       "",
		ProjectID:              "project-limit-ceiling",
		Content:                strings.Repeat("x", MaxContentBytes+10),
		Body:                   "",
		ToolName:               "",
		MessageType:            "",
		ToolCalls:              nil,
		PresidioEntities:       nil,
		PresidioScoreThreshold: nil,
		Lanes:                  []Lane{gitleaksLane},
		Origins:                testOrigins(gitleaksLane),
	})
	require.NoError(t, err)
	require.True(t, outcome.Complete)
	require.True(t, outcome.Truncated)
	require.Len(t, publisher.messages, 1)
	require.Equal(t, strings.Repeat("x", MaxContentBytes), publisher.messages[0].GetContent())
}

func TestDispatchTruncatesAtMultibyteRuneBoundary(t *testing.T) {
	t.Parallel()

	te := setupInboxTest(t, "replica-dispatch-multibyte")
	gitleaksPub := &captureEnforcementPublisher{messages: nil, attributes: nil, onPublish: nil}
	gitleaksPub.onPublish = replyOK[*riskv1.GitleaksEnforcement](te, gitleaksLane)
	dispatcher := testDispatcher(te, gitleaksPub, time.Second)
	expected := strings.Repeat("x", DefaultMaxContentBytes-1)

	outcome, err := dispatcher.Dispatch(t.Context(), DispatchRequest{
		OrganizationID:         "org-multibyte",
		OrganizationSlug:       "",
		ProjectID:              "project-multibyte",
		Content:                expected + "€tail",
		Body:                   "",
		ToolName:               "",
		MessageType:            "",
		ToolCalls:              nil,
		PresidioEntities:       nil,
		PresidioScoreThreshold: nil,
		Lanes:                  []Lane{gitleaksLane},
		Origins:                testOrigins(gitleaksLane),
	})
	require.NoError(t, err)
	require.True(t, outcome.Complete)
	require.True(t, outcome.Truncated)
	require.Len(t, gitleaksPub.messages, 1)
	require.Equal(t, expected, gitleaksPub.messages[0].GetContent())
	require.True(t, utf8.ValidString(gitleaksPub.messages[0].GetContent()))
	require.True(t, gitleaksPub.messages[0].GetContentTruncated())
}

func TestDispatchPublishesLLMLaneFields(t *testing.T) {
	t.Parallel()

	te := setupInboxTest(t, "replica-dispatch-llm")
	llmPub := &captureLLMPublisher{messages: nil, attributes: nil, onPublish: nil}
	llmPub.onPublish = replyOK[*riskv1.LLMEnforcement](te, llmLane)
	dispatcher := testLLMDispatcher(te, llmPub, DispatcherConfig{WaitTimeout: time.Second, LaneWaitTimeout: nil, Flags: nil})

	origins := testOrigins(llmLane)
	origin := origins[llmLane]
	origin.ToolCallID = "toolu_1"
	origins[llmLane] = origin
	request := llmDispatchRequest([]Lane{llmLane}, origins)

	outcome, err := dispatcher.Dispatch(t.Context(), request)
	require.NoError(t, err)
	require.True(t, outcome.Complete)
	require.False(t, outcome.Deadline)
	require.False(t, outcome.Truncated)
	require.Empty(t, outcome.Failed)
	reply := outcome.ByLane[llmLane]
	require.NotNil(t, reply)
	require.Equal(t, riskv1.EnforcementScanner_ENFORCEMENT_SCANNER_LLM_ANALYZER, reply.GetScanner())
	require.Empty(t, reply.GetPolicyId())

	require.Len(t, llmPub.messages, 1)
	message := llmPub.messages[0]
	require.Equal(t, "org-llm", message.GetOrganizationId())
	require.Equal(t, "org-llm-slug", message.GetOrganizationSlug())
	require.Equal(t, "project-llm", message.GetProjectId())
	require.Equal(t, "raw scanned content", message.GetContent())
	require.Equal(t, "please run the deploy", message.GetBody())
	require.Equal(t, "bash", message.GetToolName())
	require.Equal(t, "tool_request", message.GetMessageType())
	require.False(t, message.GetContentTruncated())
	require.Len(t, message.GetToolCalls(), 2)
	require.Equal(t, "toolu_1", message.GetToolCalls()[0].GetId())
	require.Equal(t, "bash", message.GetToolCalls()[0].GetName())
	require.JSONEq(t, `{"command":"git reset --hard"}`, message.GetToolCalls()[0].GetArguments())
	require.Equal(t, "toolu_2", message.GetToolCalls()[1].GetId())
	require.Equal(t, "read_file", message.GetToolCalls()[1].GetName())
	require.JSONEq(t, `{"path":".env"}`, message.GetToolCalls()[1].GetArguments())

	// Provenance is copied from the lane origin exactly as the gitleaks lane does.
	require.Equal(t, origin.OperationID, message.GetRequestId())
	require.Equal(t, origin.RiskPolicyID.String(), message.GetOriginRiskPolicyId())
	require.Equal(t, origin.RiskPolicyVersion, message.GetOriginRiskPolicyVersion())
	require.Equal(t, origin.ExecutionPath, message.GetExecutionPath())
	require.Equal(t, origin.MessageLinkReason, message.GetMessageLinkReason())
	require.Equal(t, origin.PolicyLinkReason, message.GetPolicyLinkReason())
	require.Equal(t, origin.ExternalConversationID, message.GetExternalConversationId())
	require.Equal(t, origin.HookSource, message.GetHookSource())
	require.Equal(t, origin.UserID, message.GetUserId())
	require.Equal(t, "toolu_1", message.GetToolCallId())
	require.Empty(t, message.GetChatId())
	require.Empty(t, message.GetChatMessageId())
	require.Empty(t, message.GetRiskPolicyId(), "shared origin must not alter finding/reply policy correlation")
	_, err = time.Parse(time.RFC3339Nano, message.GetCreatedAt())
	require.NoError(t, err)

	require.Len(t, llmPub.attributes, 1)
	replyURN := llmPub.attributes[0][requestreply.ReplyURNAttribute]
	_, correlationID, err := ParseReplyURN(replyURN)
	require.NoError(t, err)
	parsedCorrelationID, err := uuid.Parse(correlationID)
	require.NoError(t, err)
	require.Equal(t, uuid.Version(7), parsedCorrelationID.Version())
	require.Equal(t, correlationID, reply.GetCorrelationId())
}

func TestDispatchLLMLaneFallsBackToOriginMessageFields(t *testing.T) {
	t.Parallel()

	te := setupInboxTest(t, "replica-dispatch-llm-fallback")
	llmPub := &captureLLMPublisher{messages: nil, attributes: nil, onPublish: nil}
	llmPub.onPublish = replyOK[*riskv1.LLMEnforcement](te, llmLane)
	dispatcher := testLLMDispatcher(te, llmPub, DispatcherConfig{WaitTimeout: time.Second, LaneWaitTimeout: nil, Flags: nil})

	origins := testOrigins(llmLane)
	origin := origins[llmLane]
	origin.ToolName = "origin-tool"
	origin.MessageType = "origin_message_type"
	origins[llmLane] = origin
	request := llmDispatchRequest([]Lane{llmLane}, origins)
	request.ToolName = ""
	request.MessageType = ""
	request.ToolCalls = nil

	outcome, err := dispatcher.Dispatch(t.Context(), request)
	require.NoError(t, err)
	require.True(t, outcome.Complete)
	require.Len(t, llmPub.messages, 1)
	message := llmPub.messages[0]
	require.Equal(t, "origin-tool", message.GetToolName())
	require.Equal(t, "origin_message_type", message.GetMessageType())
	require.Empty(t, message.GetToolCalls())
}

func TestDispatchFansOutGitleaksAndLLMLanes(t *testing.T) {
	t.Parallel()

	te := setupInboxTest(t, "replica-dispatch-gitleaks-llm")
	gitleaksPub := &captureEnforcementPublisher{messages: nil, attributes: nil, onPublish: nil}
	gitleaksPub.onPublish = replyOK[*riskv1.GitleaksEnforcement](te, gitleaksLane)
	presidioPub := &capturePresidioPublisher{messages: nil, attributes: nil, onPublish: nil}
	llmPub := &captureLLMPublisher{messages: nil, attributes: nil, onPublish: nil}
	llmPub.onPublish = replyOK[*riskv1.LLMEnforcement](te, llmLane)
	dispatcher := testDispatcherWithLanes(te, gitleaksPub, presidioPub, llmPub, DispatcherConfig{WaitTimeout: time.Second, LaneWaitTimeout: nil, Flags: nil})

	origins := testOrigins(gitleaksLane, llmLane)
	outcome, err := dispatcher.Dispatch(t.Context(), llmDispatchRequest([]Lane{gitleaksLane, llmLane}, origins))
	require.NoError(t, err)
	require.True(t, outcome.Complete)
	require.NotNil(t, outcome.ByLane[gitleaksLane])
	require.NotNil(t, outcome.ByLane[llmLane])
	require.Equal(t, riskv1.EnforcementScanner_ENFORCEMENT_SCANNER_GITLEAKS, outcome.ByLane[gitleaksLane].GetScanner())
	require.Equal(t, riskv1.EnforcementScanner_ENFORCEMENT_SCANNER_LLM_ANALYZER, outcome.ByLane[llmLane].GetScanner())
	require.Len(t, gitleaksPub.messages, 1)
	require.Len(t, llmPub.messages, 1)
	require.Empty(t, presidioPub.messages)
	// Both lanes share one request id but each gets its own correlation id.
	require.Equal(t, gitleaksPub.messages[0].GetRequestId(), llmPub.messages[0].GetRequestId())
	require.Equal(t, "raw scanned content", gitleaksPub.messages[0].GetContent())
	require.Equal(t, "raw scanned content", llmPub.messages[0].GetContent())
	_, gitleaksCorrelation, err := ParseReplyURN(gitleaksPub.attributes[0][requestreply.ReplyURNAttribute])
	require.NoError(t, err)
	_, llmCorrelation, err := ParseReplyURN(llmPub.attributes[0][requestreply.ReplyURNAttribute])
	require.NoError(t, err)
	require.NotEqual(t, gitleaksCorrelation, llmCorrelation)
}

func TestDispatchHonoursLaneWaitTimeoutOverride(t *testing.T) {
	t.Parallel()

	te := setupInboxTest(t, "replica-dispatch-lane-timeout")
	gitleaksPub := &captureEnforcementPublisher{messages: nil, attributes: nil, onPublish: nil}
	gitleaksPub.onPublish = replyOK[*riskv1.GitleaksEnforcement](te, gitleaksLane)
	presidioPub := &capturePresidioPublisher{messages: nil, attributes: nil, onPublish: nil}
	// The LLM publisher never replies, so only the lane override bounds its wait.
	llmPub := &captureLLMPublisher{messages: nil, attributes: nil, onPublish: nil}
	dispatcher := testDispatcherWithLanes(te, gitleaksPub, presidioPub, llmPub, DispatcherConfig{
		WaitTimeout: 10 * time.Second,
		LaneWaitTimeout: map[riskv1.EnforcementScanner]time.Duration{ //nolint:exhaustive // only the LLM lane is overridden
			riskv1.EnforcementScanner_ENFORCEMENT_SCANNER_LLM_ANALYZER: 25 * time.Millisecond,
		},
		Flags: nil,
	})

	origins := testOrigins(gitleaksLane, llmLane)
	started := time.Now()
	outcome, err := dispatcher.Dispatch(t.Context(), llmDispatchRequest([]Lane{gitleaksLane, llmLane}, origins))
	require.NoError(t, err)
	require.Less(t, time.Since(started), 5*time.Second, "LLM lane must time out on its override, not the default wait")
	require.False(t, outcome.Complete)
	require.True(t, outcome.Deadline)
	require.NotNil(t, outcome.ByLane[gitleaksLane])
	require.Nil(t, outcome.ByLane[llmLane])
	require.ErrorIs(t, outcome.Failed[llmLane], context.DeadlineExceeded)
	require.NotContains(t, outcome.Failed, gitleaksLane)
	require.Len(t, llmPub.messages, 1)
	require.Zero(t, te.inbox.Snapshot().Waiters)
}

func TestDispatchRejectsLLMLaneWithPolicyID(t *testing.T) {
	t.Parallel()

	te := setupInboxTest(t, "replica-dispatch-llm-policy")
	llmPub := &captureLLMPublisher{messages: nil, attributes: nil, onPublish: nil}
	dispatcher := testLLMDispatcher(te, llmPub, DispatcherConfig{WaitTimeout: time.Second, LaneWaitTimeout: nil, Flags: nil})

	policyLane := Lane{Scanner: riskv1.EnforcementScanner_ENFORCEMENT_SCANNER_LLM_ANALYZER, PolicyID: uuid.NewString()}
	_, err := dispatcher.Dispatch(t.Context(), llmDispatchRequest([]Lane{policyLane}, testOrigins(policyLane)))
	require.ErrorContains(t, err, "unsupported enforcement lane")
	require.Empty(t, llmPub.messages)
}

func TestDispatchHonorsFlagLimitForContentBodyAndToolCalls(t *testing.T) {
	t.Parallel()

	te := setupInboxTest(t, "replica-dispatch-request-limit")
	llmPub := &captureLLMPublisher{messages: nil, attributes: nil, onPublish: nil}
	llmPub.onPublish = replyOK[*riskv1.LLMEnforcement](te, llmLane)
	const limit = 2048
	dispatcher := testLLMDispatcher(te, llmPub, DispatcherConfig{WaitTimeout: time.Second, LaneWaitTimeout: nil, Flags: contentLimitFlags(t, limit)})

	expectedContent := strings.Repeat("c", limit)
	expectedBody := strings.Repeat("b", limit-1)
	request := llmDispatchRequest([]Lane{llmLane}, testOrigins(llmLane))
	request.Content = expectedContent + "tail"
	request.Body = expectedBody + "€tail"
	expectedArguments := strings.Repeat("a", limit)
	request.ToolCalls = []ToolCall{{ID: "toolu_1", Name: "bash", Arguments: expectedArguments + "tail"}}

	outcome, err := dispatcher.Dispatch(t.Context(), request)
	require.NoError(t, err)
	require.True(t, outcome.Complete)
	require.True(t, outcome.Truncated)
	require.Len(t, llmPub.messages, 1)
	message := llmPub.messages[0]
	require.Equal(t, expectedContent, message.GetContent())
	require.Equal(t, expectedBody, message.GetBody())
	require.True(t, utf8.ValidString(message.GetBody()))
	require.Len(t, message.GetToolCalls(), 1)
	require.Equal(t, expectedArguments, message.GetToolCalls()[0].GetArguments())
	require.True(t, message.GetContentTruncated())
	var metrics metricdata.ResourceMetrics
	require.NoError(t, te.reader.Collect(t.Context(), &metrics))
	require.Equal(t, int64(1), int64CounterValue(metrics, "risk.enforcement.truncations"))
}

func TestDispatchRejectsDuplicateLane(t *testing.T) {
	t.Parallel()

	te := setupInboxTest(t, "replica-dispatch-duplicate")
	publisher := &captureEnforcementPublisher{messages: nil, attributes: nil, onPublish: nil}
	dispatcher := testDispatcher(te, publisher, time.Second)

	_, err := dispatcher.Dispatch(t.Context(), DispatchRequest{
		OrganizationID: "org-duplicate",
		ProjectID:      "project-duplicate",
		Content:        "safe content",
		Lanes:          []Lane{gitleaksLane, gitleaksLane},
		Origins:        testOrigins(gitleaksLane, gitleaksLane),
	})
	require.ErrorContains(t, err, "duplicate enforcement lane")
	require.Empty(t, publisher.messages)
}
