package gram

import (
	"fmt"
	"os"

	"github.com/urfave/cli/v2"
	"go.opentelemetry.io/otel"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/demoseed"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	slackapi "github.com/speakeasy-api/gram/server/internal/thirdparty/slack/api"
)

func newDemoSeedCommand() *cli.Command {
	return &cli.Command{
		Name:  "demo-seed",
		Usage: "Provision or refresh the shared demo organization (idempotent; intended to run daily)",
		Flags: append([]cli.Flag{
			&cli.BoolFlag{
				Name: "local",
				Usage: "Seed the local development organization as well as the shared demo org: the same data " +
					"retargeted at the dev-idp org, writable, plus the local-only fixtures (your user, API key).",
				EnvVars: []string{"GRAM_DEMO_SEED_LOCAL"},
			},
			&cli.StringFlag{
				Name:    "local-user-email",
				Usage:   "Email of the developer to adopt into the local org. Defaults to `git config user.email`. Ignored without --local.",
				EnvVars: []string{"GRAM_DEMO_SEED_LOCAL_USER_EMAIL"},
			},
			&cli.StringFlag{
				Name:     "database-url",
				Usage:    "Database URL",
				EnvVars:  []string{"GRAM_DATABASE_URL"},
				Required: true,
			},
			&cli.StringFlag{
				Name:     "assets-backend",
				Usage:    "The backend to use for managing assets",
				EnvVars:  []string{"GRAM_ASSETS_BACKEND"},
				Required: true,
			},
			&cli.StringFlag{
				Name:     "assets-uri",
				Usage:    "The location of the assets backend to connect to",
				EnvVars:  []string{"GRAM_ASSETS_URI"},
				Required: true,
			},
			&cli.StringFlag{
				Name:    "encryption-key",
				Usage:   "Key for App level AES encryption, used to store the local Slack test app token",
				EnvVars: []string{"GRAM_ENCRYPTION_KEY"},
			},
			&cli.StringFlag{
				Name:    "local-slack-bot-token",
				Usage:   "Bot token of your Slack test app; with --local, connects the local org to its workspace (see mise run zero:slack)",
				EnvVars: []string{"GRAM_DEV_SLACK_BOT_TOKEN"},
			},
			&cli.StringFlag{
				Name:    "local-slack-email",
				Usage:   "Your Slack account email, when it differs from your Speakeasy email",
				EnvVars: []string{"GRAM_DEV_SLACK_EMAIL"},
			},
			&cli.StringFlag{
				Name:    "environment",
				Usage:   "Deployment environment, which decides the API key prefix the local fixtures mint",
				EnvVars: []string{"GRAM_ENVIRONMENT"},
				Value:   "local",
			},
		}, append(clickHouseFlags(), redisFlags()...)...),
		Action: func(c *cli.Context) error {
			ctx := c.Context
			logger := PullLogger(ctx)

			db, err := newDBClient(ctx, logger, otel.GetMeterProvider(), c.String("database-url"), dbClientOptions{
				enableUnsafeLogging: false,
			})
			if err != nil {
				return fmt.Errorf("connect to postgres: %w", err)
			}
			defer db.Close()

			ch, chShutdown, err := newClickhouseClient(ctx, logger, c)
			if err != nil {
				return fmt.Errorf("connect to clickhouse: %w", err)
			}
			defer o11y.NoLogDefer(func() error { return chShutdown(ctx) })

			blob, blobShutdown, err := newAssetStorage(ctx, logger, assetStorageOptions{
				assetsBackend: c.String("assets-backend"),
				assetsURI:     c.String("assets-uri"),
			})
			if err != nil {
				return fmt.Errorf("connect to asset storage: %w", err)
			}
			defer o11y.NoLogDefer(func() error { return blobShutdown(ctx) })

			if !c.Bool("local") {
				if err := demoseed.Run(ctx, logger, db, ch, blob, demoseed.DefaultSpec()); err != nil {
					return fmt.Errorf("apply seed: %w", err)
				}
				return nil
			}

			// Locally both tenants are seeded: the dev-idp org the developer
			// logs into, and the shared demo org so /explore-demo renders the
			// same data it does in production. No membership is granted in the
			// demo org — as in production, it is reached through auth.enterDemo,
			// not the org switcher.
			//
			// The developer's own org goes first, fixtures included: it is the
			// one that lifts the BookDemo gate, so a demo-org failure
			// (ClickHouse is the flaky half locally) leaves a usable
			// environment behind.
			if err := demoseed.Run(ctx, logger, db, ch, blob, demoseed.LocalSpec()); err != nil {
				return fmt.Errorf("apply seed: %w", err)
			}

			// A missing or unreachable cache is not fatal: the fixtures just
			// cannot bust the feature/user caches, so a running server may
			// serve stale entitlements until it is restarted.
			redisClient, err := newRedisClient(ctx, redisClientOptions{
				redisAddr:     c.String("redis-cache-addr"),
				redisPassword: c.String("redis-cache-password"),
				enableTracing: false,
			})
			if err != nil {
				logger.WarnContext(ctx, "local seed could not reach redis; restart the server if features look stale", attr.SlogError(err))
				redisClient = nil
			} else {
				defer o11y.NoLogDefer(redisClient.Close)
			}

			// The fixtures warn when a stale mise.local.toml is shadowing the
			// values they provision; environment access belongs here, not in
			// the package.
			observed := make(map[string]string, len(demoseed.StaleOverrideVars()))
			for _, name := range demoseed.StaleOverrideVars() {
				observed[name] = os.Getenv(name)
			}

			slack := demoseed.LocalSlackOptions{BotToken: "", SlackEmail: c.String("local-slack-email"), Encryption: nil, Slack: nil}
			if token := c.String("local-slack-bot-token"); token != "" && token != "unset" {
				encryptionClient, err := encryption.New(c.String("encryption-key"))
				if err != nil {
					return fmt.Errorf("create encryption client for the local Slack app: %w", err)
				}
				slack.BotToken = token
				slack.Encryption = encryptionClient
				slack.Slack = slackapi.NewClient("", guardian.NewDefaultPolicy(otel.GetTracerProvider()).Client())
			}

			if err := demoseed.RunLocalFixtures(ctx, logger, db, blob, redisClient, demoseed.LocalFixturesOptions{
				DeveloperEmail: c.String("local-user-email"),
				Environment:    c.String("environment"),
				ObservedEnv:    observed,
				Slack:          slack,
			}); err != nil {
				return fmt.Errorf("apply local fixtures: %w", err)
			}

			if err := demoseed.Run(ctx, logger, db, ch, blob, demoseed.DefaultSpec()); err != nil {
				return fmt.Errorf("apply demo org seed: %w", err)
			}

			return nil
		},
	}
}
