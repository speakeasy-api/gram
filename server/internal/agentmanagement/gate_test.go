package agentmanagement

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/server/gen/agents"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type staticSessionAuthorizer struct {
	authCtx *contextvalues.AuthContext
}

func (a staticSessionAuthorizer) AuthorizeWithPostAuthenticationCheck(
	ctx context.Context,
	_ string,
	_ *security.APIKeyScheme,
	check func(context.Context) error,
) (context.Context, error) {
	ctx = contextvalues.SetAuthContext(ctx, a.authCtx)
	return ctx, check(ctx)
}

type recordingAgentManagementFeatures struct {
	evaluation feature.Evaluation
	err        error
	flag       feature.Flag
	distinctID string
	groups     map[string]string
}

func (*recordingAgentManagementFeatures) IsFlagEnabled(context.Context, feature.Flag, string, map[string]string) (bool, error) {
	return false, nil
}

func (*recordingAgentManagementFeatures) IsFlagEnabledLocal(context.Context, feature.Flag, string, map[string]string, map[string]string) (bool, error) {
	return false, nil
}

func (*recordingAgentManagementFeatures) FlagPayload(context.Context, feature.Flag, string, map[string]string) ([]byte, error) {
	return nil, nil
}

func (f *recordingAgentManagementFeatures) EvaluateFlag(_ context.Context, flag feature.Flag, distinctID string, groups map[string]string) (feature.Evaluation, error) {
	f.flag = flag
	f.distinctID = distinctID
	f.groups = groups
	return f.evaluation, f.err
}

func TestAgentManagementRolloutGateRequiresAuthoritativeEnablement(t *testing.T) {
	t.Parallel()

	backendFailure := errors.New("feature provider unavailable")
	for _, test := range []struct {
		name       string
		features   feature.Provider
		wantErr    bool
		wantLookup bool
	}{
		{name: "enabled", features: &recordingAgentManagementFeatures{evaluation: feature.EvaluationEnabled}, wantLookup: true},
		{name: "disabled", features: &recordingAgentManagementFeatures{evaluation: feature.EvaluationDisabled}, wantErr: true, wantLookup: true},
		{name: "indeterminate", features: &recordingAgentManagementFeatures{evaluation: feature.EvaluationIndeterminate}, wantErr: true, wantLookup: true},
		{name: "provider error", features: &recordingAgentManagementFeatures{err: backendFailure}, wantErr: true, wantLookup: true},
		{name: "missing provider", wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			service := &Service{logger: testenv.NewLogger(t), features: test.features}
			ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
				ActiveOrganizationID: "organization",
				OrganizationSlug:     "organization-slug",
			})

			err := service.requireAgentManagementEnabled(ctx)
			if test.wantErr {
				requireOopsCode(t, err, oops.CodeNotFound)
			} else {
				require.NoError(t, err)
			}

			flags, ok := test.features.(*recordingAgentManagementFeatures)
			if !test.wantLookup {
				require.False(t, ok)
				return
			}
			require.True(t, ok)
			require.Equal(t, feature.FlagAgentManagement, flags.flag)
			require.Equal(t, "organization", flags.distinctID)
			require.Equal(t, feature.OrgProjectGroups("organization-slug", ""), flags.groups)
		})
	}
}

func TestGeneratedAgentManagementEndpointsCannotBypassRolloutGate(t *testing.T) {
	t.Parallel()

	backendFailure := errors.New("feature provider unavailable")
	for _, test := range []struct {
		name     string
		features feature.Provider
	}{
		{name: "disabled", features: &recordingAgentManagementFeatures{evaluation: feature.EvaluationDisabled}},
		{name: "indeterminate", features: &recordingAgentManagementFeatures{evaluation: feature.EvaluationIndeterminate}},
		{name: "provider error", features: &recordingAgentManagementFeatures{err: backendFailure}},
		{name: "missing provider"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			authCtx := &contextvalues.AuthContext{
				ActiveOrganizationID: "organization",
				OrganizationSlug:     "organization-slug",
			}
			service := &Service{
				logger:   testenv.NewLogger(t),
				auth:     staticSessionAuthorizer{authCtx: authCtx},
				features: test.features,
			}
			endpoint := gen.NewCreateEndpoint(service, service.APIKeyAuth)

			_, err := endpoint(t.Context(), &gen.CreatePayload{Name: "must not be created"})
			requireOopsCode(t, err, oops.CodeNotFound)
		})
	}
}

func TestAgentManagementRolloutGateRejectsMissingTenantContextBeforeEvaluation(t *testing.T) {
	t.Parallel()

	features := &recordingAgentManagementFeatures{evaluation: feature.EvaluationEnabled}
	service := &Service{logger: testenv.NewLogger(t), features: features}

	requireOopsCode(t, service.requireAgentManagementEnabled(t.Context()), oops.CodeNotFound)
	requireOopsCode(t, service.requireAgentManagementEnabled(contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{})), oops.CodeNotFound)
	require.Empty(t, features.flag)
}
