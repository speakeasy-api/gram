package gram

import (
	"github.com/urfave/cli/v2"

	"github.com/speakeasy-api/gram/server/internal/redisrole"
)

func redisFlags() []cli.Flag {
	flags := []cli.Flag{
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
	return append(flags, redisRoleFlags()...)
}

// redisRoleFlags configure the evictable Redis role. Commands that declare
// their own Redis flags append these as well.
func redisRoleFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:    "redis-evictable-addr",
			Usage:   "Address of the redis instance for key families that rebuild on a miss. Unset, empty or equal to redis-cache-addr keeps them on the cache server",
			EnvVars: []string{"GRAM_REDIS_EVICTABLE_ADDR"},
		},
		&cli.StringFlag{
			Name:    "redis-evictable-password",
			Usage:   "Password for the evictable redis instance",
			EnvVars: []string{"GRAM_REDIS_EVICTABLE_PASSWORD"},
		},
		&cli.StringFlag{
			Name:    "redis-role-guard",
			Usage:   "What to do with a redis key sent to the wrong role: report (log and count) or enforce (refuse the command)",
			Value:   string(redisrole.GuardReport),
			EnvVars: []string{"GRAM_REDIS_ROLE_GUARD"},
		},
	}
}
