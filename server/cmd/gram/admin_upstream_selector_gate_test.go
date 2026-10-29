package gram

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
)

type selectorGateFlagResult struct {
	enabled bool
	err     error
}

// selectorGateFlags answers only local evaluations of the upstream selector
// write flag, keyed by organization.
type selectorGateFlags struct {
	results map[string]selectorGateFlagResult
}

func (f selectorGateFlags) IsFlagEnabled(context.Context, feature.Flag, string, map[string]string) (bool, error) {
	return false, errors.New("remote evaluation not expected")
}

func (f selectorGateFlags) IsFlagEnabledLocal(_ context.Context, flag feature.Flag, distinctID string, _, _ map[string]string) (bool, error) {
	if flag != feature.FlagAuthClientUpstreamSelectorWrites {
		return false, errors.New("unexpected flag " + string(flag))
	}
	result := f.results[distinctID]
	return result.enabled, result.err
}

func (f selectorGateFlags) FlagPayload(context.Context, feature.Flag, string, map[string]string) ([]byte, error) {
	return nil, nil
}

func adminCommandContext(t *testing.T, environment string) *cli.Context {
	t.Helper()
	set := flag.NewFlagSet("admin", flag.ContinueOnError)
	for _, f := range newAdminCommand().Flags {
		require.NoError(t, f.Apply(set))
	}
	require.NoError(t, set.Set("environment", environment))
	return cli.NewContext(cli.NewApp(), set, nil)
}

func TestAdminCommandAcceptsPostHogConfig(t *testing.T) {
	t.Parallel()
	want := map[string]string{
		"posthog-api-key":          "POSTHOG_API_KEY",
		"posthog-endpoint":         "POSTHOG_ENDPOINT",
		"posthog-personal-api-key": "POSTHOG_PERSONAL_API_KEY",
	}
	got := map[string]string{}
	for _, f := range newAdminCommand().Flags {
		sf, ok := f.(*cli.StringFlag)
		if !ok {
			continue
		}
		if _, wanted := want[sf.Name]; wanted && len(sf.EnvVars) > 0 {
			got[sf.Name] = sf.EnvVars[0]
		}
	}
	require.Equal(t, want, got)
}

func TestAdminUpstreamSelectorWriteGateUsesFeatureFlags(t *testing.T) {
	t.Parallel()
	c := adminCommandContext(t, "prod")
	flags := selectorGateFlags{results: map[string]selectorGateFlagResult{
		"org-enabled":  {enabled: true, err: nil},
		"org-disabled": {enabled: false, err: nil},
		"org-error":    {enabled: true, err: errors.New("flag definitions unavailable")},
	}}
	gate := newAdminUpstreamSelectorWriteGate(t.Context(), c, slog.New(slog.DiscardHandler), flags)
	require.NotNil(t, gate)
	ctx := authz.WithUpstreamSelectorWriteGate(t.Context(), gate)

	require.True(t, authz.UpstreamSelectorWritesAllowed(ctx, "org-enabled"))
	require.False(t, authz.UpstreamSelectorWritesAllowed(ctx, "org-disabled"))
	require.False(t, authz.UpstreamSelectorWritesAllowed(ctx, "org-error"))
	require.False(t, authz.UpstreamSelectorWritesAllowed(ctx, "org-unknown"))
	require.False(t, authz.UpstreamSelectorWritesAllowed(ctx, ""))
}

func TestAdminUpstreamSelectorWriteGateFailsClosedWithoutPostHog(t *testing.T) {
	t.Parallel()
	c := adminCommandContext(t, "prod")
	logger := slog.New(slog.DiscardHandler)
	posthogClient := posthog.New(t.Context(), logger, "", "", "")
	gate := newAdminUpstreamSelectorWriteGate(t.Context(), c, logger, posthogClient)
	ctx := authz.WithUpstreamSelectorWriteGate(t.Context(), gate)

	require.False(t, authz.UpstreamSelectorWritesAllowed(ctx, "org-enabled"))
}
