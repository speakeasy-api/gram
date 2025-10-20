package gram

import "github.com/urfave/cli/v2"

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
}
