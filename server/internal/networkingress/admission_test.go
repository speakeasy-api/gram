package networkingress_test

import (
	"context"
	"errors"
	"testing"

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

	missing := networkingress.NewExpansionAdmission(ti.features, &feature.InMemory{}, orgrepo.New(ti.conn), networkingress.NewRepositoryActiveIngressChecker(ti.conn))
	require.Error(t, missing.CheckExpansion(ctx, ti.orgID))

	failing := networkingress.NewExpansionAdmission(ti.features, &errorFlagProvider{InMemory: &feature.InMemory{}}, orgrepo.New(ti.conn), networkingress.NewRepositoryActiveIngressChecker(ti.conn))
	require.Error(t, failing.CheckExpansion(ctx, ti.orgID))
}

func TestNetworkModeAdmissionRequiresEnabledIngress(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	admission := networkingress.NewExpansionAdmission(ti.features, ti.flags, orgrepo.New(ti.conn), networkingress.NewRepositoryActiveIngressChecker(ti.conn))

	require.Error(t, admission.CheckNetworkAccess(ctx, networkaccess.EligibilityInput{OrganizationID: ti.orgID, Mode: networkaccess.ModeDual}))
	ti.create(t, ctx)
	require.NoError(t, admission.CheckNetworkAccess(ctx, networkaccess.EligibilityInput{OrganizationID: ti.orgID, Mode: networkaccess.ModeDual}))

	disabled := false
	_, err := ti.service.UpdateIngress(ctx, &gen.UpdateIngressPayload{Enabled: &disabled})
	require.NoError(t, err)
	require.Error(t, admission.CheckNetworkAccess(ctx, networkaccess.EligibilityInput{OrganizationID: ti.orgID, Mode: networkaccess.ModePrivateOnly}))
}

func TestNetworkModePublicRecoveryNeedsNoGates(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	productfeaturestest.Disable(t, ctx, ti.conn, ti.features, ti.orgID, productfeatures.FeatureNetworkIngress)
	ti.flags.SetFlag(feature.FlagNetworkIngressRollout, ti.orgID, false)
	admission := networkingress.NewExpansionAdmission(ti.features, ti.flags, orgrepo.New(ti.conn), networkingress.NewRepositoryActiveIngressChecker(ti.conn))
	require.NoError(t, admission.CheckNetworkAccess(ctx, networkaccess.EligibilityInput{OrganizationID: ti.orgID, Mode: networkaccess.ModePublicOnly}))
}
