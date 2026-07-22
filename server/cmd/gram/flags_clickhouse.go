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
	// clickhouse-conn-max-lifetime must stay comfortably below the idle timeout
	// of any load balancer fronting ClickHouse. Otherwise pooled connections can
	// be dropped server-side while idle and then handed back to a query,
	// surfacing as intermittent connection resets/timeouts.
	&cli.DurationFlag{
		Name:     "clickhouse-conn-max-lifetime",
		Required: false,
		EnvVars:  []string{"CLICKHOUSE_CONN_MAX_LIFETIME"},
		Value:    5 * time.Minute,
	},
	&cli.DurationFlag{
		Name:     "clickhouse-dial-timeout",
		Required: false,
		EnvVars:  []string{"CLICKHOUSE_DIAL_TIMEOUT"},
		Value:    10 * time.Second,
	},
}
