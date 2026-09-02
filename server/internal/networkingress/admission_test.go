package networkingress_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/network_ingress"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/networkaccess"
	"github.com/speakeasy-api/gram/server/internal/networkingress"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/productfeatures/productfeaturestest"
)

type errorFlagProvider struct{ *feature.InMemory }

func (*errorFlagProvider) EvaluateFlag(context.Context, feature.Flag, string, map[string]string) (feature.Evaluation, error) {
	return feature.EvaluationIndeterminate, errors.New("flag service unavailable")
}

func TestExpansionAdmissionDeniesIndeterminateAndErrors(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	missing := networkingress.NewExpansionAdmission(ti.features, &feature.InMemory{}, orgrepo.New(ti.conn), true)
	require.Error(t, missing.CheckExpansion(ctx, ti.orgID))

	failing := networkingress.NewExpansionAdmission(ti.features, &errorFlagProvider{InMemory: &feature.InMemory{}}, orgrepo.New(ti.conn), true)
	require.Error(t, failing.CheckExpansion(ctx, ti.orgID))
}

func TestNetworkModeAdmissionRequiresEnabledIngress(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	admission := networkingress.NewExpansionAdmission(ti.features, ti.flags, orgrepo.New(ti.conn), true)
	finalize, err := admission.PrepareNetworkAccess(ctx, networkaccess.EligibilityInput{OrganizationID: ti.orgID, Mode: networkaccess.ModeDual})
	require.NoError(t, err)

	require.Error(t, finalizeNetworkAccessInTransaction(ctx, ti, finalize))
	ti.create(t, ctx)
	require.NoError(t, finalizeNetworkAccessInTransaction(ctx, ti, finalize))

	disabled := false
	_, err = ti.service.UpdateIngress(ctx, &gen.UpdateIngressPayload{Enabled: &disabled})
	require.NoError(t, err)
	finalize, err = admission.PrepareNetworkAccess(ctx, networkaccess.EligibilityInput{OrganizationID: ti.orgID, Mode: networkaccess.ModePrivateOnly})
	require.NoError(t, err)
	require.Error(t, finalizeNetworkAccessInTransaction(ctx, ti, finalize))
}

func TestNetworkModeAdmissionRequiresReconcilerReadiness(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ti.create(t, ctx)
	admission := networkingress.NewExpansionAdmission(ti.features, ti.flags, orgrepo.New(ti.conn), false)
	finalize, err := admission.PrepareNetworkAccess(ctx, networkaccess.EligibilityInput{OrganizationID: ti.orgID, Mode: networkaccess.ModeDual})
	require.Error(t, err)
	require.Error(t, finalize.Finalize(ctx, nil))
}

func TestNetworkModePublicRecoveryNeedsNoGates(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	productfeaturestest.Disable(t, ctx, ti.conn, ti.features, ti.orgID, productfeatures.FeatureNetworkIngress)
	ti.flags.SetFlag(feature.FlagNetworkIngressRollout, ti.orgID, false)
	admission := networkingress.NewExpansionAdmission(ti.features, ti.flags, orgrepo.New(ti.conn), false)
	finalize, err := admission.PrepareNetworkAccess(ctx, networkaccess.EligibilityInput{OrganizationID: ti.orgID, Mode: networkaccess.ModePublicOnly})
	require.NoError(t, err)
	require.NoError(t, finalize.Finalize(ctx, nil))
}

func finalizeNetworkAccessInTransaction(ctx context.Context, ti *testInstance, finalize networkaccess.AdmissionFinalizer) error {
	if err := pgx.BeginFunc(ctx, ti.conn, func(tx pgx.Tx) error {
		if err := finalize.Finalize(ctx, tx); err != nil {
			return fmt.Errorf("finalize network access admission: %w", err)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("run network access admission transaction: %w", err)
	}
	return nil
}
