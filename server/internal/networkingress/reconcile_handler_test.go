package networkingress_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	networkingressv1 "github.com/speakeasy-api/gram/infra/gen/gram/networkingress/v1"
	"github.com/speakeasy-api/gram/server/internal/networkingress"
	"github.com/speakeasy-api/gram/server/internal/streams"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

func TestNetworkIngressHandlerCoalescesAndRejectsOtherQueue(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	calls := 0
	handler := networkingress.NewReconcileHandler(testenv.NewLogger(t), "authoritative", func(_ context.Context, actual uuid.UUID) error {
		require.Equal(t, id, actual)
		calls++
		return nil
	})
	valid := networkingressv1.ReconcileRequested_builder{IngressId: new(id.String()), TemporalTaskQueue: new("authoritative")}.Build()
	other := networkingressv1.ReconcileRequested_builder{IngressId: new(uuid.NewString()), TemporalTaskQueue: new("preview")}.Build()
	bad := networkingressv1.ReconcileRequested_builder{IngressId: new("invalid"), TemporalTaskQueue: new("authoritative")}.Build()
	var messages []streams.BatchMessage[*networkingressv1.ReconcileRequested]
	for _, request := range []*networkingressv1.ReconcileRequested{valid, valid, other, bad} {
		var message streams.BatchMessage[*networkingressv1.ReconcileRequested]
		message.Message = request
		messages = append(messages, message)
	}
	require.NoError(t, handler.HandleBatchWithResult(t.Context(), messages))
	require.Equal(t, 1, calls)
}
