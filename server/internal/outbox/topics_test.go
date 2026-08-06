package outbox

import (
	"testing"

	"github.com/stretchr/testify/require"

	webhooksv1 "github.com/speakeasy-api/gram/infra/gen/gram/webhooks/v1"
)

// TestRegisterTopic_RejectsUnusableMessages pins where the failure happens. An
// unusable registration is silently accepted by the map — proto.MessageName
// returns "" for a nil rather than erroring — and only becomes visible when
// AssertResolvable hands the entry to the Pub/Sub broker, which dereferences
// it. Rejecting at the call site keeps the panic pointed at the mistake.
func TestRegisterTopic_RejectsUnusableMessages(t *testing.T) {
	t.Parallel()

	t.Run("nil message", func(t *testing.T) {
		t.Parallel()

		require.PanicsWithValue(t, "outbox.RegisterTopic: message is nil", func() {
			RegisterTopic(nil)
		})
	})

	t.Run("typed nil", func(t *testing.T) {
		t.Parallel()

		// This one resolves its descriptor, so it registers under the correct
		// name and would not crash the startup check — but Publish rejects a
		// typed nil by the same IsValid test, so it could never be published.
		var msg *webhooksv1.Event
		require.Panics(t, func() {
			RegisterTopic(msg)
		})
	})
}

// TestRegisterTopic_LeavesRegistryUsable is the consequence that motivates the
// guard: nothing unresolvable may reach ProtobufType, since the startup check
// passes whatever it finds straight to the broker.
func TestRegisterTopic_LeavesRegistryUsable(t *testing.T) {
	t.Parallel()

	for _, name := range RegisteredTopics() {
		require.NotEmpty(t, name, "the empty name is what a nil registration would leave behind")

		msg, ok := ProtobufType(name)
		require.True(t, ok)
		require.NotNil(t, msg)
		require.True(t, msg.ProtoReflect().IsValid(),
			"AssertResolvable dereferences this via PublisherForMessage")
	}
}
