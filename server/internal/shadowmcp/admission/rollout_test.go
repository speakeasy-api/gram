package admission

import (
	"context"
	"errors"
	"maps"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/feature"
)

const (
	testOrganizationID = "organization-id"
	testOrgSlug        = "organization-slug"
	testProjectSlug    = "project-slug"
)

func TestResolveRolloutClosedDecisions(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name        string
		modeEnabled bool
		payload     []byte
		killEnabled bool
		want        RolloutConfig
	}{
		{
			name:        "explicitly disabled mode preserves legacy with inactive kill switch",
			modeEnabled: false,
			killEnabled: false,
			want:        RolloutConfig{Mode: ModeLegacy, DirectRemoteDistributionDisabled: false},
		},
		{
			name:        "explicitly disabled mode preserves legacy with active kill switch",
			modeEnabled: false,
			killEnabled: true,
			want:        RolloutConfig{Mode: ModeLegacy, DirectRemoteDistributionDisabled: true},
		},
		{
			name:        "enabled mode selects report from its closed payload",
			modeEnabled: true,
			payload:     []byte(`{"mode":"report"}`),
			killEnabled: false,
			want:        RolloutConfig{Mode: ModeReport, DirectRemoteDistributionDisabled: false},
		},
		{
			name:        "enabled mode selects enforce from its closed payload",
			modeEnabled: true,
			payload:     []byte(`{"mode":"enforce"}`),
			killEnabled: true,
			want:        RolloutConfig{Mode: ModeEnforce, DirectRemoteDistributionDisabled: true},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			provider := &feature.InMemory{}
			provider.SetFlag(feature.FlagPlatformMCPShadowAudienceEnforcement, testOrganizationID, test.modeEnabled)
			provider.SetFlag(feature.FlagPlatformMCPDirectRemoteDistributionDisabled, testOrganizationID, test.killEnabled)
			if test.payload != nil {
				provider.SetFlagPayload(feature.FlagPlatformMCPShadowAudienceEnforcement, testOrganizationID, test.payload)
			}

			got, err := ResolveRollout(t.Context(), provider, testOrganizationID, testOrgSlug, testProjectSlug)

			require.NoError(t, err)
			require.Equal(t, test.want, got)
		})
	}
}

func TestResolveRolloutRejectsUnavailableOrOpenModeDecisions(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name     string
		provider feature.Provider
		setup    func(*feature.InMemory)
	}{
		{name: "nil provider", provider: nil},
		{name: "indeterminate mode", setup: func(*feature.InMemory) {}},
		{name: "enabled mode without payload", setup: func(provider *feature.InMemory) {
			provider.SetFlag(feature.FlagPlatformMCPShadowAudienceEnforcement, testOrganizationID, true)
		}},
		{name: "enabled mode with malformed payload", setup: func(provider *feature.InMemory) {
			provider.SetFlag(feature.FlagPlatformMCPShadowAudienceEnforcement, testOrganizationID, true)
			provider.SetFlagPayload(feature.FlagPlatformMCPShadowAudienceEnforcement, testOrganizationID, []byte(`not-json`))
		}},
		{name: "enabled mode with missing mode", setup: func(provider *feature.InMemory) {
			provider.SetFlag(feature.FlagPlatformMCPShadowAudienceEnforcement, testOrganizationID, true)
			provider.SetFlagPayload(feature.FlagPlatformMCPShadowAudienceEnforcement, testOrganizationID, []byte(`{}`))
		}},
		{name: "enabled mode cannot select legacy", setup: func(provider *feature.InMemory) {
			provider.SetFlag(feature.FlagPlatformMCPShadowAudienceEnforcement, testOrganizationID, true)
			provider.SetFlagPayload(feature.FlagPlatformMCPShadowAudienceEnforcement, testOrganizationID, []byte(`{"mode":"legacy"}`))
		}},
		{name: "enabled mode rejects unknown selection", setup: func(provider *feature.InMemory) {
			provider.SetFlag(feature.FlagPlatformMCPShadowAudienceEnforcement, testOrganizationID, true)
			provider.SetFlagPayload(feature.FlagPlatformMCPShadowAudienceEnforcement, testOrganizationID, []byte(`{"mode":"open"}`))
		}},
		{name: "enabled mode rejects unknown payload fields", setup: func(provider *feature.InMemory) {
			provider.SetFlag(feature.FlagPlatformMCPShadowAudienceEnforcement, testOrganizationID, true)
			provider.SetFlagPayload(feature.FlagPlatformMCPShadowAudienceEnforcement, testOrganizationID, []byte(`{"mode":"report","extra":true}`))
		}},
		{name: "enabled mode rejects multiple JSON values", setup: func(provider *feature.InMemory) {
			provider.SetFlag(feature.FlagPlatformMCPShadowAudienceEnforcement, testOrganizationID, true)
			provider.SetFlagPayload(feature.FlagPlatformMCPShadowAudienceEnforcement, testOrganizationID, []byte(`{"mode":"report"} {}`))
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			provider := test.provider
			if test.setup != nil {
				inMemory := &feature.InMemory{}
				test.setup(inMemory)
				inMemory.SetFlag(feature.FlagPlatformMCPDirectRemoteDistributionDisabled, testOrganizationID, false)
				provider = inMemory
			}

			got, err := ResolveRollout(t.Context(), provider, testOrganizationID, testOrgSlug, testProjectSlug)

			require.Equal(t, RolloutConfig{}, got)
			var unavailable *RolloutUnavailableError
			require.ErrorAs(t, err, &unavailable)
			require.Equal(t, feature.FlagPlatformMCPShadowAudienceEnforcement, unavailable.Flag)
		})
	}
}

