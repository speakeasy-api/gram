package gram

import "github.com/urfave/cli/v2"

func clickHouseFlags() []cli.Flag {
	return []cli.Flag{
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
}

func clickHouseReadFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:     "clickhouse-read-host",
			Usage:    "ClickHouse read replica host",
			Required: true,
			EnvVars:  []string{"CLICKHOUSE_READ_HOST"},
		},
		&cli.StringFlag{
			Name:     "clickhouse-read-database",
			Usage:    "ClickHouse read replica database",
			Required: true,
			EnvVars:  []string{"CLICKHOUSE_READ_DATABASE"},
		},
		&cli.StringFlag{
			Name:     "clickhouse-read-username",
			Usage:    "ClickHouse read replica username",
			Required: true,
			EnvVars:  []string{"CLICKHOUSE_READ_USERNAME"},
		},
		&cli.StringFlag{
			Name:     "clickhouse-read-password",
			Usage:    "ClickHouse read replica password",
			Required: true,
			EnvVars:  []string{"CLICKHOUSE_READ_PASSWORD"},
		},
		&cli.StringFlag{
			Name:     "clickhouse-read-native-port",
			Usage:    "ClickHouse read replica native protocol port",
			Required: true,
			EnvVars:  []string{"CLICKHOUSE_READ_NATIVE_PORT"},
		},
		&cli.BoolFlag{
			Name:     "clickhouse-read-insecure",
			Usage:    "Disable TLS certificate verification for the ClickHouse read replica",
			Required: true,
			EnvVars:  []string{"CLICKHOUSE_READ_INSECURE"},
		},
	}
}
