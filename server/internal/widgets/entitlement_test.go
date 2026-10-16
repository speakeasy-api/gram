package widgets_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/widgets"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// sensorPayload asks the sensor_evaluations dataset, which needs the
// organization's signals intelligence feature.
func sensorPayload(name string) *gen.CreateWidgetPayload {
	query := map[string]any{"window": "7d", "grain": "day", "dimensions": []any{"sensor"}, "measures": []any{map[string]any{"op": "count"}}}
	return &gen.CreateWidgetPayload{Name: name, Description: nil, Dataset: "sensor_evaluations", Query: query, Visualization: barChart(), SessionToken: nil, ProjectSlugInput: nil}
}

func TestWidgetsOnAGatedDatasetFollowTheEntitlement(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.CreateWidget(ctx, sensorPayload("Evaluations by sensor"))
	requireOopsCode(t, err, oops.CodeBadRequest)
	require.ErrorContains(t, err, "unknown_dataset", "a dataset the organization is not entitled to is refused as unknown")

	ti.features.signalsIntelligence.Store(true)
	created, err := ti.service.CreateWidget(ctx, sensorPayload("Evaluations by sensor"))
	require.NoError(t, err)
	require.Nil(t, created.InvalidReason)

	ti.features.signalsIntelligence.Store(false)
	read, err := ti.service.GetWidget(ctx, &gen.GetWidgetPayload{ID: created.ID, SessionToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.NotNil(t, read.InvalidReason, "a saved widget whose dataset is no longer available reads as broken")
	require.Contains(t, *read.InvalidReason, "unknown_dataset")

	listed, err := ti.service.ListWidgets(ctx, &gen.ListWidgetsPayload{SessionToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Len(t, listed.Widgets, 1)
	require.NotNil(t, listed.Widgets[0].InvalidReason)

	_, err = ti.service.DuplicateWidget(ctx, &gen.DuplicateWidgetPayload{ID: created.ID, SessionToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeBadRequest)

	update := &gen.UpdateWidgetPayload{ID: created.ID, Name: "Renamed", Description: nil, Dataset: "sensor_evaluations", Query: sensorPayload("").Query, Visualization: barChart(), SessionToken: nil, ProjectSlugInput: nil}
	_, err = ti.service.UpdateWidget(ctx, update)
	requireOopsCode(t, err, oops.CodeBadRequest)

	ti.features.signalsIntelligence.Store(true)
	updated, err := ti.service.UpdateWidget(ctx, update)
	require.NoError(t, err)
	require.Equal(t, "Renamed", updated.Name)
}