func TestResolveRolloutRequiresAuthoritativeKillSwitchDecision(t *testing.T) {
	t.Parallel()

	provider := &feature.InMemory{}
	provider.SetFlag(feature.FlagPlatformMCPShadowAudienceEnforcement, testOrganizationID, false)

	got, err := ResolveRollout(t.Context(), provider, testOrganizationID, testOrgSlug, testProjectSlug)

	require.Equal(t, RolloutConfig{}, got)
	var unavailable *RolloutUnavailableError
	require.ErrorAs(t, err, &unavailable)
	require.Equal(t, feature.FlagPlatformMCPDirectRemoteDistributionDisabled, unavailable.Flag)
}

func TestResolveRolloutUsesOrganizationIdentityAndProjectGroups(t *testing.T) {
	t.Parallel()

	provider := &recordingRolloutProvider{
		evaluations: map[feature.Flag]feature.Evaluation{ //nolint:exhaustive // only queried rollout controls are relevant
			feature.FlagPlatformMCPShadowAudienceEnforcement:        feature.EvaluationEnabled,
			feature.FlagPlatformMCPDirectRemoteDistributionDisabled: feature.EvaluationDisabled,
		},
		payload: []byte(`{"mode":"report"}`),
	}

	got, err := ResolveRollout(t.Context(), provider, testOrganizationID, testOrgSlug, testProjectSlug)

	require.NoError(t, err)
	require.Equal(t, RolloutConfig{Mode: ModeReport}, got)
	require.Len(t, provider.calls, 3)
	for _, call := range provider.calls {
		require.Equal(t, testOrganizationID, call.distinctID)
		require.Equal(t, feature.OrgProjectGroups(testOrgSlug, testProjectSlug), call.groups)
	}
	require.Equal(t, feature.FlagPlatformMCPShadowAudienceEnforcement, provider.calls[0].flag)
	require.Equal(t, feature.FlagPlatformMCPShadowAudienceEnforcement, provider.calls[1].flag)
	require.True(t, provider.calls[1].payload)
	require.Equal(t, feature.FlagPlatformMCPDirectRemoteDistributionDisabled, provider.calls[2].flag)
}

func TestResolveRolloutRejectsIncompleteOrganizationTarget(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name           string
		organizationID string
		orgSlug        string
		projectSlug    string
	}{
		{name: "missing organization distinct ID", organizationID: "", orgSlug: testOrgSlug, projectSlug: testProjectSlug},
		{name: "missing organization group slug", organizationID: testOrganizationID, orgSlug: "", projectSlug: testProjectSlug},
		{name: "missing project group slug", organizationID: testOrganizationID, orgSlug: testOrgSlug, projectSlug: ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			provider := &recordingRolloutProvider{}
			got, err := ResolveRollout(t.Context(), provider, test.organizationID, test.orgSlug, test.projectSlug)

			require.Equal(t, RolloutConfig{}, got)
			var unavailable *RolloutUnavailableError
			require.ErrorAs(t, err, &unavailable)
			require.Equal(t, feature.FlagPlatformMCPShadowAudienceEnforcement, unavailable.Flag)
			require.Empty(t, provider.calls)
		})
	}
}

