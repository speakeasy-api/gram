package gram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/localaccounts"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
)

func TestAccountStateOutput(t *testing.T) { //nolint:paralleltest // app.Run mutates urfave's shared help command
	for _, machine := range []bool{false, true} {
		for _, scenario := range []string{"success", "same profile", "dryrun", "status", "error", "cache", "restart"} {
			t.Logf("%s/json=%t", scenario, machine)
			var out, progress bytes.Buffer
			args := []string{"gram", "account-state", "apply", "payg"}
			if scenario == "status" {
				args = []string{"gram", "account-state", "status"}
			}
			if scenario == "dryrun" {
				args = append(args, "--dry-run")
			}
			if machine {
				args = append(args, "--json")
			}
			cmd := accountStateCommand(func(_ context.Context, r accountStateRequest) (any, error) {
				r.progress("Validating...")
				if scenario == "error" {
					return nil, errors.New("unsafe configuration")
				}
				if scenario == "status" {
					return accountStateStatus{State: localaccounts.State{AccountType: "payg", Whitelisted: true, Profile: localaccounts.PAYG}}, nil
				}
				result := localaccounts.Result{Profile: r.Profile, DryRun: r.DryRun, Committed: !r.DryRun}
				if scenario == "same profile" {
					result.Before.Profile = r.Profile
				}
				if !r.DryRun {
					r.progress("Stopping...")
					r.progress("Applying...")
					r.progress("Restoring...")
				}
				if scenario == "cache" {
					result.CacheRefreshError = "unavailable"
				}
				if scenario == "restart" {
					return result, errors.New("restore server failed")
				}
				return result, nil
			})
			app := &cli.App{Commands: []*cli.Command{cmd}, Writer: &out, ErrWriter: &progress}
			err := app.Run(args)
			failed := scenario == "error" || scenario == "cache" || scenario == "restart"
			if failed {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Contains(t, progress.String(), "Validating...")
			if scenario != "error" && scenario != "status" && scenario != "dryrun" {
				require.Equal(t, "Validating...\nStopping...\nApplying...\nRestoring...\n", progress.String())
			}
			if machine {
				if scenario == "error" {
					require.Empty(t, out.String())
				} else {
					require.True(t, json.Valid(out.Bytes()), out.String())
				}
				if scenario == "cache" || scenario == "restart" {
					require.Contains(t, out.String(), `"committed": true`)
				}
			} else {
				require.NotContains(t, out.String(), `"before"`)
				require.NotContains(t, out.String(), "organization_id")
				switch scenario {
				case "success":
					require.Contains(t, out.String(), "PAYG profile applied.")
				case "same profile":
					require.Contains(t, out.String(), "PAYG profile applied.")
				case "dryrun":
					require.Contains(t, out.String(), "No changes made; services untouched")
				case "status":
					require.Contains(t, out.String(), "account type=payg; whitelisted=true")
				case "error":
					require.Contains(t, out.String(), "no account changes committed")
				default:
					require.Contains(t, out.String(), "Profile saved, but")
				}
				if failed {
					require.NotContains(t, out.String(), "profile applied.")
				}
			}
		}
	}
}

func TestAccountStateJSONFlagPositions(t *testing.T) { //nolint:paralleltest // app.Run mutates urfave's shared help command
	for _, args := range [][]string{{"--json", "--dry-run", "payg"}, {"payg", "--json", "--dry-run"}, {"--dry-run", "payg", "--json"}} {
		var out, progress bytes.Buffer
		cmd := accountStateCommand(func(_ context.Context, r accountStateRequest) (any, error) {
			require.True(t, r.JSON)
			require.True(t, r.DryRun)
			return localaccounts.Result{DryRun: true}, nil
		})
		app := &cli.App{Commands: []*cli.Command{cmd}, Writer: &out, ErrWriter: &progress}
		require.NoError(t, app.Run(append([]string{"gram", "account-state", "apply"}, args...)))
		require.True(t, json.Valid(out.Bytes()))
	}
}

func TestAccountStateDaemonProgress(t *testing.T) {
	t.Parallel()
	for _, failRestart := range []bool{false, true} {
		var progress []string
		request := accountStateRequest{Progress: func(s string) { progress = append(progress, s) }}
		run := request.daemonProgress(func(_ context.Context, _ string, args ...string) ([]byte, error) {
			if args[0] == "status" {
				return []byte(fmt.Sprintf(`{"id":%q,"namespace":"fixture","name":%q,"status":"running","pid":12}`, args[1], args[1][len("fixture/"):])), nil
			}
			if failRestart && args[0] == "start" && args[1] == "fixture/server" {
				return nil, errors.New("failed")
			}
			return nil, nil
		})
		result, err := localaccounts.WithStoppedWriters(t.Context(), "/work/fixture", run, func(context.Context) (localaccounts.Result, error) {
			request.progress("Applying...")
			return localaccounts.Result{Committed: true}, nil
		})
		require.True(t, result.Committed)
		if failRestart {
			require.Error(t, err)
		} else {
			require.NoError(t, err)
		}
		require.Equal(t, []string{"Stopping services...", "Applying...", "Restarting services..."}, progress)
	}
}

// Result contains the requested fixture and before-state, not an observed after-state.
func TestAccountStateSuccessDoesNotInventAfterState(t *testing.T) {
	t.Parallel()
	for _, profile := range []localaccounts.Profile{localaccounts.PAYG, localaccounts.Enterprise, localaccounts.ActiveTrial, localaccounts.ExpiredTrial} {
		t.Log(string(profile))
		var out bytes.Buffer
		result := localaccounts.Result{Profile: profile, Committed: true, Before: localaccounts.State{AccountType: "enterprise", Whitelisted: true, Trial: json.RawMessage(`{"converted_at":"2026-01-01T00:00:00Z"}`)}}
		require.NoError(t, writeAccountStateHuman(&out, accountStateRequest{Profile: profile}, result, nil))
		require.Contains(t, out.String(), readableProfile(profile)+" profile applied.")
		for _, invented := range []string{"effective", "requested=", "Access", "trial=", "none or converted", "profile changed"} {
			require.NotContains(t, out.String(), invented)
		}
	}
}
