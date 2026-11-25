// Package requestreply defines transport-independent request-reply contracts.
package requestreply

import (
	"context"

	"google.golang.org/protobuf/proto"
)

// ReplyURNAttribute carries the request's transport-level return address.
const ReplyURNAttribute = "gram-reply-urn"

// RequestBroker publishes requests and waits for their correlated replies.
type RequestBroker[Req proto.Message, Resp proto.Message] interface {
	// Request mints a correlation id, publishes req with its reply address in
	// transport metadata, and blocks until its reply arrives or ctx ends. A
	// context deadline returns the zero Resp and the context error.
	Request(ctx context.Context, req Req) (Resp, error)
}

// ReplyBroker sends a reply to a request's return address.
type ReplyBroker[Resp proto.Message] interface {
	// Reply answers the request that carried to as its return address.
	Reply(ctx context.Context, to string, reply Resp) error
}
