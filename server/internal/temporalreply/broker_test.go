package temporalreply

import (
	"context"
	"maps"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/client"
	temporalmocks "go.temporal.io/sdk/mocks"
	"google.golang.org/protobuf/proto"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/requestreply"
)

const testURNNamespace = "test:reply"

type capturePublisher struct {
	message    *riskv1.GitleaksEnforcement
	attributes map[string]string
	result     gcp.PublishResult
}

func (p *capturePublisher) Publish(_ context.Context, message *riskv1.GitleaksEnforcement, options ...gcp.PublishOption) gcp.PublishResult {
	var opts gcp.PublishOptions
	for _, option := range options {
		option(&opts)
	}
	p.message = message
	p.attributes = maps.Clone(opts.Attributes)
	return p.result
}

func (p *capturePublisher) Stop(context.Context) error {
	return nil
}

func TestRequesterRoundTrip(t *testing.T) {
	t.Parallel()

	temporalClient := temporalmocks.NewClient(t)
	run := temporalmocks.NewWorkflowRun(t)
	publisher := &capturePublisher{message: nil, attributes: nil, result: gcp.NewSuccessPublishResult()}
	reply := &riskv1.EnforcementReply{}
	reply.SetReason("first")
	payload, err := proto.Marshal(reply)
	require.NoError(t, err)

	temporalClient.On(
		"ExecuteWorkflow",
		mock.Anything,
		mock.MatchedBy(func(options client.StartWorkflowOptions) bool {
			id, parseErr := uuid.Parse(options.ID)
			return parseErr == nil && id.Version() == uuid.Version(7) && options.TaskQueue == "test-replies"
		}),
		WorkflowName,
	).Return(run, nil)
	run.On("Get", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		out, ok := args.Get(1).(*[]byte)
		require.True(t, ok)
		*out = payload
	}).Return(nil)

	broker, err := NewRequestBroker(temporalClient, publisher, &riskv1.EnforcementReply{}, Config{
		TaskQueue:    "test-replies",
		URNNamespace: testURNNamespace,
	})
	require.NoError(t, err)
	request := &riskv1.GitleaksEnforcement{}
	got, err := broker.Request(t.Context(), request)
	require.NoError(t, err)
	require.Equal(t, "first", got.GetReason())
	require.Same(t, request, publisher.message)

	replyURN := publisher.attributes[requestreply.ReplyURNAttribute]
	require.NotEmpty(t, replyURN)
	workflowID, err := ParseReplyURN(testURNNamespace, replyURN)
	require.NoError(t, err)
	parsedID, err := uuid.Parse(workflowID)
	require.NoError(t, err)
	require.Equal(t, uuid.Version(7), parsedID.Version())
}

func TestRequesterDeadlineReturnsZeroResponseAndContextError(t *testing.T) {
	t.Parallel()

	temporalClient := temporalmocks.NewClient(t)
	run := temporalmocks.NewWorkflowRun(t)
	publisher := &capturePublisher{message: nil, attributes: nil, result: gcp.NewSuccessPublishResult()}
	temporalClient.On("ExecuteWorkflow", mock.Anything, mock.Anything, WorkflowName).Return(run, nil)
	run.On("Get", mock.Anything, mock.Anything).Return(func(ctx context.Context, _ any) error {
		return ctx.Err()
	})
	run.On("GetID").Return("workflow-id")
	run.On("GetRunID").Return("run-id")
	temporalClient.On("CancelWorkflow", mock.Anything, "workflow-id", "run-id").Return(nil)

	broker, err := NewRequestBroker(temporalClient, publisher, &riskv1.EnforcementReply{}, Config{
		TaskQueue:    "test-replies",
		URNNamespace: testURNNamespace,
	})
	require.NoError(t, err)
	ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer cancel()

	got, err := broker.Request(ctx, &riskv1.GitleaksEnforcement{})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Nil(t, got)
}

func TestReplierSignalsWorkflowFromURN(t *testing.T) {
	t.Parallel()

	temporalClient := temporalmocks.NewClient(t)
	reply := &riskv1.EnforcementReply{}
	reply.SetReason("answer")
	payload, err := proto.Marshal(reply)
	require.NoError(t, err)
	temporalClient.On("SignalWorkflow", mock.Anything, "request-id", "", ReplySignalName, payload).Return(nil)

	broker, err := NewReplyBroker[*riskv1.EnforcementReply](temporalClient, testURNNamespace)
	require.NoError(t, err)
	require.NoError(t, broker.Reply(t.Context(), ReplyURN(testURNNamespace, "request-id"), reply))
}

func TestReplyURNRejectsRedisDestination(t *testing.T) {
	t.Parallel()

	_, err := ParseReplyURN(testURNNamespace, "urn:gram:test:reply:replica-1:request-id")
	require.ErrorContains(t, err, "not temporal")
}
