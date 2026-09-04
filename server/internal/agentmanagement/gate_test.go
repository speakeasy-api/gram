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

func (a staticSessionAuthorizer) Authorize(ctx context.Context, _ string, _ *security.APIKeyScheme) (context.Context, error) {
	return contextvalues.SetAuthContext(ctx, a.authCtx), nil
}

type recordingM1Features struct {
	evaluation feature.Evaluation
	err        error
	flag       feature.Flag
	distinctID string
	groups     map[string]string
}

func (*recordingM1Features) IsFlagEnabled(context.Context, feature.Flag, string, map[string]string) (bool, error) {
	return false, nil
}

func (*recordingM1Features) IsFlagEnabledLocal(context.Context, feature.Flag, string, map[string]string, map[string]string) (bool, error) {
	return false, nil
}

func (*recordingM1Features) FlagPayload(context.Context, feature.Flag, string, map[string]string) ([]byte, error) {
	return nil, nil
}

func (f *recordingM1Features) EvaluateFlag(_ context.Context, flag feature.Flag, distinctID string, groups map[string]string) (feature.Evaluation, error) {
	f.flag = flag
	f.distinctID = distinctID
	f.groups = groups
	return f.evaluation, f.err
}

func TestM1RolloutGateRequiresAuthoritativeEnablement(t *testing.T) {
	t.Parallel()

	backendFailure := errors.New("feature provider unavailable")
	for _, test := range []struct {
		name       string
		features   feature.Provider
		wantErr    bool
		wantLookup bool
	}{
		{name: "enabled", features: &recordingM1Features{evaluation: feature.EvaluationEnabled}, wantLookup: true},
		{name: "disabled", features: &recordingM1Features{evaluation: feature.EvaluationDisabled}, wantErr: true, wantLookup: true},
		{name: "indeterminate", features: &recordingM1Features{evaluation: feature.EvaluationIndeterminate}, wantErr: true, wantLookup: true},
		{name: "provider error", features: &recordingM1Features{err: backendFailure}, wantErr: true, wantLookup: true},
		{name: "missing provider", wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			service := &Service{logger: testenv.NewLogger(t), features: test.features}
			ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
				ActiveOrganizationID: "organization",
				OrganizationSlug:     "organization-slug",
			})

			err := service.requireM1Enabled(ctx)
			if test.wantErr {
				requireOopsCode(t, err, oops.CodeNotFound)
			} else {
				require.NoError(t, err)
			}

			flags, ok := test.features.(*recordingM1Features)
			if !test.wantLookup {
				require.False(t, ok)
				return
			}
			require.True(t, ok)
			require.Equal(t, feature.FlagAgentManagementM1, flags.flag)
			require.Equal(t, "organization", flags.distinctID)
			require.Equal(t, feature.OrgProjectGroups("organization-slug", ""), flags.groups)
		})
	}
}

func TestGeneratedM1EndpointsCannotBypassRolloutGate(t *testing.T) {
	t.Parallel()

	backendFailure := errors.New("feature provider unavailable")
	for _, test := range []struct {
		name     string
		features feature.Provider
	}{
		{name: "disabled", features: &recordingM1Features{evaluation: feature.EvaluationDisabled}},
		{name: "indeterminate", features: &recordingM1Features{evaluation: feature.EvaluationIndeterminate}},
		{name: "provider error", features: &recordingM1Features{err: backendFailure}},
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

func TestM1RolloutGateRejectsMissingTenantContextBeforeEvaluation(t *testing.T) {
	t.Parallel()

	features := &recordingM1Features{evaluation: feature.EvaluationEnabled}
	service := &Service{logger: testenv.NewLogger(t), features: features}

	requireOopsCode(t, service.requireM1Enabled(t.Context()), oops.CodeNotFound)
	requireOopsCode(t, service.requireM1Enabled(contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{})), oops.CodeNotFound)
	require.Empty(t, features.flag)
}