func TestResolveRolloutWrapsProviderFailuresAsUnavailable(t *testing.T) {
	t.Parallel()

	backendFailure := errors.New("feature provider unavailable")
	for _, test := range []struct {
		name      string
		provider  *failingRolloutProvider
		wantFlag  feature.Flag
		wantCalls int
	}{
		{
			name:      "mode evaluation failure",
			provider:  &failingRolloutProvider{failFlag: feature.FlagPlatformMCPShadowAudienceEnforcement, failure: backendFailure},
			wantFlag:  feature.FlagPlatformMCPShadowAudienceEnforcement,
			wantCalls: 1,
		},
		{
			name:      "mode payload failure",
			provider:  &failingRolloutProvider{payloadFailure: backendFailure},
			wantFlag:  feature.FlagPlatformMCPShadowAudienceEnforcement,
			wantCalls: 2,
		},
		{
			name:      "kill-switch evaluation failure",
			provider:  &failingRolloutProvider{failFlag: feature.FlagPlatformMCPDirectRemoteDistributionDisabled, failure: backendFailure},
			wantFlag:  feature.FlagPlatformMCPDirectRemoteDistributionDisabled,
			wantCalls: 3,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := ResolveRollout(t.Context(), test.provider, testOrganizationID, testOrgSlug, testProjectSlug)

			require.Equal(t, RolloutConfig{}, got)
			var unavailable *RolloutUnavailableError
			require.ErrorAs(t, err, &unavailable)
			require.Equal(t, test.wantFlag, unavailable.Flag)
			require.ErrorIs(t, err, backendFailure)
			require.Equal(t, test.wantCalls, test.provider.calls)
		})
	}
}

type rolloutProviderCall struct {
	flag       feature.Flag
	distinctID string
	groups     map[string]string
	payload    bool
}

type recordingRolloutProvider struct {
	evaluations map[feature.Flag]feature.Evaluation
	payload     []byte
	calls       []rolloutProviderCall
}

func (p *recordingRolloutProvider) IsFlagEnabled(context.Context, feature.Flag, string, map[string]string) (bool, error) {
	return false, nil
}

func (p *recordingRolloutProvider) IsFlagEnabledLocal(context.Context, feature.Flag, string, map[string]string, map[string]string) (bool, error) {
	return false, nil
}

func (p *recordingRolloutProvider) EvaluateFlag(_ context.Context, flag feature.Flag, distinctID string, groups map[string]string) (feature.Evaluation, error) {
	p.calls = append(p.calls, rolloutProviderCall{flag: flag, distinctID: distinctID, groups: cloneGroups(groups)})
	return p.evaluations[flag], nil
}

func (p *recordingRolloutProvider) FlagPayload(_ context.Context, flag feature.Flag, distinctID string, groups map[string]string) ([]byte, error) {
	p.calls = append(p.calls, rolloutProviderCall{flag: flag, distinctID: distinctID, groups: cloneGroups(groups), payload: true})
	return p.payload, nil
}

type failingRolloutProvider struct {
	failFlag       feature.Flag
	failure        error
	payloadFailure error
	calls          int
}

func (p *failingRolloutProvider) IsFlagEnabled(context.Context, feature.Flag, string, map[string]string) (bool, error) {
	return false, p.failure
}

func (p *failingRolloutProvider) IsFlagEnabledLocal(context.Context, feature.Flag, string, map[string]string, map[string]string) (bool, error) {
	return false, p.failure
}

func (p *failingRolloutProvider) EvaluateFlag(_ context.Context, flag feature.Flag, _ string, _ map[string]string) (feature.Evaluation, error) {
	p.calls++
	if flag == p.failFlag {
		return feature.EvaluationIndeterminate, p.failure
	}
	if flag == feature.FlagPlatformMCPShadowAudienceEnforcement {
		return feature.EvaluationEnabled, nil
	}
	return feature.EvaluationDisabled, nil
}

func (p *failingRolloutProvider) FlagPayload(context.Context, feature.Flag, string, map[string]string) ([]byte, error) {
	p.calls++
	if p.payloadFailure != nil {
		return nil, p.payloadFailure
	}
	return []byte(`{"mode":"enforce"}`), nil
}

func cloneGroups(groups map[string]string) map[string]string {
	cloned := make(map[string]string, len(groups))
	maps.Copy(cloned, groups)
	return cloned
}
