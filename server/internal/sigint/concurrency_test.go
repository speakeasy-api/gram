package sigint_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/sigint"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestConcurrentAttachAndDeleteLeavesNoDanglingMembership(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	signal := createSignal(t, ctx, ti, "retiring")
	sensor := createSensor(t, ctx, ti, "services", "multi_label")
	start := make(chan struct{})
	updates := make(chan error, 8)
	deleted := make(chan error, 1)
	for range cap(updates) {
		go func() {
			<-start
			_, err := ti.service.UpdateSensor(ctx, &gen.UpdateSensorPayload{
				ID: sensor.ID, Name: nil, Description: nil, Instructions: nil, Mode: nil,
				SignalIds: []string{signal.ID}, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
			})
			updates <- err
		}()
	}
	go func() {
		<-start
		_, err := ti.service.DeleteSignal(ctx, &gen.DeleteSignalPayload{
			ID: signal.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
		})
		deleted <- err
	}()
	close(start)

	deleteErr := <-deleted
	var updateErrors [8]error
	for i := range updateErrors {
		updateErrors[i] = <-updates
	}
	require.NoError(t, deleteErr)
	for _, err := range updateErrors {
		if err != nil {
			requireOopsCode(t, err, oops.CodeNotFound)
		}
	}
	require.Empty(t, getSensor(t, ctx, ti, sensor.ID).SignalIds)
	_, err := ti.service.GetSignal(ctx, &gen.GetSignalPayload{
		ID: signal.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}
