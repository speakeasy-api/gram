package sigint_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/sigint"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestReadOnlyProjectGrantCannotMutateConfiguration(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	signal := createSignal(t, ctx, ti, "api")
	sensor := createSensor(t, ctx, ti, "services", "multi_label", signal.ID)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	readOnly := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeProjectRead, authCtx.ProjectID.String()))

	read, err := ti.service.GetSignal(readOnly, &gen.GetSignalPayload{
		ID: signal.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, signal.ID, read.ID)
	require.Equal(t, []string{signal.ID}, getSensor(t, readOnly, ti, sensor.ID).SignalIds)

	_, err = ti.service.CreateSignal(readOnly, &gen.CreateSignalPayload{
		Name: "forbidden", Description: nil, ClassifierCriteria: nil,
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.CreateSensor(readOnly, &gen.CreateSensorPayload{
		Name: "forbidden", Description: nil, Instructions: nil, Mode: "exclusive", SignalIds: nil,
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
	name := "forbidden"
	_, err = ti.service.UpdateSignal(readOnly, &gen.UpdateSignalPayload{
		ID: signal.ID, Name: &name, Description: nil, ClassifierCriteria: nil,
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.UpdateSensor(readOnly, &gen.UpdateSensorPayload{
		ID: sensor.ID, Name: &name, Description: nil, Instructions: nil, Mode: nil, SignalIds: nil,
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.DeleteSignal(readOnly, &gen.DeleteSignalPayload{
		ID: signal.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.DeleteSensor(readOnly, &gen.DeleteSensorPayload{
		ID: sensor.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)

	noGrants := authztest.WithExactGrants(t, ctx)
	_, err = ti.service.GetSignal(noGrants, &gen.GetSignalPayload{
		ID: signal.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.GetSensor(noGrants, &gen.GetSensorPayload{
		ID: sensor.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.ListSignals(noGrants, &gen.ListSignalsPayload{
		Cursor: nil, Limit: 2, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.ListSensors(noGrants, &gen.ListSensorsPayload{
		Cursor: nil, Limit: 2, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}
