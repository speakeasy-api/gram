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
		Name:      "catalog-transfer",
		Usage:     "Sync the local catalog through authenticated local Admin HTTP",
		ArgsUsage: " ",
		Action: func(c *cli.Context) error {
			if c.NArg() != 0 {
				return fmt.Errorf("catalog-transfer accepts no arguments")
			}
			result, err := cataloglocalclient.Sync(c.Context, os.Getenv("GRAM_ADMIN_SERVER_URL"), os.Getenv("GRAM_ADMIN_OIDC_EMULATOR_URL"), os.Getenv("GRAM_ADMIN_OIDC_MOCK_USER_EMAIL"))
			if err != nil {
				return fmt.Errorf("sync local catalog: %w", err)
			}
			if err := json.NewEncoder(c.App.Writer).Encode(result); err != nil {
				return fmt.Errorf("write catalog sync result: %w", err)
			}
			return nil
		},
	}
}
