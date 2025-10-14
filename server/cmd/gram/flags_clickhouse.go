package gram

import "github.com/urfave/cli/v2"

var clickHouseFlags = []cli.Flag{
	&cli.StringFlag{
		Name:     "clickhouse-host",
		Usage:    "Clickhouse Host",
		Required: false,
		EnvVars:  []string{"CLICKHOUSE_HOST"},
		Value:    "localhost",
	},
	&cli.StringFlag{
		Name:     "clickhouse-database",
		Usage:    "Clickhouse Database",
		Required: false,
		EnvVars:  []string{"CLICKHOUSE_DATABASE"},
		Value:    "gram",
	},
	&cli.StringFlag{
		Name:     "clickhouse-username",
		Usage:    "Clickhouse Username",
		Required: false,
		EnvVars:  []string{"CLICKHOUSE_USERNAME"},
		Value:    "gram",
	},
	&cli.StringFlag{
		Name:     "clickhouse-password",
		Usage:    "Clickhouse Password",
		Required: false,
		EnvVars:  []string{"CLICKHOUSE_PASSWORD"},
		Value:    "gram",
	},
	&cli.StringFlag{
		Name:     "clickhouse-native-port",
		Usage:    "Clickhouse Native Port",
		Required: false,
		EnvVars:  []string{"CLICKHOUSE_NATIVE_PORT"},
		Value:    "9440",
	},
	&cli.BoolFlag{
		Name:     "clickhouse-insecure",
		Usage:    "Clickhouse Insecure",
		Required: false,
		EnvVars:  []string{"CLICKHOUSE_INSECURE"},
		Value:    false,
	},
}
