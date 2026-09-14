package gram

import (
	"context"
	"fmt"
	"github.com/speakeasy-api/gram/server/internal/demoseed"
	"github.com/urfave/cli/v2"
)

func newAdminSeedCommand() *cli.Command {
	return &cli.Command{
		Name:  "admin-seed",
		Usage: fmt.Sprintf("Seed %d fictional admin-list organizations (local development database only)", demoseed.AdminSeedOrganizationCount),
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "environment", EnvVars: []string{"GRAM_ENVIRONMENT"}, Required: true},
			&cli.StringFlag{Name: "database-url", EnvVars: []string{"GRAM_DATABASE_URL"}, Required: true},
		},
		Action: adminSeedAction(demoseed.RunAdminSeed),
	}
}

func adminSeedAction(seed func(context.Context, string, string) error) cli.ActionFunc {
	return func(c *cli.Context) error {
		if err := seed(c.Context, c.String("environment"), c.String("database-url")); err != nil {
			return err
		}
		_, err := fmt.Fprintf(c.App.Writer, "Seeded %d fictional organizations (%d active, %d disabled). Search: %s\n",
			demoseed.AdminSeedOrganizationCount, demoseed.AdminSeedOrganizationsPerStatus,
			demoseed.AdminSeedOrganizationsPerStatus, demoseed.AdminSeedSearchName)
		return err
	}
}
