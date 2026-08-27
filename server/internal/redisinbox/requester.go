package redisinbox

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"

	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/requestreply"
)

// Requester publishes addressed messages after installing their reply waiter.
type Requester[Req requestreply.AddressedMessage, Resp proto.Message] struct {
	inbox     *Inbox[Resp]
	publisher gcp.Publisher[Req]
}

// NewRequestBroker composes a publisher with a replica reply inbox.
func NewRequestBroker[Req requestreply.AddressedMessage, Resp proto.Message](inbox *Inbox[Resp], publisher gcp.Publisher[Req]) *Requester[Req, Resp] {
	return &Requester[Req, Resp]{inbox: inbox, publisher: publisher}
}

// Request registers before publishing, stamps a fresh UUIDv7 return address,
// and waits for the first correlated reply.
func (r *Requester[Req, Resp]) Request(ctx context.Context, req Req) (Resp, error) {
	var zero Resp
	correlationID, err := uuid.NewV7()
	if err != nil {
		return zero, fmt.Errorf("mint request correlation id: %w", err)
	}
	started := time.Now()
	w, release, err := r.inbox.Register(correlationID.String())
	if err != nil {
		return zero, fmt.Errorf("register request waiter: %w", err)
	}
	defer release()

	req.SetReplyUrn(r.inbox.URN(correlationID.String()))
	if _, err := r.publisher.Publish(ctx, req).Get(ctx); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return zero, ctxErr //nolint:wrapcheck // Request returns the context error as part of its contract.
		}
		return zero, fmt.Errorf("publish request: %w", err)
	}

	return r.inbox.AwaitRegistered(ctx, correlationID.String(), w, started)
}

// Close flushes and stops the request publisher.
func (r *Requester[Req, Resp]) Close(ctx context.Context) error {
	if err := r.publisher.Stop(ctx); err != nil {
		return fmt.Errorf("stop request publisher: %w", err)
	}
	return nil
}

var _ requestreply.RequestBroker[requestreply.AddressedMessage, proto.Message] = (*Requester[requestreply.AddressedMessage, proto.Message])(nil)
