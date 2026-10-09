package sigint_test

import (
	"strings"
	"testing"

	gen "github.com/speakeasy-api/gram/server/gen/sigint"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/stretchr/testify/require"
)

func TestSignalSlugDefaultsUpdatesAndReservesDeletedValues(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	signal := createSignal(t, ctx, ti, "Billing Questions")
	require.Equal(t, types.Slug("billing-questions"), signal.Slug)

	renamed, err := ti.service.UpdateSignal(ctx, &gen.UpdateSignalPayload{ID: signal.ID, Name: new("Invoices")})
	require.NoError(t, err)
	require.Equal(t, signal.Slug, renamed.Slug)

	updated, err := ti.service.UpdateSignal(ctx, &gen.UpdateSignalPayload{ID: signal.ID, Slug: new(types.Slug("invoice_questions"))})
	require.NoError(t, err)
	require.Equal(t, types.Slug("invoice_questions"), updated.Slug)

	_, err = ti.service.CreateSignal(ctx, &gen.CreateSignalPayload{Name: "Duplicate", Slug: new(updated.Slug)})
	requireOopsCode(t, err, oops.CodeConflict)

	_, err = ti.service.DeleteSignal(ctx, &gen.DeleteSignalPayload{ID: signal.ID})
	require.NoError(t, err)

	_, err = ti.service.CreateSignal(ctx, &gen.CreateSignalPayload{Name: "Deleted duplicate", Slug: new(updated.Slug)})
	requireOopsCode(t, err, oops.CodeConflict)
}

func TestSensorSlugDefaultsUpdatesAndRejectsConflicts(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	sensor := createSensor(t, ctx, ti, "Customer Intent", "multi_label")
	require.Equal(t, types.Slug("customer-intent"), sensor.Slug)

	renamed, err := ti.service.UpdateSensor(ctx, &gen.UpdateSensorPayload{ID: sensor.ID, Name: new("Intent")})
	require.NoError(t, err)
	require.Equal(t, sensor.Slug, renamed.Slug)

	updated, err := ti.service.UpdateSensor(ctx, &gen.UpdateSensorPayload{ID: sensor.ID, Slug: new(types.Slug("intent"))})
	require.NoError(t, err)
	require.Equal(t, types.Slug("intent"), updated.Slug)

	other := createSensor(t, ctx, ti, "Other", "multi_label")

	_, err = ti.service.UpdateSensor(ctx, &gen.UpdateSensorPayload{ID: other.ID, Slug: new(updated.Slug)})
	requireOopsCode(t, err, oops.CodeConflict)

	_, err = ti.service.DeleteSensor(ctx, &gen.DeleteSensorPayload{ID: sensor.ID})
	require.NoError(t, err)

	_, err = ti.service.CreateSensor(ctx, &gen.CreateSensorPayload{Name: "Duplicate", Mode: "multi_label", Slug: new(updated.Slug)})
	requireOopsCode(t, err, oops.CodeConflict)
}

func TestSlugValidationAndNormalization(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	signal, err := ti.service.CreateSignal(ctx, &gen.CreateSignalPayload{Name: "Billing", Slug: new(types.Slug(" BILLING_QUESTION "))})
	require.NoError(t, err)
	require.Equal(t, types.Slug("billing_question"), signal.Slug)

	_, err = ti.service.CreateSignal(ctx, &gen.CreateSignalPayload{Name: "Invalid", Slug: new(types.Slug("not/valid"))})
	requireOopsCode(t, err, oops.CodeBadRequest)

	_, err = ti.service.CreateSensor(ctx, &gen.CreateSensorPayload{Name: "Invalid", Mode: "multi_label", Slug: new(types.Slug(strings.Repeat("x", 41)))})
	requireOopsCode(t, err, oops.CodeBadRequest)

	_, err = ti.service.CreateSignal(ctx, &gen.CreateSignalPayload{Name: "!!!"})
	requireOopsCode(t, err, oops.CodeBadRequest)

	long := createSignal(t, ctx, ti, strings.Repeat("a", 100))
	require.Len(t, long.Slug, 40)
}
