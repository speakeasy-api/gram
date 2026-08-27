package temporalreply

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"google.golang.org/protobuf/proto"

	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/redisinbox"
	"github.com/speakeasy-api/gram/server/internal/requestreply"
)

const (
	temporalDestination = "temporal"
	cancelTimeout       = 5 * time.Second
)

// Config identifies the workflow task queue and return-address family used by
// a pair of request and reply brokers.
type Config struct {
	// TaskQueue is polled by the worker that registers Workflow.
	TaskQueue string

	// URNNamespace names the return-address family, for example "risk:enforce".
	URNNamespace string
}

// Requester starts a per-request workflow, publishes an addressed request, and
// waits for the workflow's first reply.
type Requester[Req requestreply.AddressedMessage, Resp proto.Message] struct {
	client            client.Client
	publisher         gcp.Publisher[Req]
	responsePrototype Resp
	config            Config
}

// Replier signals serialized replies to request workflows.
type Replier[Resp proto.Message] struct {
	client       client.Client
	urnNamespace string
}

// NewRequestBroker builds a Temporal request broker. responsePrototype must be
// a non-nil message of the reply type and is used only to allocate results.
func NewRequestBroker[Req requestreply.AddressedMessage, Resp proto.Message](
	temporalClient client.Client,
	publisher gcp.Publisher[Req],
	responsePrototype Resp,
	config Config,
) (*Requester[Req, Resp], error) {
	if temporalClient == nil {
		return nil, errors.New("temporal client is required")
	}
	if publisher == nil {
		return nil, errors.New("request publisher is required")
	}
	if any(responsePrototype) == nil || !responsePrototype.ProtoReflect().IsValid() {
		return nil, errors.New("response prototype is required")
	}
	if config.TaskQueue == "" {
		return nil, errors.New("temporal reply task queue is required")
	}
	if config.URNNamespace == "" {
		return nil, errors.New("temporal reply urn namespace is required")
	}

	return &Requester[Req, Resp]{
		client:            temporalClient,
		publisher:         publisher,
		responsePrototype: responsePrototype,
		config:            config,
	}, nil
}

// NewReplyBroker builds the responder side of a Temporal request-reply pair.
func NewReplyBroker[Resp proto.Message](temporalClient client.Client, urnNamespace string) (*Replier[Resp], error) {
	if temporalClient == nil {
		return nil, errors.New("temporal client is required")
	}
	if urnNamespace == "" {
		return nil, errors.New("temporal reply urn namespace is required")
	}
	return &Replier[Resp]{client: temporalClient, urnNamespace: urnNamespace}, nil
}

// Request starts the correlation workflow before publishing, then blocks on
// its result. The minted UUIDv7 is both the workflow ID and return-address ID.
func (r *Requester[Req, Resp]) Request(ctx context.Context, req Req) (Resp, error) {
	var zero Resp
	correlationID, err := uuid.NewV7()
	if err != nil {
		return zero, fmt.Errorf("mint request correlation id: %w", err)
	}

	run, err := r.client.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                    correlationID.String(),
		TaskQueue:             r.config.TaskQueue,
		WorkflowIDReusePolicy: enumspb.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE,
	}, WorkflowName)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return zero, ctxErr //nolint:wrapcheck // Request returns the context error as part of its contract.
		}
		return zero, fmt.Errorf("start reply workflow: %w", err)
	}

	req.SetReplyUrn(ReplyURN(r.config.URNNamespace, correlationID.String()))
	if _, err := r.publisher.Publish(ctx, req).Get(ctx); err != nil {
		r.cancelWorkflow(ctx, run)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return zero, ctxErr //nolint:wrapcheck // Request returns the context error as part of its contract.
		}
		return zero, fmt.Errorf("publish request: %w", err)
	}

	var payload []byte
	if err := run.Get(ctx, &payload); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			r.cancelWorkflow(ctx, run)
			return zero, ctxErr //nolint:wrapcheck // Request returns the context error as part of its contract.
		}
		return zero, fmt.Errorf("wait for reply workflow: %w", err)
	}

	reply, ok := r.responsePrototype.ProtoReflect().Type().New().Interface().(Resp)
	if !ok {
		return zero, fmt.Errorf("allocate response type %T", r.responsePrototype)
	}
	if err := proto.Unmarshal(payload, reply); err != nil {
		return zero, fmt.Errorf("unmarshal workflow reply: %w", err)
	}
	return reply, nil
}

func (r *Requester[Req, Resp]) cancelWorkflow(ctx context.Context, run client.WorkflowRun) {
	cancelCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cancelTimeout)
	defer cancel()
	_ = r.client.CancelWorkflow(cancelCtx, run.GetID(), run.GetRunID())
}

// Close flushes and stops the request publisher. The injected Temporal client
// remains owned by its caller.
func (r *Requester[Req, Resp]) Close(ctx context.Context) error {
	if err := r.publisher.Stop(ctx); err != nil {
		return fmt.Errorf("stop request publisher: %w", err)
	}
	return nil
}

// Reply marshals reply and signals the workflow addressed by to.
func (r *Replier[Resp]) Reply(ctx context.Context, to string, reply Resp) error {
	workflowID, err := ParseReplyURN(r.urnNamespace, to)
	if err != nil {
		return err
	}
	payload, err := proto.Marshal(reply)
	if err != nil {
		return fmt.Errorf("marshal reply: %w", err)
	}
	if err := r.client.SignalWorkflow(ctx, workflowID, "", ReplySignalName, payload); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("signal reply workflow: %w", ctxErr)
		}
		return fmt.Errorf("signal reply workflow: %w", err)
	}
	return nil
}

// ReplyURN formats a Temporal return address in the shared reply URN grammar.
func ReplyURN(namespace, correlationID string) string {
	return redisinbox.URN(namespace, temporalDestination, correlationID)
}

// ParseReplyURN extracts the workflow ID from a Temporal return address.
func ParseReplyURN(namespace, value string) (string, error) {
	destination, correlationID, err := redisinbox.ParseURN(namespace, value)
	if err != nil {
		return "", fmt.Errorf("parse temporal reply urn: %w", err)
	}
	if destination != temporalDestination {
		return "", fmt.Errorf("reply urn destination %q is not temporal", destination)
	}
	return correlationID, nil
}

var _ requestreply.RequestBroker[requestreply.AddressedMessage, proto.Message] = (*Requester[requestreply.AddressedMessage, proto.Message])(nil)
var _ requestreply.ReplyBroker[proto.Message] = (*Replier[proto.Message])(nil)
