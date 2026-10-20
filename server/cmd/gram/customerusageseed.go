package gram

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/urfave/cli/v2"

	"github.com/speakeasy-api/gram/server/internal/demoseed"
)

func newCustomerUsageSeedCommand() *cli.Command {
	return &cli.Command{
		Name:  "customer-usage-seed",
		Usage: "Seed fictional paying organizations with metered usage for the admin Customer usage page (local development only)",
		Flags: append([]cli.Flag{
			&cli.StringFlag{Name: "environment", EnvVars: []string{"GRAM_ENVIRONMENT"}, Required: true},
			&cli.StringFlag{Name: "database-url", EnvVars: []string{"GRAM_DATABASE_URL"}, Required: true},
		}, clickHouseFlags()...),
		Action: func(c *cli.Context) error {
			var shutdown func(context.Context) error
			openClickhouse := func(ctx context.Context) (clickhouse.Conn, error) {
				conn, closeConn, err := newClickhouseClient(ctx, slog.New(slog.DiscardHandler), c)
				shutdown = closeConn
				return conn, err
			}
			err := demoseed.RunCustomerUsageSeed(c.Context, c.String("environment"), c.String("database-url"), c.String("clickhouse-host"), openClickhouse)
			if shutdown != nil {
				_ = shutdown(context.Background())
			}
			if err != nil {
				return fmt.Errorf("seed customer usage: %w", err)
			}
			if _, err := fmt.Fprintln(c.App.Writer, "Seeded fictional customers for the admin Customer usage page. Open /customer-usage in the admin app."); err != nil {
				return fmt.Errorf("report customer usage seed result: %w", err)
			}
			return nil
		},
	}
}
