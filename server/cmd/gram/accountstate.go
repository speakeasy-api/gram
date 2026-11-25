package gram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/speakeasy-api/gram/server/internal/localaccounts"
	"github.com/urfave/cli/v2"
)

type accountStateRequest struct {
	Action   string
	Profile  localaccounts.Profile
	DryRun   bool
	JSON     bool
	Progress func(string)
}

type accountStateRunner func(context.Context, accountStateRequest) (any, error)

func newAccountStateCommand() *cli.Command {
	return accountStateCommand(runAccountState)
}

func accountStateCommand(run accountStateRunner) *cli.Command {
	action := func(name string) cli.ActionFunc {
		return func(c *cli.Context) error {
			request, err := parseAccountStateRequest(name, c.Args().Slice(), c.Bool("dry-run"))
			if err != nil {
				return err
			}
			request.JSON = request.JSON || c.Bool("json")
			request.Progress = func(message string) { _, _ = fmt.Fprintln(c.App.ErrWriter, message) }
			result, runErr := run(c.Context, request)
			if applied, ok := result.(localaccounts.Result); ok && applied.CacheRefreshError != "" {
				runErr = errors.Join(runErr, errors.New("account profile committed; cache refresh failed; retry after restoring local Redis"))
			}
			if request.JSON {
				if result != nil {
					runErr = errors.Join(runErr, writeAccountState(c.App.Writer, result))
				}
			} else {
				runErr = errors.Join(runErr, writeAccountStateHuman(c.App.Writer, request, result, runErr))
			}
			if runErr != nil {
				return runErr
			}
			return nil
		}
	}
	dryRun := func() []cli.Flag {
		return []cli.Flag{&cli.BoolFlag{Name: "dry-run", Usage: "Check target and safety; do not change account state"}, &cli.BoolFlag{Name: "json", Usage: "Write full result JSON to stdout (progress goes to stderr)"}}
	}
	return &cli.Command{
		Name: "account-state", Usage: "Local-only account profiles for the selected dev-idp user and sole organization",
		Action: cli.ShowSubcommandHelp,
		Subcommands: []*cli.Command{
			{Name: "status", Usage: "Read selected local account state", Flags: []cli.Flag{&cli.BoolFlag{Name: "json", Usage: "Write full result JSON to stdout"}}, Action: action("status")},
			{Name: "repair", Usage: "Restore enterprise account access without reseeding", Flags: dryRun(), Action: action("repair")},
			{Name: "apply", Usage: "Apply enterprise, payg, active-trial, or expired-trial", ArgsUsage: "PROFILE", Flags: dryRun(), Action: action("apply")},
		},
	}
}

func parseAccountStateRequest(action string, args []string, dryRun bool) (accountStateRequest, error) {
	r := accountStateRequest{Action: action, Profile: "", DryRun: dryRun, JSON: false, Progress: nil}
	switch action {
	case "status", "repair":
		if len(args) != 0 {
			return r, errors.New("this command accepts no user, organization, or profile override")
		}
		if action == "repair" {
			r.Profile = localaccounts.Enterprise
		}
	case "apply":
		// urfave stops flag parsing at PROFILE; accept only known boolean flags.
		if len(args) > 1 {
			for _, flag := range args[1:] {
				switch flag {
				case "--dry-run":
					r.DryRun = true
				case "--json":
					r.JSON = true
				default:
					return r, errors.New("apply accepts one profile and only --dry-run or --json")
				}
			}
			args = args[:1]
		}
		if len(args) != 1 {
			return r, errors.New("apply requires exactly one profile")
		}
		r.Profile = localaccounts.Profile(args[0])
		if err := r.Profile.Validate(); err != nil {
			return r, fmt.Errorf("validate account profile: %w", err)
		}
	default:
		return r, errors.New("unknown account-state action")
	}
	return r, nil
}

func writeAccountState(w io.Writer, result any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		return fmt.Errorf("report account state (writes may already be committed): %w", err)
	}
	return nil
}

// progress is deliberately optional for direct callers and tests.
func (r accountStateRequest) progress(message string) {
	if r.Progress != nil {
		r.Progress(message)
	}
}

type accountStateStatus struct {
	Target localaccounts.Target `json:"target"`
	State  localaccounts.State  `json:"state"`
}

func writeAccountStateHuman(w io.Writer, r accountStateRequest, value any, runErr error) error {
	if runErr != nil {
		outcome := "Failed: no account changes committed."
		if result, ok := value.(localaccounts.Result); ok && result.Committed {
			outcome = "Profile saved, but service restoration failed; see error and progress."
			if result.CacheRefreshError != "" {
				outcome = "Profile saved, but cache refresh failed. Check errors and progress for service restoration."
			}
		}
		_, err := fmt.Fprintln(w, outcome)
		if err != nil {
			return fmt.Errorf("report account failure: %w", err)
		}
		return nil
	}
	if status, ok := value.(accountStateStatus); ok {
		_, err := fmt.Fprintf(w, "Local account: account type=%s; whitelisted=%t; stored profile=%s; trial=%s.\n", status.State.AccountType, status.State.Whitelisted, profileLabel(status.State.Profile), trialSummary(status.State.Trial))
		if err != nil {
			return fmt.Errorf("report current account: %w", err)
		}
		return nil
	}
	result, ok := value.(localaccounts.Result)
	if !ok {
		return errors.New("unexpected account-state result")
	}
	if result.DryRun {
		_, err := fmt.Fprintf(w, "Dry run: would apply %s. Current account: %s; stored profile=%s; whitelisted=%t; trial=%s.\nNo changes made; services untouched.\n", readableProfile(r.Profile), result.Before.AccountType, profileLabel(result.Before.Profile), result.Before.Whitelisted, trialSummary(result.Before.Trial))
		if err != nil {
			return fmt.Errorf("report account preview: %w", err)
		}
		return nil
	}
	if !result.Committed {
		return errors.New("account profile was not committed")
	}
	_, err := fmt.Fprintf(w, "%s profile applied.\n", readableProfile(result.Profile))
	if err != nil {
		return fmt.Errorf("report applied profile: %w", err)
	}
	return nil
}

func readableProfile(p localaccounts.Profile) string {
	switch p {
	case localaccounts.PAYG:
		return "PAYG"
	case localaccounts.Enterprise:
		return "enterprise"
	case localaccounts.ActiveTrial:
		return "active trial"
	case localaccounts.ExpiredTrial:
		return "expired trial"
	default:
		return string(p)
	}
}

func profileLabel(p localaccounts.Profile) string {
	if p == "" {
		return "unmanaged"
	}
	return string(p)
}

func trialSummary(raw json.RawMessage) string {
	var trial struct {
		ConvertedAt *string `json:"converted_at"`
		DemotedAt   *string `json:"demoted_at"`
		EndsAt      *string `json:"ends_at"`
	}
	if len(raw) == 0 || string(raw) == "null" {
		return "none"
	}
	if json.Unmarshal(raw, &trial) != nil {
		return "unknown"
	}
	if trial.ConvertedAt != nil {
		return "converted"
	}
	if trial.DemotedAt != nil {
		return "demoted"
	}
	if trial.EndsAt != nil {
		return "ends " + *trial.EndsAt
	}
	return "present"
}
