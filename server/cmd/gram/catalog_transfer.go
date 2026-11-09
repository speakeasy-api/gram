package gram

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/speakeasy-api/gram/server/internal/cataloglocalclient"
	"github.com/urfave/cli/v2"
)

func newCatalogTransferCommand() *cli.Command {
	return &cli.Command{
		Name:  "catalog-transfer",
		Usage: "Manage local catalog using automatic local Admin login",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "operation", Value: "refresh", Usage: "refresh or reset"},
			&cli.StringFlag{Name: "confirmation", Usage: "Exact server preview confirmation; required for reset apply"},
			&cli.StringFlag{Name: "action", Value: "preview", Usage: "bootstrap, preview, apply, or status"},
			&cli.StringFlag{Name: "plan-id", Usage: "Exact preview UUID; with --action apply explicitly confirms this plan only"},
		},
		ArgsUsage: " ",
		Action: func(c *cli.Context) error {
			if c.NArg() != 0 {
				return fmt.Errorf("catalog-transfer accepts no arguments")
			}
			operation := c.String("operation")
			if operation != "refresh" && operation != "reset" {
				return fmt.Errorf("operation must be refresh or reset")
			}
			var validation error
			if operation == "reset" {
				validation = cataloglocalclient.ValidateReset(c.String("action"), c.String("plan-id"), c.String("confirmation"))
			} else {
				if c.String("confirmation") != "" {
					return fmt.Errorf("--confirmation is only valid for reset")
				}
				validation = cataloglocalclient.Validate(c.String("action"), c.String("plan-id"))
			}
			if err := validation; err != nil {
				return fmt.Errorf("validate catalog action: %w", err)
			}
			adminURL, providerURL := os.Getenv("GRAM_ADMIN_SERVER_URL"), os.Getenv("GRAM_ADMIN_OIDC_EMULATOR_URL")
			if adminURL == "" {
				return fmt.Errorf("GRAM_ADMIN_SERVER_URL must be configured")
			}
			if providerURL == "" {
				return fmt.Errorf("GRAM_ADMIN_OIDC_EMULATOR_URL must be configured")
			}
			var result any
			var err error
			if operation == "reset" {
				result, err = cataloglocalclient.RunReset(c.Context, adminURL, providerURL, os.Getenv("GRAM_ADMIN_OIDC_MOCK_USER_EMAIL"), c.String("action"), c.String("plan-id"), c.String("confirmation"))
			} else {
				result, err = cataloglocalclient.Run(c.Context, adminURL, providerURL, os.Getenv("GRAM_ADMIN_OIDC_MOCK_USER_EMAIL"), c.String("action"), c.String("plan-id"))
			}

			if result != nil {
				if err := json.NewEncoder(c.App.Writer).Encode(result); err != nil {
					return fmt.Errorf("write catalog sync result: %w", err)
				}
			}
			if err != nil {
				return fmt.Errorf("local catalog %s: %w", c.String("action"), err)
			}
			return nil
		},
	}
}
