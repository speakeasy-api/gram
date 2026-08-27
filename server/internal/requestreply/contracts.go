// Package requestreply defines transport-independent request-reply contracts.
package requestreply

import (
	"context"

	"google.golang.org/protobuf/proto"
)

// AddressedMessage is satisfied by generated request protos that declare a
// reply_urn field using the opaque API.
type AddressedMessage interface {
	proto.Message
	GetReplyUrn() string
	SetReplyUrn(string)
}

// RequestBroker publishes requests and waits for their correlated replies.
type RequestBroker[Req AddressedMessage, Resp proto.Message] interface {
	// Request mints a correlation id, stamps the reply address, publishes req,
	// and blocks until its reply arrives or ctx ends. A context deadline returns
	// the zero Resp and the context error.
	Request(ctx context.Context, req Req) (Resp, error)
}

// ReplyBroker sends a reply to a request's return address.
type ReplyBroker[Resp proto.Message] interface {
	// Reply answers the request that carried to as its return address.
	Reply(ctx context.Context, to string, reply Resp) error
}
