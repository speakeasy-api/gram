package gram

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/urfave/cli/v2"
	"go.opentelemetry.io/otel"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/demoseed"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/inv"
	"github.com/speakeasy-api/gram/server/internal/mcpauthz"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/dailyoutcomes"
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
				Name:    "site-url",
				Usage:   "Canonical public site URL for local catalog assets; required with --local",
				EnvVars: []string{"GRAM_SITE_URL"},
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
				Usage:   "Key for App level AES encryption, which seals the demo sessions' dummy token and the local Slack test app token",
				EnvVars: []string{"GRAM_ENCRYPTION_KEY"},
			},
			&cli.StringFlag{
				Name:    "local-mock-mcp-url",
				Usage:   "With --local, the URL of the local mock MCP server (server/cmd/mock-mcp) to register as a working remote MCP server; empty leaves it out",
				EnvVars: []string{"GRAM_MOCK_MCP_URL"},
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
				Name:    "local-devidp-url",
				Usage:   "External URL of the local dev-idp; the Enterprise Sign-In (ID-JAG) Server signs in and exchanges identities through it",
				EnvVars: []string{"GRAM_DEVIDP_EXTERNAL_URL"},
			},
			&cli.StringFlag{
				Name:    "local-devidp-client-id",
				Usage:   "The dev-idp's static login client id, which Speakeasy's dev-idp registration for the Enterprise Sign-In (ID-JAG) Server reuses",
				EnvVars: []string{"GRAM_IDP_CLIENT_ID"},
			},
			&cli.StringFlag{
				Name:    "local-cache-dir",
				Usage:   "Directory that keeps the Linear and Notion clients the local seed registers, so reseeding reuses them",
				EnvVars: []string{"GRAM_DEMOSEED_CACHE_DIR"},
			},
			&cli.StringFlag{
				Name:    "server-url",
				Usage:   "Speakeasy's own URL: it hosts the MCP fixture the seeded servers reach, serves the seeded clients' metadata documents, and takes their sign-ins back",
				EnvVars: []string{"GRAM_SERVER_URL"},
			},
			&cli.StringFlag{
				Name:     "authz-issuer-url",
				Usage:    "Speakeasy platform signing issuer origin, under which the seeded assistant triggers are registered",
				EnvVars:  []string{"GRAM_AUTHZ_ISSUER_URL"},
				Required: true,
			},
			&cli.StringFlag{
				Name:    "environment",
				Usage:   "Deployment environment, which decides the API key prefix the local fixtures mint",
				EnvVars: []string{"GRAM_ENVIRONMENT"},
				Value:   "local",
			},
		}, append(clickHouseFlags(), redisFlags()...)...),
		Action: func(c *cli.Context) error {
			var siteURL *url.URL
			var upstreams demoseed.LocalUpstreamOptions
			if c.Bool("local") {
				var err error
				siteURL, err = parseSiteURL(c.String("site-url"))
				if err != nil {
					return fmt.Errorf("local seed site-url: %w", err)
				}
				// Validate before the seed writes anything, so a bad setting
				// cannot leave the org reseeded without its local fixtures.
				upstreams, err = localUpstreamOptions(c)
				if err != nil {
					return err
				}
				if err := validateMockMCPURL(c.String("local-mock-mcp-url")); err != nil {
					return fmt.Errorf("local seed local-mock-mcp-url: %w", err)
				}
			}
			// HTTP is only for an environment explicitly declared local: the
			// environment flag's default must not relax the issuer check.
			issuerURL := c.String("authz-issuer-url")
			allowHTTP := c.IsSet("environment") && c.String("environment") == "local"
			inv.Require("demo seed assistant trigger issuer",
				"authz-issuer-url is a Speakeasy issuer origin", mcpauthz.ValidateIssuerOrigin(issuerURL, allowHTTP),
			)
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

			enc, err := encryption.New(c.String("encryption-key"))
			if err != nil {
				return fmt.Errorf("create encryption client (set GRAM_ENCRYPTION_KEY): %w", err)
			}

			if !c.Bool("local") {
				if err := demoseed.Run(ctx, logger, db, ch, blob, enc, issuerURL, c.String("server-url"), demoseed.DefaultSpec()); err != nil {
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
			if err := demoseed.Run(ctx, logger, db, ch, blob, enc, issuerURL, c.String("server-url"), demoseed.LocalSpec()); err != nil {
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
				slack.BotToken = token
				slack.Encryption = enc
				slack.Slack = slackapi.NewClient("", guardian.NewDefaultPolicy(otel.GetTracerProvider()).Client())
			}

			if err := demoseed.RunLocalFixtures(ctx, logger, db, blob, redisClient, demoseed.LocalFixturesOptions{
				DeveloperEmail: c.String("local-user-email"),
				SiteURL:        siteURL,
				Environment:    c.String("environment"),
				ObservedEnv:    observed,
				Slack:          slack,
				Upstreams:      upstreams,
				ClickHouse:     ch,
				MockMCPURL:     c.String("local-mock-mcp-url"),
			}); err != nil {
				return fmt.Errorf("apply local fixtures: %w", err)
			}

			if err := demoseed.Run(ctx, logger, db, ch, blob, enc, issuerURL, c.String("server-url"), demoseed.DefaultSpec()); err != nil {
				return fmt.Errorf("apply demo org seed: %w", err)
			}

			// The long-term outcomes table has no tenant, so only the local seed
			// fills it, by running the daily job over the seeded days. Production
			// never writes the demo organization's made-up attempts into it.
			// Only the dev organization counts locally, so every provider, app and client name folds to other.
			outcomes := dailyoutcomes.NewBuilder(logger, otel.GetMeterProvider(), db, ch)
			now := time.Now()
			for daysAgo := 1; daysAgo <= demoseed.ConnectionAttemptDays; daysAgo++ {
				day := dailyoutcomes.StartOfDay(now.AddDate(0, 0, -daysAgo))
				_, err := outcomes.BuildDay(ctx, day, now)
				switch {
				case err == nil, errors.Is(err, dailyoutcomes.ErrDayOpen), errors.Is(err, dailyoutcomes.ErrDayExpiring):
				case errors.Is(err, dailyoutcomes.ErrStagedMismatch), errors.Is(err, dailyoutcomes.ErrStoredMismatch):
					// The local worker is building the same day; its build wins.
					logger.WarnContext(ctx, "local connection outcomes day built concurrently", attr.SlogError(err))
				default:
					return fmt.Errorf("build local connection outcomes: %w", err)
				}
			}

			return nil
		},
	}
}

