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

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/redisinbox"
	"github.com/speakeasy-api/gram/server/internal/requestreply"
)

type captureEnforcementPublisher struct {
	messages   []*riskv1.GitleaksEnforcement
	attributes []map[string]string
	onPublish  func(context.Context, *riskv1.GitleaksEnforcement, map[string]string) error
}

func (p *captureEnforcementPublisher) Publish(ctx context.Context, message *riskv1.GitleaksEnforcement, options ...gcp.PublishOption) gcp.PublishResult {
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

func (p *captureEnforcementPublisher) Stop(context.Context) error {
	return nil
}

type capturePresidioPublisher struct {
	messages  []*riskv1.PresidioEnforcement
	onPublish func(context.Context, *riskv1.PresidioEnforcement, map[string]string) error
}

func (p *capturePresidioPublisher) Publish(ctx context.Context, message *riskv1.PresidioEnforcement, options ...gcp.PublishOption) gcp.PublishResult {
	var opts gcp.PublishOptions
	for _, option := range options {
		option(&opts)
	}
	p.messages = append(p.messages, message)
	if p.onPublish != nil {
		if err := p.onPublish(ctx, message, maps.Clone(opts.Attributes)); err != nil {
			return gcp.NewErrPublishResult(err)
		}
	}
	return gcp.NewSuccessPublishResult()
}

func (p *capturePresidioPublisher) Stop(context.Context) error {
	return nil
}

func testDispatcher(inbox *Inbox, publisher *captureEnforcementPublisher, waitTimeout time.Duration) *Dispatcher {
	return testDispatcherWithPresidio(inbox, publisher, &capturePresidioPublisher{messages: nil, onPublish: nil}, waitTimeout)
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
	}
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
	requestID, err := uuid.Parse(message.GetRequestId())
	require.NoError(t, err)
	require.Equal(t, uuid.Version(7), requestID.Version())
	require.Len(t, publisher.attributes, 1)
	replyURN := publisher.attributes[0][requestreply.ReplyURNAttribute]
	_, correlationID, err := ParseReplyURN(replyURN)
	require.NoError(t, err)
	parsedCorrelationID, err := uuid.Parse(correlationID)
	require.NoError(t, err)
	require.Equal(t, uuid.Version(7), parsedCorrelationID.Version())
	require.NotEqual(t, message.GetRequestId(), correlationID)
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
	presidioPub := &capturePresidioPublisher{messages: nil, onPublish: nil}
	presidioPub.onPublish = func(ctx context.Context, _ *riskv1.PresidioEnforcement, attributes map[string]string) error {
		replyURN := attributes[requestreply.ReplyURNAttribute]
		_, correlationID, err := ParseReplyURN(replyURN)
		if err != nil {
			return err
		}
		return te.writer.Reply(ctx, replyURN, testReply(correlationID, presidioLane, riskv1.EnforcementStatus_ENFORCEMENT_STATUS_OK))
	}
	dispatcher := testDispatcherWithPresidio(te.inbox, gitleaksPub, presidioPub, time.Second)

	outcome, err := dispatcher.Dispatch(t.Context(), DispatchRequest{
		OrganizationID: "org-presidio",
		ProjectID:      "project-presidio",
		Content:        "safe content",
		Lanes:          []Lane{gitleaksLane, presidioLane},
	})
	require.NoError(t, err)
	require.True(t, outcome.Complete)
	require.NotNil(t, outcome.ByLane[gitleaksLane])
	require.NotNil(t, outcome.ByLane[presidioLane])
	require.Len(t, presidioPub.messages, 1)
	message := presidioPub.messages[0]
	require.Equal(t, "org-presidio", message.GetOrganizationId())
	require.Equal(t, "project-presidio", message.GetProjectId())
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
	})
	require.ErrorContains(t, err, "duplicate enforcement lane")
	require.Empty(t, publisher.messages)
}
