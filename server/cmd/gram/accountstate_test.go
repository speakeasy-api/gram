package gram

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/localaccounts"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
)

func TestAccountStateRequest(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		action string
		args   []string
		want   localaccounts.Profile
		bad    bool
	}{
		{"status", nil, "", false}, {"repair", nil, localaccounts.Enterprise, false},
		{"apply", []string{"enterprise"}, localaccounts.Enterprise, false},
		{"apply", []string{"payg"}, localaccounts.PAYG, false},
		{"apply", []string{"active-trial"}, localaccounts.ActiveTrial, false},
		{"apply", []string{"expired-trial"}, localaccounts.ExpiredTrial, false},
		{"apply", nil, "", true}, {"apply", []string{"book-a-demo"}, "", true},
		{"apply", []string{"enterprise", "other-user"}, "", true},
		{"repair", []string{"payg"}, "", true}, {"status", []string{"other-org"}, "", true},
	} {
		r, err := parseAccountStateRequest(tt.action, tt.args, true)
		if tt.bad {
			require.Error(t, err)
		} else {
			require.NoError(t, err)
			require.Equal(t, tt.want, r.Profile)
			require.True(t, r.DryRun)
		}
	}
}

func TestAccountStateCommand(t *testing.T) { //nolint:paralleltest // app.Run mutates urfave's shared help command
	for _, tt := range []struct {
		args    []string
		profile localaccounts.Profile
		dry     bool
	}{
		{[]string{"repair", "--dry-run"}, localaccounts.Enterprise, true},
		{[]string{"apply", "--dry-run", "expired-trial"}, localaccounts.ExpiredTrial, true},
		{[]string{"apply", "expired-trial", "--dry-run"}, localaccounts.ExpiredTrial, true},
		{[]string{"status"}, "", false},
	} {
		var out bytes.Buffer
		called := false
		cmd := accountStateCommand(func(_ context.Context, r accountStateRequest) (any, error) {
			called = true
			require.Equal(t, tt.profile, r.Profile)
			require.Equal(t, tt.dry, r.DryRun)
			return localaccounts.Result{Profile: r.Profile, DryRun: r.DryRun, Committed: !r.DryRun}, nil
		})
		app := &cli.App{Commands: []*cli.Command{cmd}, Writer: &out}
		require.NoError(t, app.Run(append([]string{"gram", "account-state"}, tt.args...)))
		require.True(t, called)
	}
}

func TestAccountStateCommittedCacheFailure(t *testing.T) { //nolint:paralleltest // app.Run mutates urfave's shared help command
	var out bytes.Buffer
	cmd := accountStateCommand(func(context.Context, accountStateRequest) (any, error) {
		return localaccounts.Result{Committed: true, CacheRefreshError: "unavailable"}, nil
	})
	app := &cli.App{Commands: []*cli.Command{cmd}, Writer: &out}
	err := app.Run([]string{"gram", "account-state", "repair", "--json"})
	require.ErrorContains(t, err, "committed")
	require.Contains(t, out.String(), `"committed": true`)
	require.ErrorContains(t, writeAccountState(adminSeedFailingWriter{err: errors.New("closed")}, localaccounts.Result{Committed: true}), "writes may already be committed")
}

// Setenv is confined to this test and restored automatically; no developer configuration
// or live provider/account state is changed.
func TestLocalAccountConfigExternalProviderGuard(t *testing.T) { //nolint:paralleltest // temporarily modifies process environment
	for _, tt := range []struct {
		name, stripe, polar, cert, key string
		blocked                        bool
	}{
		{name: "absent"},
		{name: "standard Stripe placeholder", stripe: "unset"},
		{name: "configured Stripe", stripe: "test-only-credential", blocked: true},
		{name: "configured Polar", stripe: "unset", polar: "test-only-credential", blocked: true},
		{name: "Polar placeholder remains rejected", polar: "unset", blocked: true},
		{name: "Temporal certificate", stripe: "unset", cert: "test-only-certificate", blocked: true},
		{name: "Temporal key", stripe: "unset", key: "test-only-key", blocked: true},
	} {
		t.Log(tt.name)
		t.Setenv("STRIPE_API_KEY", tt.stripe)
		t.Setenv("POLAR_API_KEY", tt.polar)
		t.Setenv("TEMPORAL_CLIENT_CERT", tt.cert)
		t.Setenv("TEMPORAL_CLIENT_KEY", tt.key)
		config, err := localAccountConfig()
		require.NoError(t, err)
		require.Equal(t, tt.blocked, config.ExternalProvidersConfigured)
	}
}

func TestAccountStateLifecycleFailureReportsCommit(t *testing.T) { //nolint:paralleltest // app.Run mutates urfave's shared help command
	var out bytes.Buffer
	cmd := accountStateCommand(func(context.Context, accountStateRequest) (any, error) {
		return localaccounts.Result{Committed: true}, errors.New("restore server failed")
	})
	app := &cli.App{Commands: []*cli.Command{cmd}, Writer: &out}
	require.ErrorContains(t, app.Run([]string{"gram", "account-state", "repair", "--json"}), "restore server failed")
	require.Contains(t, out.String(), `"committed": true`)
}

func TestAccountStateInvalidArgumentsDoNotRun(t *testing.T) { //nolint:paralleltest // app.Run mutates urfave's shared help command
	for _, args := range [][]string{{"apply", "invalid"}, {"repair", "payg"}, {"status", "other"}} {
		cmd := accountStateCommand(func(context.Context, accountStateRequest) (any, error) {
			t.Fatal("invalid args reached runner")
			return nil, nil
		})
		app := &cli.App{Commands: []*cli.Command{cmd}}
		require.Error(t, app.Run(append([]string{"gram", "account-state"}, args...)))
	}
}

func TestAccountStateUnsafeConfigDoesNotManageDaemons(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("COMPOSE_PROJECT_NAME", "gram-test")
	t.Setenv("TEMPORAL_NAMESPACE", "gram-test")
	marker := filepath.Join(dir, "called")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "pitchfork"), []byte("#!/bin/sh\ntouch '"+marker+"'\nexit 1\n"), 0700))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GRAM_ENVIRONMENT", "production")
	for _, request := range []accountStateRequest{
		{Action: "repair", Profile: localaccounts.Enterprise},
		{Action: "apply", Profile: localaccounts.PAYG, DryRun: true},
		{Action: "status"},
	} {
		_, err := runAccountState(t.Context(), request)
		require.ErrorContains(t, err, "GRAM_ENVIRONMENT=local")
		_, err = os.Stat(marker)
		require.ErrorIs(t, err, os.ErrNotExist)
	}
}

func TestLocalAccountConfigComposeDefault(t *testing.T) {
	t.Setenv("COMPOSE_PROJECT_NAME", "")
	c, err := localAccountConfig()
	require.NoError(t, err)
	require.Equal(t, "gram", c.ComposeProject, "must match compose.yml's default")
	t.Setenv("COMPOSE_PROJECT_NAME", "gram-secondary")
	c, err = localAccountConfig()
	require.NoError(t, err)
	require.Equal(t, "gram-secondary", c.ComposeProject)
}