// localUpstreamOptions reads the settings of the servers the local seed
// wires through the developer's machine. Without a server URL they keep the
// shared seed's setup.
func localUpstreamOptions(c *cli.Context) (demoseed.LocalUpstreamOptions, error) {
	opts := demoseed.LocalUpstreamOptions{ServerURL: nil, Encryption: nil, DevIDPURL: "", DevIDPClientID: "", HTTPClient: nil, Policy: nil, CacheDir: ""}
	raw := c.String("server-url")
	if raw == "" || raw == "unset" {
		return opts, nil
	}
	serverURL, err := parseOrigin(raw)
	if err != nil {
		return opts, fmt.Errorf("GRAM_SERVER_URL: %w", err)
	}
	encryptionClient, err := encryption.New(c.String("encryption-key"))
	if err != nil {
		return opts, fmt.Errorf("create encryption client for the local upstreams: %w", err)
	}
	opts.ServerURL = serverURL
	opts.Encryption = encryptionClient
	devIDPURL, err := parseDevIDPURL(c.String("local-devidp-url"))
	if err != nil {
		return opts, fmt.Errorf("GRAM_DEVIDP_EXTERNAL_URL: %w", err)
	}
	opts.DevIDPURL = devIDPURL
	opts.DevIDPClientID = c.String("local-devidp-client-id")
	// The dev-idp listens on localhost, which the default egress policy
	// refuses, so only its client skips the policy; the local seed only ever
	// calls the developer's own dev-idp with it.
	unsafePolicy, err := guardian.NewUnsafePolicy(otel.GetTracerProvider(), []string{})
	if err != nil {
		return opts, fmt.Errorf("create the dev-idp egress policy: %w", err)
	}
	opts.HTTPClient = unsafePolicy.Client()
	opts.HTTPClient.Timeout = 10 * time.Second
	// Linear and Notion are public hosts the default egress policy allows.
	opts.Policy = guardian.NewDefaultPolicy(otel.GetTracerProvider())
	opts.CacheDir = c.String("local-cache-dir")
	return opts, nil
}

// parseOrigin accepts an absolute http(s) origin: a scheme and host, with no
// path, query or fragment.
func parseOrigin(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("parse %q: %w", raw, err)
	}
	if (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return nil, fmt.Errorf("%q is not an absolute http(s) URL", raw)
	}
	if u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, fmt.Errorf("%q must be an origin, without a path, query or fragment", raw)
	}
	return u, nil
}

// parseDevIDPURL reads the local dev-idp's external URL. Empty or "unset"
// disables the dev-idp wiring. Otherwise it must be an absolute origin, over
// HTTPS unless the host is loopback: identity traffic goes there.
func parseDevIDPURL(raw string) (string, error) {
	if raw == "" || raw == "unset" {
		return "", nil
	}
	u, err := parseOrigin(raw)
	if err != nil {
		return "", err
	}
	if u.Scheme != "https" && !isLoopbackHostname(u.Hostname()) {
		return "", fmt.Errorf("%q must use HTTPS unless it is on a loopback host", raw)
	}
	return strings.TrimRight(u.String(), "/"), nil
}

// isLoopbackHostname reports whether host is localhost or a loopback address.
func isLoopbackHostname(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// validateMockMCPURL accepts an empty value, which leaves the mock MCP server
// out of the local fixtures, or an absolute http or https URL with a host.
func validateMockMCPURL(raw string) error {
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("parse %q: %w", raw, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("%q must be an absolute http or https URL", raw)
	}
	if u.Hostname() == "" {
		return fmt.Errorf("%q has no host", raw)
	}
	return nil
}
