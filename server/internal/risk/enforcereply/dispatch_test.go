package enforcereply

import (
	"context"
	"errors"
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
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
)

func testDispatcher(inbox *Inbox, publisher *captureEnforcementPublisher, waitTimeout time.Duration) *Dispatcher {
	return testDispatcherWithPresidio(inbox, publisher, &capturePresidioPublisher{messages: nil, attributes: nil, onPublish: nil}, waitTimeout)
}

func testDispatcherWithPresidio(inbox *Inbox, gitleaksPub *captureEnforcementPublisher, presidioPub *capturePresidioPublisher, waitTimeout time.Duration) *Dispatcher {
	gitleaksReq := redisinbox.NewRequestBroker(inbox, gitleaksPub)
	presidioReq := redisinbox.NewRequestBroker(inbox, presidioPub)
	return &Dispatcher{
		gitleaks: &typedEnforcementLane[*riskv1.GitleaksEnforcement]{broker: gitleaksReq},
		presidio: &typedEnforcementLane[*riskv1.PresidioEnforcement]{broker: presidioReq},
		close: func(ctx context.Context) error {
			return errors.Join(gitleaksReq.Close(ctx), presidioReq.Close(ctx))
		},
		waitTimeout: waitTimeout,
		stokenCodec: stokens.NewCodec(),
	}
}
func testOrigins(lanes ...Lane) map[Lane]metering.RiskProvenance {
	origins := make(map[Lane]metering.RiskProvenance, len(lanes))
	operationID := uuid.NewString()
	for _, lane := range lanes {
		origins[lane] = metering.RiskProvenance{
			OrganizationID:    "org",
			ProjectID:         uuid.New(),
			RiskPolicyID:      uuid.New(),
			RiskPolicyVersion: 1,
			PolicyLinkReason:  "",
			ChatID:            uuid.Nil,
			ChatMessageID:     uuid.Nil,
			ContentPartID:     uuid.Nil,
			MessageLinkReason: "realtime_not_persisted",
			OperationID:       operationID,
			ExecutionPath:     "realtime_streams",
			RequestID:         "",
			MessageType:       "user_message",
			HookSource:        "test",
			UserID:            "user",
			ToolCallID:        "",
			ToolName:          "",
			Model:             "",
			Provider:          "",
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
	dispatcher := testDispatcher(te.inbox, publisher, time.Second)

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
	require.NotNil(t, outcome.ByLane[gitleaksLane])
	require.Len(t, publisher.messages, 1)
	message := publisher.messages[0]
	require.Equal(t, "org-dispatch", message.GetOrganizationId())
	require.Equal(t, "project-dispatch", message.GetProjectId())
	require.Equal(t, "safe content", message.GetContent())
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
	dispatcher := testDispatcher(te.inbox, publisher, time.Second)
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
	dispatcher := testDispatcherWithPresidio(te.inbox, gitleaksPub, presidioPub, time.Second)

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
	dispatcher := testDispatcherWithPresidio(te.inbox, gitleaksPub, presidioPub, time.Second)

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
	dispatcher := testDispatcher(te.inbox, publisher, 25*time.Millisecond)

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

func TestDispatchRejectsOversizedContent(t *testing.T) {
	t.Parallel()

	te := setupInboxTest(t, "replica-dispatch-oversized")
	publisher := &captureEnforcementPublisher{messages: nil, attributes: nil, onPublish: nil}
	dispatcher := testDispatcher(te.inbox, publisher, time.Second)

	_, err := dispatcher.Dispatch(t.Context(), DispatchRequest{
		OrganizationID: "org-oversized",
		ProjectID:      "project-oversized",
		Content:        strings.Repeat("x", MaxContentBytes+1),
		Lanes:          []Lane{gitleaksLane},
		Origins:        testOrigins(gitleaksLane),
	})
	require.ErrorContains(t, err, "maximum is 51200 bytes")
	require.Empty(t, publisher.messages)
}

func TestDispatchRejectsDuplicateLane(t *testing.T) {
	t.Parallel()

	te := setupInboxTest(t, "replica-dispatch-duplicate")
	publisher := &captureEnforcementPublisher{messages: nil, attributes: nil, onPublish: nil}
	dispatcher := testDispatcher(te.inbox, publisher, time.Second)

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
