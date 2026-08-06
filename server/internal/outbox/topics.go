package outbox

import (
	"fmt"
	"sync"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// Topic registry.
//
// A publish_outbox row names its destination by proto full name, and the relay
// resolves that name back to a message type in order to find the topic. The
// obvious mechanism, protoregistry.GlobalTypes, is deliberately not used: it
// only knows the types linked into the running binary, and nothing forces the
// worker to link a topic that only the API server writes. A missing link would
// then surface as every row for that topic failing to resolve at publish time,
// once per row, forever.
//
// Registering topics explicitly moves that failure to startup — see
// AssertResolvable — and gives a grep-able list of what may legally be written
// to the outbox.
var (
	topicsMu sync.RWMutex
	topics   = map[protoreflect.FullName]proto.Message{}
)

// RegisterTopic makes msg's type resolvable by the relay. Call it from an init
// function in the package that owns the topic, passing a zero value: only the
// descriptor is read.
//
// It panics rather than returning an error, because the only callers are init
// functions holding a compile-time literal — a bad argument there is a
// programming mistake, not a runtime condition, and an error return would just
// make every caller panic anyway.
//
// The guard matters because an unusable entry does not fail where it was made.
// proto.MessageName(nil) is "", not an error, so a nil registration lands in the
// map under the empty name and AssertResolvable then hands that nil to
// PublisherForMessage, which dereferences it — a nil pointer panic inside the
// Pub/Sub broker, several frames from the call that caused it.
func RegisterTopic(msg proto.Message) {
	if msg == nil {
		panic("outbox.RegisterTopic: message is nil")
	}
	if !msg.ProtoReflect().IsValid() {
		// A typed nil resolves its descriptor and so registers under the right
		// name, but it is not the zero value this wants: Publish rejects one by
		// the same test, so accepting it here would register a prototype that
		// could never legally be published.
		panic(fmt.Sprintf("outbox.RegisterTopic: %T is a typed nil, want a zero value", msg))
	}

	name := proto.MessageName(msg)
	if name == "" {
		panic(fmt.Sprintf("outbox.RegisterTopic: %T has no message name", msg))
	}

	topicsMu.Lock()
	defer topicsMu.Unlock()

	topics[name] = msg
}

// ProtobufType returns a zero-valued message for a registered topic name. It
// satisfies gcp.TopicResolver.
func ProtobufType(name protoreflect.FullName) (proto.Message, bool) {
	topicsMu.RLock()
	defer topicsMu.RUnlock()

	msg, ok := topics[name]

	return msg, ok
}

// RegisteredTopics returns every registered topic name.
func RegisteredTopics() []protoreflect.FullName {
	topicsMu.RLock()
	defer topicsMu.RUnlock()

	names := make([]protoreflect.FullName, 0, len(topics))
	for name := range topics {
		names = append(names, name)
	}

	return names
}

// AssertResolvable checks that every registered topic can be turned into a
// publisher, and is meant to run at process startup in any binary that drains
// the outbox. Deferring the check to publish time would turn a build or wiring
// mistake into a silent stream of dead-lettered rows.
func AssertResolvable(resolve func(protoreflect.FullName) error) error {
	for _, name := range RegisteredTopics() {
		if err := resolve(name); err != nil {
			return fmt.Errorf("outbox topic %s is not publishable: %w", name, err)
		}
	}

	return nil
}
