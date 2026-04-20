package gram

import "github.com/urfave/cli/v2"

var redisFlags = []cli.Flag{
	&cli.StringFlag{
		Name:    "redis-cache-addr",
		Usage:   "Address of the redis cache server",
		EnvVars: []string{"GRAM_REDIS_CACHE_ADDR"},
	},
	&cli.StringFlag{
		Name:    "redis-cache-password",
		Usage:   "Password for the redis cache server",
		EnvVars: []string{"GRAM_REDIS_CACHE_PASSWORD"},
	},
	&cli.BoolFlag{
		Name:    "redis-enable-tracing",
		Usage:   "Enable tracing for the redis cache server",
		Value:   false,
		EnvVars: []string{"GRAM_REDIS_ENABLE_TRACING"},
	},
}
