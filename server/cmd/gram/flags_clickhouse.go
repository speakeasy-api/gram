package gram

import (
	"time"

	"github.com/urfave/cli/v2"
)

var clickHouseFlags = []cli.Flag{
	&cli.StringFlag{
		Name:     "clickhouse-host",
		Required: false,
		EnvVars:  []string{"CLICKHOUSE_HOST"},
		Value:    "localhost",
	},
	&cli.StringFlag{
		Name:     "clickhouse-database",
		Required: false,
		EnvVars:  []string{"CLICKHOUSE_DATABASE"},
		Value:    "default",
	},
	&cli.StringFlag{
		Name:     "clickhouse-username",
		Required: false,
		EnvVars:  []string{"CLICKHOUSE_USERNAME"},
		Value:    "gram",
	},
	&cli.StringFlag{
		Name:     "clickhouse-password",
		Required: false,
		EnvVars:  []string{"CLICKHOUSE_PASSWORD"},
		Value:    "gram",
	},
	&cli.StringFlag{
		Name:     "clickhouse-native-port",
		Required: false,
		EnvVars:  []string{"CLICKHOUSE_NATIVE_PORT"},
		Value:    "9440",
	},
	&cli.BoolFlag{
		Name:     "clickhouse-insecure",
		Required: false,
		EnvVars:  []string{"CLICKHOUSE_INSECURE"},
		Value:    false,
	},
	&cli.IntFlag{
		Name:     "clickhouse-max-open-conns",
		Required: false,
		EnvVars:  []string{"CLICKHOUSE_MAX_OPEN_CONNS"},
		Value:    10,
	},
	&cli.IntFlag{
		Name:     "clickhouse-max-idle-conns",
		Required: false,
		EnvVars:  []string{"CLICKHOUSE_MAX_IDLE_CONNS"},
		Value:    5,
	},
	&cli.DurationFlag{
		Name:     "clickhouse-conn-max-lifetime",
		Required: false,
		EnvVars:  []string{"CLICKHOUSE_CONN_MAX_LIFETIME"},
		// Recycle pooled connections well before a network middlebox (load
		// balancer/NAT) silently drops idle ones, which otherwise surfaces as
		// "connection reset" errors on reuse. Kept below common idle timeouts,
		// and far below the driver default of 1 hour.
		Value: 5 * time.Minute,
	},
	&cli.DurationFlag{
		Name:     "clickhouse-dial-timeout",
		Required: false,
		EnvVars:  []string{"CLICKHOUSE_DIAL_TIMEOUT"},
		Value:    10 * time.Second,
	},
}
