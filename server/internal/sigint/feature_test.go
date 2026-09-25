package sigint_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
)

func requireConfigurationRejected(t *testing.T, ctx context.Context, ti *testInstance, code oops.Code) {
	t.Helper()
	// Nil payloads also verify gating happens before resource validation/access.
	_, err := ti.service.CreateSignal(ctx, nil)
	requireOopsCode(t, err, code)
	_, err = ti.service.GetSignal(ctx, nil)
	requireOopsCode(t, err, code)
	_, err = ti.service.ListSignals(ctx, nil)
	requireOopsCode(t, err, code)
	_, err = ti.service.UpdateSignal(ctx, nil)
	requireOopsCode(t, err, code)
	_, err = ti.service.DeleteSignal(ctx, nil)
	requireOopsCode(t, err, code)
	_, err = ti.service.CreateSensor(ctx, nil)
	requireOopsCode(t, err, code)
	_, err = ti.service.GetSensor(ctx, nil)
	requireOopsCode(t, err, code)
	_, err = ti.service.ListSensors(ctx, nil)
	requireOopsCode(t, err, code)
	_, err = ti.service.UpdateSensor(ctx, nil)
	requireOopsCode(t, err, code)
	_, err = ti.service.DeleteSensor(ctx, nil)
	requireOopsCode(t, err, code)
}

func TestDisabledFeatureRejectsConfiguration(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NoError(t, ti.features.SetFeatureEnabled(ctx, authCtx.ActiveOrganizationID, productfeatures.FeatureSignalsIntelligence, false))
	requireConfigurationRejected(t, ctx, ti, oops.CodeForbidden)
}

func TestDisabledFeatureRejectsLegacyAPIKeys(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	keyAuth := *authCtx
	keyAuth.APIKeyID = "test-producer-key"
	keyAuth.SessionID = nil
	ctx = contextvalues.WithLegacyAPIKeyAuthorization(ctx, &keyAuth)
	// Enabled producer keys retain their existing project authorization behavior.
	createSignal(t, ctx, ti, "enabled key")
	require.NoError(t, ti.features.SetFeatureEnabled(ctx, authCtx.ActiveOrganizationID, productfeatures.FeatureSignalsIntelligence, false))
	requireConfigurationRejected(t, ctx, ti, oops.CodeForbidden)
}

func TestFeatureLookupFailureRejectsConfiguration(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	requireConfigurationRejected(t, canceled, ti, oops.CodeUnexpected)
}
