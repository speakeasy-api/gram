package gram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/sourcegraph/conc/pool"
	"github.com/urfave/cli/v2"
	"github.com/urfave/cli/v2/altsrc"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.temporal.io/sdk/client"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/about"
	"github.com/speakeasy-api/gram/server/internal/access"
	"github.com/speakeasy-api/gram/server/internal/agent"
	"github.com/speakeasy-api/gram/server/internal/aiintegrations"
	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/assistant_platform_mcp_adapter"
	"github.com/speakeasy-api/gram/server/internal/assistantmemories"
	"github.com/speakeasy-api/gram/server/internal/assistants"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auditapi"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	"github.com/speakeasy-api/gram/server/internal/auth/chatsessions"
	"github.com/speakeasy-api/gram/server/internal/auth/identity"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/background"
	"github.com/speakeasy-api/gram/server/internal/background/activities"
	risk_analysis "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/businessmemory"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/chat/analysis"
	"github.com/speakeasy-api/gram/server/internal/chatanalysis"
	chatsessionssvc "github.com/speakeasy-api/gram/server/internal/chatsessions"
	"github.com/speakeasy-api/gram/server/internal/cliauth"
	"github.com/speakeasy-api/gram/server/internal/collections"
	"github.com/speakeasy-api/gram/server/internal/control"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	"github.com/speakeasy-api/gram/server/internal/deployments"
	"github.com/speakeasy-api/gram/server/internal/deviceintegrations"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/environments"
	"github.com/speakeasy-api/gram/server/internal/external"
	"github.com/speakeasy-api/gram/server/internal/externalcredentials"
	"github.com/speakeasy-api/gram/server/internal/externalkeys"
	"github.com/speakeasy-api/gram/server/internal/externalmcp"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/functions"
	"github.com/speakeasy-api/gram/server/internal/hooks"
	"github.com/speakeasy-api/gram/server/internal/instances"
	"github.com/speakeasy-api/gram/server/internal/integrations"
	"github.com/speakeasy-api/gram/server/internal/jsonwebkeysets"
	"github.com/speakeasy-api/gram/server/internal/k8s"
	"github.com/speakeasy-api/gram/server/internal/keys"
	"github.com/speakeasy-api/gram/server/internal/litellm"
	"github.com/speakeasy-api/gram/server/internal/litellm/callcache"
	"github.com/speakeasy-api/gram/server/internal/marketplace"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval"
	mcpapprovaladvisories "github.com/speakeasy-api/gram/server/internal/mcpapproval/advisories"
	mcpapprovalcatalog "github.com/speakeasy-api/gram/server/internal/mcpapproval/catalog"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/domainmeta"
	mcpapprovalevidence "github.com/speakeasy-api/gram/server/internal/mcpapproval/evidence"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/packagemeta"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/remoteprobe"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/repometa"
	"github.com/speakeasy-api/gram/server/internal/mcpclient"
	"github.com/speakeasy-api/gram/server/internal/mcpendpoints"
	"github.com/speakeasy-api/gram/server/internal/mcpmetadata"
	mcpmetadata_repo "github.com/speakeasy-api/gram/server/internal/mcpmetadata/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	"github.com/speakeasy-api/gram/server/internal/memory"
	"github.com/speakeasy-api/gram/server/internal/metamcp"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/modelkeys"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/openrouterkeys"
	"github.com/speakeasy-api/gram/server/internal/organizations"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	otelsvc "github.com/speakeasy-api/gram/server/internal/otel"
	"github.com/speakeasy-api/gram/server/internal/otelforwarding"
	"github.com/speakeasy-api/gram/server/internal/packages"
	"github.com/speakeasy-api/gram/server/internal/platformmcp"
	"github.com/speakeasy-api/gram/server/internal/platformmcp/localfixture"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
	platformchangelog "github.com/speakeasy-api/gram/server/internal/platformtools/changelog"
	platformdocs "github.com/speakeasy-api/gram/server/internal/platformtools/docs"
	platformtoolsruntime "github.com/speakeasy-api/gram/server/internal/platformtools/runtime"
	platformskills "github.com/speakeasy-api/gram/server/internal/platformtools/skills"
	platformslack "github.com/speakeasy-api/gram/server/internal/platformtools/slack"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/projects"
	"github.com/speakeasy-api/gram/server/internal/rag"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/resources"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/risk/celenv"
	riskchrepo "github.com/speakeasy-api/gram/server/internal/risk/chrepo"
	"github.com/speakeasy-api/gram/server/internal/risk/policybypass"
	"github.com/speakeasy-api/gram/server/internal/risk/presetlib"
	"github.com/speakeasy-api/gram/server/internal/scanners"
	"github.com/speakeasy-api/gram/server/internal/scanners/customruleanalyzer"
	"github.com/speakeasy-api/gram/server/internal/scanners/promptinjection"
	piopenrouter "github.com/speakeasy-api/gram/server/internal/scanners/promptinjection/openrouter"
	"github.com/speakeasy-api/gram/server/internal/scanners/promptpolicy"
	ppopenrouter "github.com/speakeasy-api/gram/server/internal/scanners/promptpolicy/openrouter"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/skillefficacy"
	"github.com/speakeasy-api/gram/server/internal/skills"
	"github.com/speakeasy-api/gram/server/internal/skills/efficacy"
	feedbackrecorder "github.com/speakeasy-api/gram/server/internal/skills/feedback"
	"github.com/speakeasy-api/gram/server/internal/spendrules"
	spendcelenv "github.com/speakeasy-api/gram/server/internal/spendrules/celenv"
	tm "github.com/speakeasy-api/gram/server/internal/telemetry"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/templates"
	ghclient "github.com/speakeasy-api/gram/server/internal/thirdparty/github"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/loops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/pylon"
	slackapi "github.com/speakeasy-api/gram/server/internal/thirdparty/slack/api"
	slack_client "github.com/speakeasy-api/gram/server/internal/thirdparty/slack/client"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/toolcallobserver"
	"github.com/speakeasy-api/gram/server/internal/trialemails"
	"github.com/speakeasy-api/gram/server/internal/triggers"
	"github.com/speakeasy-api/gram/server/internal/unproxiedmcp"

	"github.com/speakeasy-api/gram/server/internal/tokenexchange"
	"github.com/speakeasy-api/gram/server/internal/tools"
	"github.com/speakeasy-api/gram/server/internal/toolsets"
	"github.com/speakeasy-api/gram/server/internal/tunneledmcp"
	"github.com/speakeasy-api/gram/server/internal/usage"
	userRepo "github.com/speakeasy-api/gram/server/internal/users/repo"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
	"github.com/speakeasy-api/gram/server/internal/variations"
	"github.com/speakeasy-api/gram/server/internal/xmcp"
	"github.com/speakeasy-api/gram/tunnel/route"
)

// restoreLocalPluginRepositories repairs marketplace rows created before the
// persistent local publisher existed. Current snapshots survive restarts and
// are left untouched, preserving their embedded local API keys.
func restoreLocalPluginRepositories(
	ctx context.Context,
	logger *slog.Logger,
	db *pgxpool.Pool,
	localPublisher *localfixture.InMemoryGitHubPublisher,
	pluginPublisher *plugins.Service,
) error {
	queries := pluginsrepo.New(db)
	after := uuid.Nil
	for {
		candidates, err := queries.ListPluginPublishCandidates(ctx, pluginsrepo.ListPluginPublishCandidatesParams{
			AfterProjectID: after,
			ResultLimit:    100,
		})
		if err != nil {
			return fmt.Errorf("list plugin publish candidates: %w", err)
		}
		if len(candidates) == 0 {
			return nil
		}

		for _, candidate := range candidates {
			connection, err := queries.GetGitHubConnection(ctx, candidate.ProjectID)
			if errors.Is(err, pgx.ErrNoRows) {
				continue
			}
			if err != nil {
				return fmt.Errorf("get plugin connection for project %s: %w", candidate.ProjectID, err)
			}
			if files, err := localPublisher.MainBranchFiles(ctx, connection.RepoOwner, connection.RepoName); err == nil && len(files) > 0 {
				continue
			} else if err != nil && !errors.Is(err, ghclient.ErrRepoNotFound) {
				return fmt.Errorf("read local plugin repository for project %s: %w", candidate.ProjectID, err)
			}

			if _, err := pluginPublisher.PublishProject(ctx, plugins.PublishProjectInput{
				ProjectID:              candidate.ProjectID,
				CreatedByUserID:        candidate.CreatedByUserID,
				CommitMessage:          "Restore local plugin marketplace",
				ForcePlatformMCPRepair: false,
				SkipIfUnchanged:        false,
			}); err != nil {
				logger.WarnContext(ctx, "restore local plugin repository",
					attr.SlogProjectID(candidate.ProjectID.String()),
					attr.SlogError(err),
				)
			}
		}

		after = candidates[len(candidates)-1].ProjectID
		if len(candidates) < 100 {
			return nil
		}
	}
}

// shutdownDrainTimeout is how long srv.Shutdown waits for in-flight requests
// to complete on SIGTERM before the process exits. It must cover the slowest
// outbound work any endpoint can do, including the MCP runtime path
// (POST /mcp/{mcpSlug}).
//
// Note: the effective drain is also bounded by infrastructure settings such as
// terminationGracePeriodSeconds in Kubernetes, which must be set above this
// value for the full window to be honored.
const shutdownDrainTimeout = 60 * time.Second

func newStartCommand() *cli.Command {
	var shutdownFuncs []func(context.Context) error
	dbClose := func() {}
	clickhouseShutdown := noopShutdown

	flags := []cli.Flag{
		&cli.StringFlag{
			Name:    "address",
			Value:   ":8080",
			Usage:   "HTTP address to listen on",
			EnvVars: []string{"GRAM_SERVER_ADDRESS"},
		},
		&cli.StringFlag{
			Name:     "server-url",
			Usage:    "The public URL of the server",
			EnvVars:  []string{"GRAM_SERVER_URL"},
			Required: true,
		},
		&cli.StringFlag{
			Name:     "environment",
			Usage:    "The current server environment", // local, dev, prod
			Required: true,
			EnvVars:  []string{"GRAM_ENVIRONMENT"},
		},
		&cli.StringFlag{
			Name:     "ssl-key-file",
			Usage:    "The SSL key file path to use for the server",
			Required: false,
			EnvVars:  []string{"GRAM_SSL_KEY_FILE"},
		},
		&cli.StringFlag{
			Name:     "ssl-cert-file",
			Usage:    "The SSL certifate file path to use for the server",
			Required: false,
			EnvVars:  []string{"GRAM_SSL_CERT_FILE"},
		},
		&cli.StringFlag{
			Name:    "control-address",
			Value:   ":8081",
			Usage:   "HTTP address to listen on",
			EnvVars: []string{"GRAM_CONTROL_ADDRESS"},
		},
		&cli.StringFlag{
			Name:     "site-url",
			Usage:    "The URL of the site",
			EnvVars:  []string{"GRAM_SITE_URL"},
			Required: true,
		},
		&cli.StringFlag{
			Name:     "database-url",
			Usage:    "Database URL",
			EnvVars:  []string{"GRAM_DATABASE_URL"},
			Required: true,
		},
		&cli.BoolFlag{
			Name:    "unsafe-db-log",
			Usage:   "Turn on unsafe database logging. WARNING: This will log all database queries and data to the console.",
			EnvVars: []string{"GRAM_UNSAFE_DB_LOG"},
			Value:   false,
		},
		&cli.StringFlag{
			Name:     "idp-base-url",
			Usage:    "OIDC identity provider base URL (e.g. http://localhost:35291/oauth2)",
			EnvVars:  []string{"GRAM_IDP_BASE_URL"},
			Required: true,
		},
		&cli.StringFlag{
			Name:     "idp-client-id",
			Usage:    "OIDC client ID for the identity provider",
			EnvVars:  []string{"GRAM_IDP_CLIENT_ID"},
			Required: true,
		},
		&cli.StringFlag{
			Name:    "idp-client-secret",
			Usage:   "WorkOS API key for user management and identity lookups",
			EnvVars: []string{"GRAM_IDP_CLIENT_SECRET"},
		},
		&cli.BoolFlag{
			Name:    "with-otel-tracing",
			Usage:   "Enable OpenTelemetry traces",
			EnvVars: []string{"GRAM_ENABLE_OTEL_TRACES"},
		},
		&cli.BoolFlag{
			Name:    "with-otel-metrics",
			Usage:   "Enable OpenTelemetry metrics",
			EnvVars: []string{"GRAM_ENABLE_OTEL_METRICS"},
		},
		&cli.StringFlag{
			Name:     "assets-backend",
			Usage:    "The backend to use for managing assets",
			EnvVars:  []string{"GRAM_ASSETS_BACKEND"},
			Required: true,
			Action: func(c *cli.Context, val string) error {
				if val != "fs" && val != "gcs" {
					return fmt.Errorf("invalid assets backend: %s", val)
				}
				return nil
			},
		},
		&cli.StringFlag{
			Name:     "assets-uri",
			Usage:    "The location of the assets backend to connect to",
			EnvVars:  []string{"GRAM_ASSETS_URI"},
			Required: true,
		},
		&cli.StringFlag{
			Name:     "encryption-key",
			Usage:    "Key for App level AES encryption/decyryption",
			Required: true,
			EnvVars:  []string{"GRAM_ENCRYPTION_KEY"},
		},
		&cli.StringFlag{
			Name:     usersessions.JWTSigningKeyFlag,
			Usage:    "Key for JWT signing",
			Required: true,
			EnvVars:  []string{"GRAM_JWT_SIGNING_KEY"},
		},
		&cli.StringFlag{
			Name:    "openrouter-dev-key",
			Usage:   "Dev API key for OpenRouter (primarily for local development) - https://openrouter.ai/settings/keys",
			EnvVars: []string{"OPENROUTER_DEV_KEY"},
		},
		&cli.StringFlag{
			Name:     "tunnel-forward-token",
			Usage:    "Shared secret presented to the tunnel gateway forward listener to authenticate gram-server",
			Required: true,
			EnvVars:  []string{"GRAM_TUNNEL_FORWARD_TOKEN"},
		},
		&cli.StringSliceFlag{
			Name:    "tunnel-gateway-cidr-blocks",
			Usage:   "CIDR blocks the tunnel gateway advertise addresses live in (cluster pod range). Allowlisted past the guardian egress policy for tunnel forwards only; unset means tunnels to private addresses fail closed",
			EnvVars: []string{"GRAM_TUNNEL_GATEWAY_CIDR_BLOCKS"},
		},
		&cli.IntFlag{
			Name:    "public-tunnels-live-session-cap",
			Usage:   "Maximum concurrently tracked anonymous MCP sessions per tunnel (0 uses the built-in default)",
			EnvVars: []string{"GRAM_PUBLIC_TUNNELS_LIVE_SESSION_CAP"},
		},
		&cli.DurationFlag{
			Name:    "meta-member-call-timeout",
			Usage:   "Deadline for one gateway member upstream call, handshake included (0 uses the built-in default)",
			EnvVars: []string{"GRAM_META_MEMBER_CALL_TIMEOUT"},
		},
		&cli.StringFlag{
			Name:    "openrouter-provisioning-key",
			Usage:   "Provisioning key for OpenRouter to create new API keys for orgs - https://openrouter.ai/settings/provisioning-keys",
			EnvVars: []string{"OPENROUTER_PROVISIONING_KEY"},
		},
		&cli.StringFlag{
			Name:    "github-evidence-token",
			Usage:   "GitHub API token for MCP evidence repository lookups; unset falls back to the small unauthenticated per-IP budget, after which lookups land in evidence gaps",
			EnvVars: []string{"GRAM_GITHUB_EVIDENCE_TOKEN"},
		},
		&cli.StringFlag{
			Name:    "temporal-address",
			Usage:   "Address of the Temporal server",
			EnvVars: []string{"TEMPORAL_ADDRESS"},
		},
		&cli.StringFlag{
			Name:    "temporal-namespace",
			Usage:   "Namespace of the Temporal server",
			EnvVars: []string{"TEMPORAL_NAMESPACE"},
		},
		&cli.StringFlag{
			Name:    "temporal-task-queue",
			Usage:   "Task queue of the Temporal server",
			EnvVars: []string{"TEMPORAL_TASK_QUEUE"},
			Value:   "main",
		},
		&cli.StringFlag{
			Name:    "temporal-client-cert",
			Usage:   "Client cert of the Temporal server",
			EnvVars: []string{"TEMPORAL_CLIENT_CERT"},
		},
		&cli.StringFlag{
			Name:    "temporal-client-key",
			Usage:   "Client key of the Temporal server",
			EnvVars: []string{"TEMPORAL_CLIENT_KEY"},
		},
		&cli.BoolFlag{
			Name:    "dev-single-process",
			Usage:   "Run the server and worker in a single process for local development",
			EnvVars: []string{"GRAM_SINGLE_PROCESS"},
			Value:   false,
		},

		&cli.StringFlag{
			Name:     "pylon-verification-secret",
			Usage:    "The identity verification secret for pylon",
			EnvVars:  []string{"PYLON_VERIFICATION_SECRET"},
			Required: false,
		},
		&cli.StringFlag{
			Name:    "stripe-api-key",
			Usage:   "The Stripe API key",
			EnvVars: []string{"STRIPE_API_KEY"},
		},
		&cli.StringFlag{
			Name:    "stripe-webhook-secret",
			Usage:   "The Stripe webhook signing secret",
			EnvVars: []string{"STRIPE_WEBHOOK_SECRET"},
		},
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "stripe-price-id-tum",
			Aliases: []string{"stripe.price_id_tum"},
			Usage:   "The Stripe metered TUM price ID",
			EnvVars: []string{"STRIPE_PRICE_ID_TUM"},
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "stripe-meter-id-tum",
			Aliases: []string{"stripe.meter_id_tum"},
			Usage:   "The Stripe TUM billing meter ID",
			EnvVars: []string{"STRIPE_METER_ID_TUM"},
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "stripe-meter-event-name",
			Aliases: []string{"stripe.meter_event_name"},
			Usage:   "The Stripe TUM meter event name",
			EnvVars: []string{"STRIPE_METER_EVENT_NAME"},
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:    "stripe-portal-configuration-id",
			Aliases: []string{"stripe.portal_configuration_id"},
			Usage:   "The controlled Stripe customer portal configuration ID",
			EnvVars: []string{"STRIPE_PORTAL_CONFIGURATION_ID"},
		}),
		&cli.StringFlag{
			Name:     "polar-api-key",
			Usage:    "The polar API key",
			EnvVars:  []string{"POLAR_API_KEY"},
			Required: false,
		},
		&cli.StringFlag{
			Name:     "polar-webhook-secret",
			Usage:    "The polar webhook secret",
			EnvVars:  []string{"POLAR_WEBHOOK_SECRET"},
			Required: false,
		},
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:     "polar-product-id-free",
			Aliases:  []string{"polar.product_id_basic"},
			Usage:    "The product ID of the free tier in Polar",
			EnvVars:  []string{"POLAR_PRODUCT_ID_FREE"},
			Required: false,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:     "polar-product-id-pro",
			Aliases:  []string{"polar.product_id_pro"},
			Usage:    "The product ID of the pro tier in Polar",
			EnvVars:  []string{"POLAR_PRODUCT_ID_PRO"},
			Required: false,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:     "polar-meter-id-tool-calls",
			Aliases:  []string{"polar.meter_id_tool_calls"},
			Usage:    "The ID of the tool calls meter in Polar",
			EnvVars:  []string{"POLAR_METER_ID_TOOL_CALLS"},
			Required: false,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:     "polar-meter-id-servers",
			Aliases:  []string{"polar.meter_id_servers"},
			Usage:    "The ID of the servers meter in Polar",
			EnvVars:  []string{"POLAR_METER_ID_SERVERS"},
			Required: false,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:     "polar-meter-id-credits",
			Aliases:  []string{"polar.meter_id_credits"},
			Usage:    "The ID of the credits meter in Polar",
			EnvVars:  []string{"POLAR_METER_ID_CREDITS"},
			Required: false,
		}),
		altsrc.NewStringFlag(&cli.StringFlag{
			Name:     "polar-product-id-assistants",
			Aliases:  []string{"polar.product_id_assistants"},
			Usage:    "The product ID granting the assistants benefit in Polar (auto-attached on assistants-disposition signup)",
			EnvVars:  []string{"POLAR_PRODUCT_ID_ASSISTANTS"},
			Required: false,
		}),
		&cli.StringSliceFlag{
			Name:     "polar-product-ids-topup",
			Usage:    "Product IDs of one-time credit top-up packs in Polar",
			EnvVars:  []string{"POLAR_PRODUCT_IDS_TOPUP"},
			Required: false,
		},
		&cli.StringSliceFlag{
			Name:     "disallowed-cidr-blocks",
			Usage:    "List of CIDR blocks to block for SSRF protection",
			EnvVars:  []string{"GRAM_DISALLOWED_CIDR_BLOCKS"},
			Required: false,
		},
		&cli.PathFlag{
			Name:     "config-file",
			Usage:    "Path to a config file to load. Supported formats are JSON, TOML and YAML.",
			EnvVars:  []string{"GRAM_CONFIG_FILE"},
			Required: false,
		},
		&cli.StringFlag{
			Name:     "workos-endpoint",
			Usage:    "Base URL for WorkOS API calls. Leave unset for production (defaults to https://api.workos.com); set to the dev-idp's mock-workos mode for fully-local development.",
			EnvVars:  []string{"WORKOS_API_URL"},
			Required: false,
		},
		&cli.StringFlag{
			Name:     "loops-api-key",
			Usage:    "Loops API key for transactional emails (invite emails). Empty or 'unset' disables email sending.",
			EnvVars:  []string{"LOOPS_API_KEY"},
			Required: false,
		},
		&cli.StringFlag{
			Name:     "email-template-ids",
			Usage:    "JSON mapping of application email template keys to environment-specific Loops IDs",
			EnvVars:  []string{"GRAM_EMAIL_TEMPLATE_IDS"},
			Required: false,
		},
		&cli.StringFlag{
			Name:    "presidio-analyzer-url",
			Usage:   "Base URL of the Presidio Analyzer service (e.g. http://presidio-analyzer:3000). Empty disables PII scanning.",
			EnvVars: []string{"PRESIDIO_ANALYZER_URL"},
		},
		&cli.StringFlag{
			Name:     "workos-webhook-secret",
			Usage:    "WorkOS webhook signing secret for validating incoming webhook payloads",
			EnvVars:  []string{"WORKOS_WEBHOOK_SECRET"},
			Required: false,
		},
	}

	flags = append(flags, customDomainFlags()...)
	flags = append(flags, redisFlags()...)
	flags = append(flags, clickHouseFlags()...)
	flags = append(flags, functionsFlags()...)
	flags = append(flags, pluginsFlags()...)
	flags = append(flags, assistantRuntimeFlags()...)
	flags = append(flags, pulseMCPFlags()...)
	flags = append(flags, posthogFlags()...)
	flags = append(flags, svixFlags()...)
	flags = append(flags, riskReconcileFlags()...)
	flags = append(flags, gcpFlags()...)

	return &cli.Command{
		Name:  "start",
		Usage: "Start the Gram API server",
		Flags: flags,
		Action: func(c *cli.Context) error {
			serviceName := "gram-server"
			serviceEnv := c.String("environment")
			appinfo := o11y.PullAppInfo(c.Context)
			appinfo.Command = "server"
			logger := PullLogger(c.Context).With(
				attr.SlogComponent("server"),
				attr.SlogServiceName(serviceName),
				attr.SlogServiceVersion(shortGitSHA()),
				attr.SlogServiceEnv(serviceEnv),
			)
			slog.SetDefault(logger)

			customDomainARecords, err := customDomainARecordsFromCLI(c)
			if err != nil {
				return err
			}

			platformFixture, err := platformMCPLocalFixtureConfigFromCLI(serviceEnv, c.String("server-url"))
			if err != nil {
				return fmt.Errorf("invalid Platform MCP local fixture configuration: %w", err)
			}

			if serviceEnv == "local" {
				scanners.EnableRuleIDFormatEnforcement()
			}

			ctx, cancel := context.WithCancel(c.Context)
			defer cancel()

			shutdown, err := o11y.SetupOTelSDK(ctx, logger, o11y.SetupOTelSDKOptions{
				ServiceName:    serviceName,
				ServiceVersion: shortGitSHA(),
				GitSHA:         GitSHA,
				EnableTracing:  c.Bool("with-otel-tracing"),
				EnableMetrics:  c.Bool("with-otel-metrics"),
			})
			if err != nil {
				return fmt.Errorf("setup opentelemetry sdk: %w", err)
			}
			shutdownFuncs = append(shutdownFuncs, shutdown)

			tracerProvider := otel.GetTracerProvider()
			meterProvider := otel.GetMeterProvider()

			db, err := newDBClient(ctx, logger, meterProvider, c.String("database-url"), dbClientOptions{
				enableUnsafeLogging: c.Bool("unsafe-db-log"),
			})
			if err != nil {
				return fmt.Errorf("failed to connect to database: %w", err)
			}
			dbClose = db.Close

			chDB, shutdown, err := newClickhouseClient(ctx, logger, c)
			if err != nil {
				return fmt.Errorf("failed to connect to clickhouse database: %w", err)
			}
			clickhouseShutdown = shutdown

			riskFingerprinter, err := parseOptionalPepperKeyRing(ctx, logger, c.String("risk-fingerprint-pepper-keyring"))
			if err != nil {
				return err
			}

			err = o11y.StartObservers(meterProvider, db)
			if err != nil {
				return fmt.Errorf("failed to create observers: %w", err)
			}

			assetStorage, shutdown, err := newAssetStorage(ctx, logger, assetStorageOptions{
				assetsBackend: c.String("assets-backend"),
				assetsURI:     c.String("assets-uri"),
			})
			if err != nil {
				return fmt.Errorf("failed to initialize asset storage: %w", err)
			}
			shutdownFuncs = append(shutdownFuncs, shutdown)

			redisClient, err := newRedisClient(ctx, redisClientOptions{
				redisAddr:     c.String("redis-cache-addr"),
				redisPassword: c.String("redis-cache-password"),
				enableTracing: c.Bool("redis-enable-tracing"),
			})
			if err != nil {
				return fmt.Errorf("failed to connect to redis: %w", err)
			}

			guardianPolicy, err := newGuardianPolicy(c, logger, tracerProvider, meterProvider, redisClient)
			if err != nil {
				return err
			}

			pylonClient, err := pylon.NewPylon(logger, c.String("pylon-verification-secret"))
			if err != nil {
				return fmt.Errorf("failed to create pylon client: %w", err)
			}

			posthogClient := posthog.New(ctx, logger, c.String("posthog-api-key"), c.String("posthog-endpoint"), c.String("posthog-personal-api-key"))
			var featureFlags feature.Provider = posthogClient
			if c.String("environment") == "local" {
				featureFlags = newLocalFeatureFlags(ctx, logger, c.String("local-feature-flags-csv"))
			}

			workosClient, workosAvailable, err := newWorkOSClient(guardianPolicy, c)
			if err != nil {
				return fmt.Errorf("failed to create WorkOS client: %w", err)
			}
			var backgroundWorkOSClient activities.WorkOSClient = workosClient
			if !workosAvailable {
				backgroundWorkOSClient = workos.NewStubClient()
			}

			stripeClient, err := newStripeClient(ctx, logger, guardianPolicy, c)
			if err != nil {
				return fmt.Errorf("failed to create Stripe client: %w", err)
			}

			billingRepo, billingTracker, err := newBillingProvider(ctx, logger, tracerProvider, guardianPolicy, redisClient, posthogClient, stripeClient, c)
			if err != nil {
				return fmt.Errorf("failed to create billing provider: %w", err)
			}

			idpClientSecret := c.String("idp-client-secret")

			umClient := newIDPUserManagementClient(guardianPolicy, idpClientSecret, c)
			if umClient == nil {
				return fmt.Errorf("failed to create IDP user management client: idp-client-secret is required")
			}

			idpClient := identity.NewWorkOSAdapter(umClient)

			productFeatures := productfeatures.NewClient(logger, tracerProvider, db, redisClient)
			authzProvisioner := authz.NewProvisioner(db)

			identityResolver := identity.NewResolver(
				logger,
				tracerProvider,
				cache.NewRedisCacheAdapter(redisClient),
				c.String("idp-base-url"),
				c.String("idp-client-id"),
				idpClient,
				workosClient,
				orgRepo.New(db),
				userRepo.New(db),
				pylonClient,
				posthogClient,
				cache.SuffixNone,
			)

			sessionManager := sessions.NewManager(
				logger,
				tracerProvider,
				db,
				redisClient,
				cache.SuffixNone,
				idpClient,
				billingRepo,
				identityResolver,
			)

			chatSessionsManager := chatsessions.NewManager(logger, redisClient, c.String(usersessions.JWTSigningKeyFlag))

			encryptionClient, err := encryption.New(c.String("encryption-key"))
			if err != nil {
				return fmt.Errorf("failed to create encryption client: %w", err)
			}

			mcpMetadataRepo := mcpmetadata_repo.New(db)
			env := environments.NewEnvironmentEntries(logger, db, encryptionClient, mcpMetadataRepo)

			k8sClient, err := k8s.InitializeK8sClient(ctx, logger, c.String("environment"), c.String("custom-domain-k8s-namespace"), c.String("custom-domain-backend-service"))
			if err != nil {
				return fmt.Errorf("failed to create kubernetes client: %w", err)
			}

			temporalEnv, shutdown, err := newTemporalClient(logger, meterProvider, temporalClientOptions{
				address:      c.String("temporal-address"),
				namespace:    c.String("temporal-namespace"),
				taskQueue:    c.String("temporal-task-queue"),
				certPEMBlock: []byte(c.String("temporal-client-cert")),
				keyPEMBlock:  []byte(c.String("temporal-client-key")),
			})
			if err != nil {
				return fmt.Errorf("failed to create temporal client: %w", err)
			}

			if temporalEnv == nil {
				return errors.New("insufficient options to create temporal client")
			}
			shutdownFuncs = append(shutdownFuncs, shutdown)

			auditLogger := newAuditLogger()

			emailService, err := newEmailService(ctx, c, logger, guardianPolicy)
			if err != nil {
				return err
			}

			openRouterKeyRefresher := &background.OpenRouterKeyRefresher{TemporalEnv: temporalEnv}
			var openRouter interface {
				openrouter.Provisioner
				openrouter.SpendClient
			}
			if c.String("environment") == "local" {
				openRouter = openrouter.NewDevelopment(c.String("openrouter-dev-key"))
			} else {
				openRouter = openrouter.New(
					logger,
					tracerProvider,
					guardianPolicy,
					db,
					c.String("environment"),
					c.String("openrouter-provisioning-key"),
					openRouterKeyRefresher,
					productFeatures,
					billingTracker,
					encryptionClient,
				)
			}

			serverURL, err := url.Parse(c.String("server-url"))
			if err != nil {
				return fmt.Errorf("failed to parse server url: %w", err)
			}

			siteURL, err := url.Parse(c.String("site-url"))
			if err != nil {
				return fmt.Errorf("failed to parse site url: %w", err)
			}
			trialEmailNotifier := &background.TemporalTrialEmailNotifier{TemporalEnv: temporalEnv}
			loopsWorkflowClient := loops.NewWorkflowClient(ctx, logger, guardianPolicy, c.String("loops-api-key"))
			trialEmailsService := trialemails.NewService(db, loopsWorkflowClient, logger, siteURL.String())

			tigrisStore, shutdown, err := newTigrisStore(ctx, c, logger)
			if err != nil {
				return fmt.Errorf("failed to create tigris asset store: %w", err)
			}
			shutdownFuncs = append(shutdownFuncs, shutdown)

			functionsOrchestrator, shutdown, err := newFunctionOrchestrator(c, logger, tracerProvider, guardianPolicy, db, assetStorage, tigrisStore, encryptionClient)
			if err != nil {
				return fmt.Errorf("failed to create functions orchestrator: %w", err)
			}
			shutdownFuncs = append(shutdownFuncs, shutdown)
			runnerVersion := functions.RunnerVersion(conv.Default(strings.TrimPrefix(c.String("functions-runner-version"), "sha-"), GitSHA))

			slackClient := slack_client.NewSlackClient(guardianPolicy)

			logsEnabled := newFeatureChecker(logger, productFeatures, productfeatures.FeatureLogs)
			toolIOLogsEnabled := newFeatureChecker(logger, productFeatures, productfeatures.FeatureToolIOLogs)
			sessionCaptureEnabled := newFeatureChecker(logger, productFeatures, productfeatures.FeatureSessionCapture)
			challengeLoggingEnabled := authz.ChallengeLoggingEnabled(newFeatureChecker(logger, productFeatures, productfeatures.FeatureAuthzChallengeLogging))
			roleClient, err := newAccessRoleProvider(ctx, logger, guardianPolicy, c)
			if err != nil {
				return fmt.Errorf("failed to create access role provider: %w", err)
			}
			var (
				litellmTraceProcessor   *litellm.TraceProcessor
				litellmMetricProcessor  *litellm.MetricProcessor
				litellmService          *litellm.Service
				litellmHealthProcessor  *litellm.HealthProcessor
				telemetryLoggerShutdown func(context.Context) error
				publishersShutdown      func(context.Context) error
				pubsubShutdown          func(context.Context) error
			)
			shutdownFuncs = append(shutdownFuncs, func(ctx context.Context) error {
				var errs []error
				if litellmTraceProcessor != nil {
					if err := litellmTraceProcessor.Shutdown(ctx); err != nil {
						errs = append(errs, fmt.Errorf("shutdown LiteLLM trace processor: %w", err))
					}
				}
				if litellmMetricProcessor != nil {
					if err := litellmMetricProcessor.Shutdown(ctx); err != nil {
						errs = append(errs, fmt.Errorf("shutdown LiteLLM metric processor: %w", err))
					}
				}
				if litellmHealthProcessor != nil {
					if err := litellmHealthProcessor.Shutdown(ctx); err != nil {
						errs = append(errs, fmt.Errorf("shutdown LiteLLM health processor: %w", err))
					}
				}
				if telemetryLoggerShutdown != nil {
					errs = append(errs, telemetryLoggerShutdown(ctx))
				}
				if publishersShutdown != nil {
					errs = append(errs, publishersShutdown(ctx))
				}
				if pubsubShutdown != nil {
					errs = append(errs, pubsubShutdown(ctx))
				}
				return errors.Join(errs...)
			})

			_, psbroker, shutdown, err := newPubSubClient(ctx, c, logger)
			pubsubShutdown = shutdown
			if err != nil {
				return fmt.Errorf("failed to create pubsub client: %w", err)
			}

			publishers, shutdown, err := newPublishers(ctx, psbroker)
			publishersShutdown = shutdown
			if err != nil {
				return fmt.Errorf("failed to create publishers: %w", err)
			}
			authzEngine := authz.NewEngine(
				logger,
				db,
				challengeLoggingEnabled,
				roleClient,
				authz.EngineOpts{
					DevMode: c.String("environment") == "local",
				})

			telemetryLogPublisher := tm.NewLogPublisher(logger, tracerProvider, meterProvider, publishers.TelemetryLogs)

			telemLogger, shutdown := newTelemetryLogger(ctx, logger, tracerProvider, meterProvider, db, cache.NewRedisCacheAdapter(redisClient), chDB, logsEnabled, toolIOLogsEnabled, telemetryLogPublisher)
			telemetryLoggerShutdown = shutdown

			telemSvc := tm.NewService(logger, tracerProvider, db, chDB, sessionManager, chatSessionsManager, logsEnabled, sessionCaptureEnabled, posthogClient, authzEngine, featureFlags)

			// Wrap cache for hooks service in local development
			var hooksCache cache.Cache = cache.NewRedisCacheAdapter(redisClient)
			if c.String("environment") == "local" {
				hooksCache = hooks.NewLocalSessionCache(hooksCache, db)
			}

			// Turn frames are published from the writer, the one place every
			// row of a turn is persisted, so dashboard subscribers can render
			// a turn without polling for it.
			turnStream := chat.NewTurnStream(redisClient)
			chatWriter, chatWriterShutdown := chat.NewChatMessageWriter(logger, db, assetStorage)
			chatWriter = chatWriter.WithTurnStream(turnStream)
			shutdownFuncs = append(shutdownFuncs, chatWriterShutdown)

			captureStrategy := chat.NewChatMessageCaptureStrategy(logger, meterProvider, db, chatWriter)

			// One signaler for every efficacy producer in this process: the chat
			// transcript writer below, the hooks ingest paths and the assistant
			// skills_load tool all wake the same per-project coordinator.
			//
			// Throttled because every producer signals on every durable write —
			// a single chat turn is many messages — and a wake carries no payload,
			// so a burst of them and one of them ask the coordinator for exactly
			// the same pass. The trailing edge is what keeps the last write of a
			// burst from being the one that goes unanswered.
			// efficacySignaler.Shutdown is NOT registered as a shutdownFunc, for
			// the same reason riskSignaler's is not: see below.
			efficacySignaler := background.NewThrottledSignaler(
				&background.TemporalSkillEfficacySignaler{TemporalEnv: temporalEnv, Logger: logger},
				background.SkillEfficacySignalCooldown,
				logger.With(attr.SlogComponent("skill-efficacy")),
			)
			chatWriter.AddObserver(efficacy.NewObserver(logger, efficacySignaler))

			// Chat analysis rides the same durable-write event as efficacy, with
			// its own coordinator and the same throttle/flush rationale.
			chatAnalysisSignaler := background.NewThrottledSignaler(
				&background.TemporalChatAnalysisSignaler{TemporalEnv: temporalEnv, Logger: logger},
				background.ChatAnalysisSignalCooldown,
				logger.With(attr.SlogComponent("chat-analysis")),
			)
			chatWriter.AddObserver(analysis.NewObserver(logger, chatAnalysisSignaler))

			completionsClient := openrouter.NewUnifiedClient(
				logger,
				guardianPolicy,
				openRouter,
				modelkeys.NewResolver(db, encryptionClient, openRouter),
				captureStrategy,
				chat.NewDefaultUsageTrackingStrategy(db, logger, billingTracker),
				&background.TemporalChatTitleGenerator{TemporalEnv: temporalEnv},
				telemLogger,
			)

			memorySvc := memory.NewMemoryService(
				logger,
				tracerProvider,
				meterProvider,
				db,
				completionsClient,
				auditLogger,
			)

			ragService := rag.NewToolsetVectorStore(logger, tracerProvider, db, completionsClient)
			mcpRegistryClient, err := newMCPRegistryClient(logger, tracerProvider, guardianPolicy, mcpRegistryClientOptions{
				pulseTenantID: c.String("pulse-registry-tenant"),
				pulseAPIKey:   conv.NewSecret([]byte(c.String("pulse-registry-api-key"))),
				cacheImpl:     cache.NewRedisCacheAdapter(redisClient),
			})
			if err != nil {
				return fmt.Errorf("failed to create mcp registry client: %w", err)
			}

			assistantTokenManager := assistanttokens.New(c.String(usersessions.JWTSigningKeyFlag), db, authzEngine)
			assistantRuntime, err := newAssistantRuntime(ctx, logger, tracerProvider, c, guardianPolicy, db, serverURL)
			if err != nil {
				return err
			}
			shadowMCPClient := shadowmcp.NewClient(logger, db, cache.NewRedisCacheAdapter(redisClient), serverURL)
			triggerApp := newTriggersApp(logger, db, encryptionClient, temporalEnv, telemLogger, auditLogger, serverURL, siteURL, slackClient)

			platformFeatureChecker := productFeatures.PlatformFeatureCheck

			memoryTools := platformtoolsruntime.MemoryExternalTools(memorySvc)
			feedbackRecorder := feedbackrecorder.NewRecorder(db, logger, &background.TemporalSkillSuggestionSignaler{TemporalEnv: temporalEnv, Logger: logger, StartDelay: 0})
			skillTools := platformtoolsruntime.AssistantSkillTools(logger, db, feedbackRecorder, platformskills.WithEfficacySignaler(efficacySignaler))
			triggerTools := platformtoolsruntime.TriggerExternalTools(db, triggerApp, auditLogger)
			// mcpService captures this map by reference now; the remaining
			// insights tools (chat/orgs/risk/deployments/skills) are merged in once
			// their backing services exist further down.
			managedInsightsTools := append([]platformtools.ExternalTool{}, platformtoolsruntime.ManagedAssistantLogsTools(telemSvc)...)
			platformToolsets := map[string]platformtools.Toolset{}
			// Runner-callable platform tools the runtime must be able to execute
			// (trigger tools are wired separately via WithTriggerTools).
			assistantPlatformExtras := append([]platformtools.ExternalTool{}, memoryTools...)
			assistantPlatformExtras = append(assistantPlatformExtras, skillTools...)

			platformSvc := platformtoolsruntime.NewService(
				logger,
				db,
				telemSvc,
				auditLogger,
				platformtoolsruntime.WithTriggerTools(triggerApp),
				platformtoolsruntime.WithSlackHTTPClient(guardianPolicy.PooledClient()),
				platformtoolsruntime.WithFileURLMinting(encryptionClient, serverURL),
				platformtoolsruntime.WithFeatureChecker(platformFeatureChecker),
				platformtoolsruntime.WithExternalTools(assistantPlatformExtras),
			)

			remoteChallengeManager := remotesessions.NewChallengeManager(
				logger,
				tracerProvider,
				meterProvider,
				db,
				encryptionClient,
				guardianPolicy,
				cache.NewRedisCacheAdapter(redisClient),
				serverURL,
			)

			toolDispositionCache := mcpservers.NewToolDispositionCache(logger, db, cache.NewRedisCacheAdapter(redisClient))
			var platformSelectedUseRecorder toolcallobserver.SuccessRecorder = platformmcp.NewSelectedUseRecorder(db)
			remoteProxyManager := remotemcp.NewProxyManager(
				logger,
				tracerProvider,
				meterProvider,
				guardianPolicy,
				authzEngine,
				posthogClient,
				telemLogger,
				billingRepo,
				billingTracker,
				toolDispositionCache,
				platformSelectedUseRecorder,
				toolfilter.NewSessionToolWitnessStore(logger, cache.NewRedisCacheAdapter(redisClient)),
			)

			// guardian.WithAllowedCIDRBlocks silently drops invalid CIDRs, so a
			// typo here would strand tunnels fail-closed with no signal. Reject
			// misconfiguration at startup instead.
			tunnelGatewayCIDRs := c.StringSlice("tunnel-gateway-cidr-blocks")
			for _, cidr := range tunnelGatewayCIDRs {
				if _, _, err := net.ParseCIDR(cidr); err != nil {
					return fmt.Errorf("invalid tunnel gateway CIDR block %q: %w", cidr, err)
				}
			}

			mcpService := mcp.NewService(
				logger,
				tracerProvider,
				meterProvider,
				db,
				sessionManager,
				chatSessionsManager,
				env,
				posthogClient,
				featureFlags,
				serverURL,
				siteURL,
				encryptionClient,
				cache.NewRedisCacheAdapter(redisClient),
				guardianPolicy,
				functionsOrchestrator,
				billingTracker,
				billingRepo,
				telemLogger,
				telemSvc,
				ragService,
				triggerApp,
				temporalEnv,
				authzEngine,
				assistantTokenManager,
				shadowMCPClient,
				auditLogger,
				assistantPlatformExtras,
				platformFeatureChecker,
				platformToolsets,
				identityResolver,
				usersessions.NewSigner(c.String(usersessions.JWTSigningKeyFlag)),
				remoteChallengeManager,
				remoteProxyManager,
				route.NewRedis(redisClient),
				c.String("tunnel-forward-token"),
				tunnelGatewayCIDRs,
				redisClient,
				mcp.TunnelPublicConfig{
					SessionTTL:         0,
					LiveSessionCap:     c.Int("public-tunnels-live-session-cap"),
					InitializeRate:     ratelimit.Rate{Tokens: 0, Interval: 0, Burst: 0},
					RequestRate:        ratelimit.Rate{Tokens: 0, Interval: 0, Burst: 0},
					MaxRequestLifetime: 0,
				},
				mcp.MetaRuntimeConfig{
					MemberCallTimeout: c.Duration("meta-member-call-timeout"),
				},
			)

			chatClient := chat.NewAgenticChatClient(
				logger,
				db,
				env,
				cache.NewRedisCacheAdapter(redisClient),
				completionsClient,
				mcpclient.NewInternalMCPClient(mcpService),
			)
			contextWindowResolver := openrouter.NewContextWindowResolver(logger, guardianPolicy, cache.NewRedisCacheAdapter(redisClient))
			chatService := chat.NewService(logger, tracerProvider, db, sessionManager, chatSessionsManager, openRouter, chatClient, contextWindowResolver, posthogClient, telemSvc, assetStorage, authzEngine, assistantTokenManager, billingRepo, auditLogger).
				WithTurnStream(turnStream)
			assistantsCore := assistants.NewServiceCore(logger, tracerProvider, meterProvider, db, guardianPolicy, encryptionClient, assistantRuntime, slackClient, assistantTokenManager, serverURL, telemLogger, contextWindowResolver, auditLogger)
			assistantsCore.SetWakeCanceller(triggerApp)
			assistantsCore.SetDashboardIngestor(triggerApp)
			assistantsCore.SetChatMessageWriter(chatWriter)
			assistantsCore.SetAssetStorage(assetStorage)
			assistantsCore.SetAssetSigningKey(c.String(usersessions.JWTSigningKeyFlag))
			assistantsCore.SetSlackImageInlining(env, slackapi.NewClient("", guardianPolicy.PooledClient()))
			assistantsCore.SetFeatureProvider(featureFlags)
			assistantsSvc := assistants.NewService(logger, tracerProvider, meterProvider, db, sessionManager, authzEngine, assistantsCore, &background.AssistantWorkflowSignaler{TemporalEnv: temporalEnv}, ratelimit.NewRedisStore(redisClient))
			triggerApp.RegisterDispatcher(assistantsSvc)

			mcpMetadataService := mcpmetadata.NewService(logger, tracerProvider, db, sessionManager, serverURL, siteURL, cache.NewRedisCacheAdapter(redisClient), authzEngine, auditLogger)

			otelForwardClient := otelforwarding.NewClient(logger, db, encryptionClient, cache.NewRedisCacheAdapter(redisClient))
			otelForwarder := otelforwarding.NewForwarder(logger, tracerProvider, meterProvider, guardianPolicy)
			otelForwarder.Start(ctx)
			shutdownFuncs = append(shutdownFuncs, func(ctx context.Context) error {
				otelForwarder.Shutdown(ctx)
				return nil
			})
			litellmCalls := callcache.New(cache.NewRedisCacheAdapter(redisClient))
			litellmTraceProcessor = litellm.NewTraceProcessor(logger, meterProvider, telemLogger, litellmCalls)
			litellmMetricProcessor = litellm.NewMetricProcessor(logger, meterProvider, telemLogger)
			litellmHealthProcessor = litellm.NewHealthProcessor(logger, db)
			litellmInstanceResolver := litellm.NewInstanceResolver(logger, db)
			litellmTraceProcessor.SetInstanceResolver(litellmInstanceResolver)
			litellmMetricProcessor.SetInstanceResolver(litellmInstanceResolver)
			litellmTraceProcessor.Start(ctx)
			litellmMetricProcessor.Start(ctx)
			litellmHealthProcessor.Start(ctx)

			svixClient, shutdown, err := newSvixClient(c, logger, guardianPolicy)
			if shutdown != nil {
				shutdownFuncs = append(shutdownFuncs, shutdown)
			}
			if err != nil {
				return fmt.Errorf("failed to create svix webhook sender: %w", err)
			}

			// Construct the GitHub App client once; share with the plugin publish
			// flow and the marketplace proxy so they hit the same token cache and
			// the same App identity. nil when the App isn't configured.
			var ghClient *ghclient.Client
			if appID, key := c.Int64("plugins-github-app-id"), c.String("plugins-github-private-key"); appID != 0 && key != "" {
				ghClient, err = ghclient.NewClient(appID, []byte(key), guardianPolicy.Client())
				if err != nil {
					return fmt.Errorf("create github app client: %w", err)
				}
			}

			// Marketplace proxy routes (URL-based marketplace.json + git Smart
			// HTTP for plugin source clones). Mounted via the outermost
			// mux.Use middleware so /m/ and /p/ paths short-circuit the Goa
			// mux. Public base URL is server-url by definition - the proxy
			// lives on this server, so the plugin sources we embed in the
			// rendered manifest must point back at it. nil when no App is
			// configured.
			//
			// We wrap the proxy with the recovery middleware before mounting:
			// the dispatch happens inside the outermost mux.Use, ahead of the
			// chain-level recovery, so without this wrap a panic in any
			// marketplace handler (or the DB resolver) would crash the
			// server process.
			var (
				marketplaceServer      *marketplace.Server
				localMarketplaceServer *marketplace.LocalServer
				marketplaceRoutes      http.Handler
			)
			if ghClient != nil {
				marketplaceServer = marketplace.NewServer(
					marketplace.NewDBResolver(db, ghClient),
					guardianPolicy.Client(),
					logger,
				)
				marketplaceRoutes = middleware.NewRecovery(logger)(marketplaceServer.Routes())
				logger.InfoContext(ctx, "marketplace proxy: enabled",
					attr.SlogServerAddress(c.String("address")),
				)
			} else if platformFixture == nil {
				logger.InfoContext(ctx, "marketplace proxy: disabled (no github app configured)")
			}

			// Hooks binary artifacts (checksum-verifying proxy in front of the
			// pinned GitHub release). Served from this domain so bootstrap
			// scripts never need GitHub egress; mounted like the marketplace
			// proxy — dispatched ahead of the Goa mux with its own recovery
			// wrap — but not gated on the GitHub App, since release assets are
			// public.
			hooksArtifactServer := plugins.NewHooksArtifactServer(logger, guardianPolicy.Client())
			hooksArtifactRoutes := middleware.NewRecovery(logger)(hooksArtifactServer.Routes())

			mux := goahttp.NewMuxer()
			mux.Use(func(h http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method == http.MethodGet && r.URL.Path == "/healthz" {
						w.WriteHeader(http.StatusOK)
						return
					}
					if marketplaceServer != nil && marketplaceServer.IsMarketplaceRoute(r) {
						marketplaceRoutes.ServeHTTP(w, r)
						return
					}
					if localMarketplaceServer != nil && localMarketplaceServer.IsMarketplaceRoute(r) {
						marketplaceRoutes.ServeHTTP(w, r)
						return
					}
					if hooksArtifactServer.IsHooksReleaseRoute(r) {
						hooksArtifactRoutes.ServeHTTP(w, r)
						return
					}

					h.ServeHTTP(w, r)
				})
			})
			// Drop client-supplied baggage on public routes before otelhttp
			// extracts inbound trace context, so untrusted baggage never enters
			// the request context.
			mux.Use(middleware.DropInboundOTelBaggage)
			mux.Use(func(h http.Handler) http.Handler {
				return otelhttp.NewHandler(h, "http",
					otelhttp.WithServerName("gram"),
					// Public MCP/OAuth routes are reachable by any third party, so
					// their inbound trace context is potentially untrusted input.
					// Treat them as OTel public endpoints: start a fresh root span
					// and record the inbound context as a span link rather than
					// adopting it as the parent. Trusted first-party routes (/rpc,
					// /admin) keep parent-child continuity.
					otelhttp.WithPublicEndpointFn(middleware.IsOTelPublicEndpoint),
				)
			})
			mux.Use(middleware.RouteLabelerMiddleware)
			// Must stay below otelhttp: they stamp attributes onto the span
			// otelhttp opened for the request.
			mux.Use(middleware.HookDeviceTelemetry)
			mux.Use(middleware.MCPProtocolVersionTelemetry)
			mux.Use(middleware.NewHTTPLoggingMiddleware(logger))
			mux.Use(middleware.NewRecovery(logger))
			mux.Use(middleware.CORSMiddleware(c.String("environment"), c.String("server-url"), chatSessionsManager))
			mux.Use(customdomains.Middleware(logger, db, c.String("environment"), serverURL))
			mux.Use(middleware.SessionMiddleware)
			mux.Use(middleware.RBACOverrideMiddleware())
			// LiteLLM dispatch must run before OTLP forwarding: LiteLLM ingest
			// is excluded from outbound forwarding, and the canonical metrics
			// path is shared with harness telemetry.
			mux.Use(litellm.OTLPMetricsDispatch(func() *litellm.Service { return litellmService }))
			mux.Use(otelforwarding.Middleware(logger, otelForwardClient, otelForwarder))

			// Reuse the same Presidio client the worker uses for offline analysis
			// so the runtime hook scanner can flag/redact PII inputs too.
			var hookPIIScanner risk_analysis.PIIScanner
			if presidioURL := c.String("presidio-analyzer-url"); presidioURL != "" {
				hookPIIScanner = risk_analysis.NewPresidioClient(presidioURL, tracerProvider, meterProvider, logger)
			}

			// L1 prompt-injection engine is the LLM judge (POC-193). A completions
			// client is always constructed, so the judge is always available.
			hookJudgeLimiter := openrouter.NewJudgeRateLimiter(ratelimit.NewRedisStore(redisClient))
			hookPIScanner := promptinjection.NewScanner(logger, piopenrouter.New(logger, tracerProvider, meterProvider, completionsClient, hookJudgeLimiter).Classify)

			hookPromptJudge := ppopenrouter.New(logger, tracerProvider, meterProvider, completionsClient, hookJudgeLimiter).Evaluate
			hookPromptPolicyScanner := promptpolicy.NewScanner(logger, hookPromptJudge)
			celEngine, err := celenv.New()
			if err != nil {
				return fmt.Errorf("create cel engine: %w", err)
			}
			builtinPresets, err := presetlib.New()
			if err != nil {
				return fmt.Errorf("load built-in exclusion library: %w", err)
			}
			customRulesScanner, err := customruleanalyzer.NewScanner(db)
			if err != nil {
				return fmt.Errorf("create custom rules scanner: %w", err)
			}
			riskScanner, err := risk.NewScanner(logger, tracerProvider, meterProvider, db, customRulesScanner, hookPIIScanner, hookPIScanner, hookPromptPolicyScanner, featureFlags, celEngine)
			if err != nil {
				return fmt.Errorf("create risk scanner: %w", err)
			}
			policyBypass := risk.NewPolicyBypassEvaluator(logger, db)

			spendCelEngine, err := spendcelenv.New()
			if err != nil {
				return fmt.Errorf("create spend rules cel engine: %w", err)
			}
			spendGate, err := spendrules.NewGate(logger, cache.NewRedisCacheAdapter(redisClient), spendCelEngine)
			if err != nil {
				return fmt.Errorf("create spend gate: %w", err)
			}

			about.Attach(mux, about.NewService(logger, tracerProvider, guardianPolicy))
			platformslack.NewFileProxy(logger, encryptionClient, guardianPolicy.PooledClient()).Attach(mux)
			external.AttachWebhookHandler(mux, external.NewWebhookHandler(logger, tracerProvider, newWorkOSWebhooksClient(c), temporalEnv))
			roleManager := access.NewRoleManager(logger, db, roleClient, auditLogger)
			access.Attach(mux, access.NewService(logger, tracerProvider, db, chDB, sessionManager, roleManager, authzEngine, auditLogger, emailService, siteURL, telemSvc))
			agent.Attach(mux, agent.NewService(logger, tracerProvider, db, sessionManager, authzEngine, auditLogger, productFeatures, serverURL.String(), assetStorage))
			assistants.Attach(mux, assistantsSvc)
			assistantmemories.Attach(mux, assistantmemories.NewService(
				logger,
				tracerProvider,
				db,
				sessionManager,
				authzEngine,
				memorySvc,
			))
			businessmemory.Attach(mux, businessmemory.NewService(
				logger,
				tracerProvider,
				db,
				sessionManager,
				authzEngine,
				completionsClient,
			))
			// identityMapRefreshSignaler.Shutdown is NOT registered as a
			// shutdownFunc, for the same reason riskSignaler's is not: it is
			// flushed synchronously in the drain goroutine below, while the
			// Temporal client is still open.
			identityMapRefreshSignaler := background.NewIdentityMapRefreshSignaler(temporalEnv, logger)
			hooksService := hooks.NewService(
				logger,
				db,
				tracerProvider,
				meterProvider,
				telemLogger,
				sessionManager,
				hooksCache,
				chatClient,
				temporalEnv,
				authzEngine,
				auditLogger,
				productFeatures,
				&background.TemporalChatTitleGenerator{TemporalEnv: temporalEnv},
				riskScanner,
				hookPIScanner,
				policyBypass,
				spendGate,
				shadowMCPClient,
				chatWriter,
				efficacySignaler,
				&background.TemporalSkillSuggestionSignaler{TemporalEnv: temporalEnv, Logger: logger, StartDelay: 0},
				identityMapRefreshSignaler,
				serverURL,
				siteURL,
				c.String("jwt-signing-key"),
			)
			hooks.Attach(mux, hooksService)
			litellmService = litellm.NewService(logger, tracerProvider, db, chDB, sessionManager, authzEngine, hooksService, litellmCalls, litellmTraceProcessor, litellmMetricProcessor, litellmHealthProcessor, litellmInstanceResolver, auditLogger, c.String("environment"))
			litellm.Attach(mux, litellmService)
			aiintegrations.Attach(mux, aiintegrations.NewService(logger, tracerProvider, db, sessionManager, authzEngine, auditLogger, encryptionClient, &background.TemporalAIUsagePoller{TemporalEnv: temporalEnv}))
			deviceintegrations.Attach(mux, deviceintegrations.NewService(logger, tracerProvider, db, sessionManager, authzEngine, auditLogger, encryptionClient, guardianPolicy, &background.DeviceIntegrationSyncTrigger{TemporalEnv: temporalEnv, Logger: logger}, featureFlags))
			modelkeys.Attach(mux, modelkeys.NewService(logger, tracerProvider, db, sessionManager, authzEngine, encryptionClient, openRouter, productFeatures, auditLogger))
			otelforwarding.Attach(mux, otelforwarding.NewService(logger, tracerProvider, db, sessionManager, authzEngine, auditLogger, otelForwardClient))
			auditapi.Attach(mux, auditapi.NewService(logger, tracerProvider, db, sessionManager, authzEngine))
			auth.Attach(mux, auth.NewService(
				logger,
				tracerProvider,
				db,
				sessionManager,
				identityResolver,
				auth.AuthConfigurations{
					IDPBaseURL:        c.String("idp-base-url"),
					GramServerURL:     c.String("server-url"),
					SignInRedirectURL: auth.FormSignInRedirectURL(c.String("site-url")),
					Environment:       c.String("environment"),
				},
				authzEngine,
				billingRepo,
				&background.TemporalAssistantsSubscriptionCancelScheduler{TemporalEnv: temporalEnv},
				posthogClient,
				cache.NewRedisCacheAdapter(redisClient),
				authzProvisioner,
				productfeatures.SeedOrganizationDefaultsTx,
				productfeatures.SeedEnterpriseTrialBundleTx,
				auditLogger,
				trialEmailNotifier,
			))
			organizationsService := organizations.NewService(logger, tracerProvider, db, sessionManager, workosClient, identityResolver, productFeatures, telemetryrepo.New(chDB), authzEngine, emailService, trialEmailNotifier, serverURL.String(), siteURL.String(), auditLogger, svixClient)
			organizations.Attach(mux, organizationsService)
			pluginsGitHub, err := plugins.NewGitHubConfig(plugins.GitHubConfigInput{
				Client:         ghClient,
				Org:            c.String("plugins-github-org"),
				InstallationID: c.Int64("plugins-github-installation-id"),
			})
			if err != nil {
				return fmt.Errorf("plugins github config: %w", err)
			}
			var localPublisher *localfixture.InMemoryGitHubPublisher
			if c.String("environment") == "local" && pluginsGitHub == nil {
				localPublisher, err = localfixture.NewPersistentGitHubPublisher(filepath.Join(c.String("assets-uri"), "local-plugin-marketplaces"))
				if err != nil {
					return fmt.Errorf("create local plugin publisher: %w", err)
				}
				pluginsGitHub = &plugins.GitHubConfig{
					Client:         localPublisher,
					Org:            "local-fixture",
					InstallationID: 1,
				}
				localMarketplaceServer = marketplace.NewLocalServer(
					marketplace.NewLocalDBResolver(db),
					func(ctx context.Context, owner, repo string) (map[string][]byte, error) {
						files, err := localPublisher.MainBranchFiles(ctx, owner, repo)
						if errors.Is(err, ghclient.ErrRepoNotFound) {
							return nil, marketplace.ErrNotFound
						}
						if err != nil {
							return nil, fmt.Errorf("read local marketplace main branch: %w", err)
						}
						return files, nil
					},
					logger,
				)
				marketplaceRoutes = middleware.NewRecovery(logger)(localMarketplaceServer.Routes())
				logger.InfoContext(ctx, "GitHub publishing for plugins: using local fixture publisher")
				logger.InfoContext(ctx, "marketplace proxy: using local fixture repository")
			}

			projects.Attach(mux, projects.NewService(logger, tracerProvider, db, sessionManager, authzEngine, auditLogger, temporalEnv, pluginsGitHub != nil))
			packages.Attach(mux, packages.NewService(logger, tracerProvider, db, sessionManager, authzEngine))

			var pluginPublisher *plugins.Service
			platformAdmission := platformmcp.NewAdmissionChecker(
				productFeatures,
				featureFlags,
				platformmcp.NewPostgresNewModelEligibility(db),
			)
			if pluginsGitHub != nil {
				logger.InfoContext(ctx, "GitHub publishing for plugins: enabled")
				pluginPublisher = plugins.NewPublisher(logger, db, auditLogger, pluginsGitHub, c.String("environment"), c.String("server-url"), featureFlags, platformAdmission)
				if localPublisher != nil {
					if err := restoreLocalPluginRepositories(ctx, logger, db, localPublisher, pluginPublisher); err != nil {
						return fmt.Errorf("restore local plugin repositories: %w", err)
					}
				}
			} else {
				logger.InfoContext(ctx, "GitHub publishing for plugins: disabled")
			}
			pluginsSvc := plugins.NewService(logger, tracerProvider, db, sessionManager, cache.NewRedisCacheAdapter(redisClient), authzEngine, auditLogger, pluginsGitHub, c.String("environment"), c.String("server-url"), featureFlags, platformAdmission)
			plugins.Attach(mux, pluginsSvc)
			productfeatures.Attach(mux, productfeatures.NewService(logger, tracerProvider, db, sessionManager, redisClient, authzEngine, auditLogger))
			skillefficacy.Attach(mux, skillefficacy.NewService(logger, tracerProvider, db, sessionManager, authzEngine, productFeatures, auditLogger, telemetryrepo.New(chDB)))
			// The manual trigger bypasses the write-throttled signaler on purpose:
			// an admin pressing "run now" wants the coordinator woken immediately,
			// not coalesced into the chat-write cooldown.
			chatanalysis.Attach(mux, chatanalysis.NewService(logger, tracerProvider, db, sessionManager, authzEngine, auditLogger,
				&background.TemporalChatAnalysisSignaler{TemporalEnv: temporalEnv, Logger: logger}))
			openrouterkeys.Attach(mux, openrouterkeys.NewService(logger, tracerProvider, db, sessionManager, authzEngine, auditLogger, openRouter, encryptionClient))
			skillsService := skills.NewService(logger, tracerProvider, db, sessionManager, authzEngine, productFeatures, auditLogger,
				&background.TemporalSkillSuggestionSignaler{TemporalEnv: temporalEnv, Logger: logger, StartDelay: 0}, siteURL)
			skills.Attach(mux, skillsService)
			toolsetsSvc := toolsets.NewService(logger, tracerProvider, db, sessionManager, cache.NewRedisCacheAdapter(redisClient), authzEngine, auditLogger, temporalEnv, pluginsGitHub != nil)
			toolsets.Attach(mux, toolsetsSvc)
			integrations.Attach(mux, integrations.NewService(logger, tracerProvider, db, sessionManager, authzEngine))
			templates.Attach(mux, templates.NewService(logger, tracerProvider, db, sessionManager, toolsetsSvc, authzEngine, auditLogger))
			assetsService := assets.NewService(logger, tracerProvider, guardianPolicy, db, sessionManager, chatSessionsManager, assetStorage, c.String(usersessions.JWTSigningKeyFlag), authzEngine, auditLogger)
			assets.Attach(mux, assetsService)
			deploymentsService := deployments.NewService(logger, tracerProvider, db, temporalEnv, sessionManager, assetStorage, posthogClient, siteURL, mcpRegistryClient, authzEngine, auditLogger)
			deployments.Attach(mux, deploymentsService)
			keys.Attach(mux, keys.NewService(logger, tracerProvider, db, sessionManager, c.String("environment"), authzEngine, auditLogger))
			// Hoisted so the services that authenticate as a customer's GCP identity
			// share one identity: they then agree on which impersonation targets are
			// refused, and probe for Gram's own service account once between them
			// rather than once each.
			gcpIdentity := newGCPIdentity(ctx, logger, c)
			kmsSigningClients, err := newKMSSigningClients(ctx, logger, c)
			if err != nil {
				return fmt.Errorf("build kms signing client factory: %w", err)
			}
			externalcredentials.Attach(mux, externalcredentials.NewService(logger, tracerProvider, meterProvider, db, sessionManager, authzEngine, auditLogger, gcpIdentity, productFeatures, ratelimit.NewRedisStore(redisClient)))
			externalkeys.Attach(mux, externalkeys.NewService(logger, tracerProvider, meterProvider, db, sessionManager, authzEngine, auditLogger, gcpIdentity, kmsSigningClients, productFeatures, ratelimit.NewRedisStore(redisClient)))
			jsonwebkeysets.Attach(mux, jsonwebkeysets.NewService(logger, tracerProvider, meterProvider, db, sessionManager, authzEngine, auditLogger, gcpIdentity, kmsSigningClients, productFeatures, ratelimit.NewRedisStore(redisClient)))
			cliauth.Attach(mux, cliauth.NewService(logger, tracerProvider, db, sessionManager, authzEngine, redisClient, c.String("environment")))
			chatsessionssvc.Attach(mux, chatsessionssvc.NewService(logger, tracerProvider, db, sessionManager, chatSessionsManager, authzEngine))
			environments.Attach(mux, environments.NewService(logger, tracerProvider, db, sessionManager, encryptionClient, authzEngine, auditLogger))
			upstreamRevoker := remotesessions.NewUpstreamRevoker(logger, tracerProvider, meterProvider, db, encryptionClient, guardianPolicy)
			mcpServersService := mcpservers.NewService(logger, tracerProvider, db, sessionManager, authzEngine, auditLogger, temporalEnv, toolDispositionCache, pluginsGitHub != nil, assetsService, upstreamRevoker)
			mcpservers.Attach(mux, mcpServersService)
			mcpendpoints.Attach(mux, mcpendpoints.NewService(logger, tracerProvider, db, sessionManager, authzEngine, auditLogger, temporalEnv, pluginsGitHub != nil))
			metamcp.Attach(mux, metamcp.NewService(logger, tracerProvider, db, sessionManager, authzEngine, auditLogger, temporalEnv))
			remoteSessionsCache := cache.NewRedisCacheAdapter(redisClient)
			remoteSessionsService := remotesessions.NewService(logger, tracerProvider, meterProvider, db, sessionManager, authzEngine, encryptionClient, env, guardianPolicy, auditLogger, serverURL, remotesessions.NewRefreshService(logger, db, encryptionClient, guardianPolicy, remoteSessionsCache))
			usersessions.Attach(mux, usersessions.NewService(logger, tracerProvider, meterProvider, db, sessionManager, chatSessionsManager, authzEngine, auditLogger, guardianPolicy, encryptionClient, usersessions.NewSigner(c.String(usersessions.JWTSigningKeyFlag)), serverURL.String(), ratelimit.NewRedisStore(redisClient)))
			tokenexchange.Attach(mux, tokenexchange.NewService(logger, tracerProvider, db, sessionManager, authzEngine, c.String("environment")))
			remotesessions.Attach(mux, remoteSessionsService)
			remotemcp.Attach(mux, remotemcp.NewService(logger, tracerProvider, db, sessionManager, encryptionClient, authzEngine, guardianPolicy, auditLogger, mcpServersService))
			unproxiedmcp.Attach(mux, unproxiedmcp.NewService(logger, tracerProvider, db, sessionManager, authzEngine, guardianPolicy, auditLogger))
			tunneledmcp.Attach(mux, tunneledmcp.NewService(logger, tracerProvider, db, sessionManager, authzEngine, auditLogger, route.NewRedis(redisClient), redisClient))
			xmcp.Attach(mux, xmcp.NewService(logger, db, encryptionClient, mcpService), mcpMetadataService)
			triggers.Attach(mux, triggers.NewService(logger, tracerProvider, db, sessionManager, authzEngine, triggerApp, auditLogger))
			tools.Attach(mux, tools.NewService(logger, tracerProvider, db, sessionManager, authzEngine, platformFeatureChecker, assistantPlatformExtras))
			resources.Attach(mux, resources.NewService(logger, tracerProvider, db, sessionManager, authzEngine))
			// One probe serves both the authority and tool-declarations slots:
			// they are two views of the same remote prober.
			remoteProber := remoteprobe.New(logger, guardianPolicy)
			mcpApprovalService := mcpapproval.NewService(logger, tracerProvider, db, sessionManager, authzEngine, featureFlags, auditLogger,
				mcpapprovalevidence.NewAssembler(
					packagemeta.NewClient(guardianPolicy.PooledClient()),
					repometa.NewClient(guardianPolicy.PooledClient(), repometa.WithToken(c.String("github-evidence-token"))),
					mcpapprovaladvisories.NewClient(guardianPolicy.PooledClient()),
					domainmeta.NewClient(guardianPolicy.PooledClient()),
					telemetryrepo.New(chDB),
					remoteProber,
					remoteProber,
					mcpapprovalcatalog.New(logger, db, mcpRegistryClient),
				),
				func(ctx context.Context, run mcpapproval.ResearchRun) error {
					_, err := background.ExecuteMcpResearchWorkflow(ctx, temporalEnv, activities.McpResearchInput{
						ReportID:  run.ReportID,
						RequestID: run.RequestID,
						ProjectID: run.ProjectID,
						OrgID:     run.OrgID,
					})
					if err != nil {
						return fmt.Errorf("execute research workflow: %w", err)
					}
					return nil
				})
			mcpapproval.Attach(mux, mcpApprovalService)
			instances.Attach(mux, instances.NewService(logger, tracerProvider, meterProvider, db, sessionManager, chatSessionsManager, env, encryptionClient, cache.NewRedisCacheAdapter(redisClient), guardianPolicy, functionsOrchestrator, platformSvc, billingTracker, telemLogger, productFeatures, serverURL, authzEngine))
			mcpmetadata.Attach(mux, mcpMetadataService)
			mcpCatalog := externalmcp.NewCatalogService(db, mcpRegistryClient, nil)
			externalmcp.Attach(mux, externalmcp.NewService(logger, tracerProvider, db, sessionManager, mcpRegistryClient, mcpCatalog, authzEngine, serverURL))
			collections.Attach(mux, collections.NewService(logger, tracerProvider, db, sessionManager, authzEngine, auditLogger, serverURL))
			platformMCPAssistant, err := configurePlatformMCP(ctx, platformMCPConfig{
				Logger:                 logger,
				MeterProvider:          meterProvider,
				TracerProvider:         tracerProvider,
				Mux:                    mux,
				DB:                     db,
				Redis:                  redisClient,
				ServerURL:              serverURL,
				DashboardURL:           siteURL,
				Environment:            c.String("environment"),
				JWTSigningKey:          c.String(usersessions.JWTSigningKeyFlag),
				ProductFeatures:        productFeatures,
				FeatureFlags:           featureFlags,
				Authz:                  authzEngine,
				Encryption:             encryptionClient,
				Identity:               identityResolver,
				Sessions:               sessionManager,
				Registry:               mcpRegistryClient,
				Catalog:                mcpCatalog,
				GuardianPolicy:         guardianPolicy,
				RemoteChallengeManager: remoteChallengeManager,
				AuditLogger:            auditLogger,
				PluginPublisher:        pluginPublisher,
				TemporalEnv:            temporalEnv,
				Skills:                 skillsService,
				Telemetry:              telemetryrepo.New(chDB),
				TelemetryDrilldown:     telemetryrepo.New(chDB),
				SessionCapture:         platformmcp.FeatureChecker(sessionCaptureEnabled),
				LocalFixture:           platformFixture,
			})
			if err != nil {
				return err
			}
			mcp.Attach(mux, mcpService, mcpMetadataService)
			chat.Attach(mux, chatService)
			variations.Attach(mux, variations.NewService(logger, tracerProvider, db, sessionManager, authzEngine, auditLogger))
			customdomains.Attach(mux, customdomains.NewService(logger, tracerProvider, db, sessionManager, &background.CustomDomainRegistrationClient{TemporalEnv: temporalEnv}, authzEngine, auditLogger, c.String("custom-domain-cname"), customDomainARecords))
			usage.Attach(mux, usage.NewService(logger, tracerProvider, db, sessionManager, billingRepo, serverURL, siteURL, posthogClient, openRouter, openRouterKeyRefresher, stripeClient, authzEngine, telemetryrepo.New(chDB), auditLogger, featureFlags, productFeatures, trialEmailNotifier))
			tm.Attach(mux, telemSvc)
			functions.Attach(mux, functions.NewService(logger, tracerProvider, db, encryptionClient, tigrisStore))
			otelsvc.Attach(mux, otelsvc.NewService(logger, tracerProvider, db, chDB, sessionManager, authzEngine, otelsvc.FeatureChecker(logsEnabled), publishers.OTELSpans, publishers.OTELLogs))

			riskSignaler := background.NewThrottledSignaler(
				&background.TemporalRiskAnalysisSignaler{TemporalEnv: temporalEnv, Logger: logger},
				30*time.Second,
				logger,
			)
			// riskSignaler.Shutdown is intentionally NOT registered as a shutdownFunc.
			// runShutdown runs every func concurrently, which races temporalClient.Close()
			// against the signaler's trailing-edge flush over the same gRPC connection
			// ("grpc: the client connection is closing"). Instead it is flushed
			// synchronously in the drain goroutine below, while Temporal is still open.
			riskReconciler := &background.TemporalRiskExclusionReconciler{TemporalEnv: temporalEnv, Logger: logger}
			riskResultsCleaner := &background.TemporalRiskPolicyResultsCleaner{TemporalEnv: temporalEnv, Logger: logger}
			shadowMCPInventoryRepo := telemetryrepo.New(chDB)
			riskService := risk.NewService(
				logger,
				tracerProvider,
				db,
				sessionManager,
				authzEngine,
				riskSignaler,
				riskReconciler,
				riskResultsCleaner,
				completionsClient,
				shadowMCPClient,
				auditLogger,
				cache.NewRedisCacheAdapter(redisClient),
				c.String(usersessions.JWTSigningKeyFlag),
				mcpApprovalService,
				hookPIIScanner,
				hookPIScanner,
				featureFlags,
				celEngine,
				builtinPresets,
				hookPromptJudge,
				policybypass.ReconcilePolicyURLs,
				func(ctx context.Context, projectID uuid.UUID, canonicalURLs []string) ([]string, error) {
					urls, err := shadowMCPInventoryRepo.ListExistingShadowMCPInventoryURLs(ctx, telemetryrepo.ListExistingShadowMCPInventoryURLsParams{
						GramProjectID:       projectID.String(),
						CanonicalServerURLs: canonicalURLs,
					})
					if err != nil {
						return nil, fmt.Errorf("list existing shadow mcp inventory urls: %w", err)
					}
					return urls, nil
				},
				riskchrepo.New(chDB),
				publishers.RiskFindings,
				assetStorage,
			)
			chatWriter.AddObserver(riskService)
			risk.Attach(mux, riskService)

			spendEvaluator := &background.TemporalSpendRuleEvaluator{TemporalEnv: temporalEnv}
			spendrules.Attach(mux, spendrules.NewService(
				logger,
				tracerProvider,
				db,
				chDB,
				sessionManager,
				authzEngine,
				auditLogger,
				spendCelEngine,
				cache.NewRedisCacheAdapter(redisClient),
				featureFlags,
				spendEvaluator,
			))

			// Fresh spend-relevant telemetry (Claude Code OTEL logs,
			// Codex/Cursor usage rows) triggers a throttled per-org
			// evaluation so breached budgets block within seconds instead of
			// waiting for the scheduled sweep. Flushed in the drain goroutine
			// below, not via shutdownFuncs, for the same gRPC-close race
			// reason as riskSignaler.
			spendUsageTrigger := spendrules.NewUsageTrigger(
				logger,
				cache.NewRedisCacheAdapter(redisClient),
				spendEvaluator,
				spendrules.UsageSignalCooldown,
			)
			telemLogger.AddObserver(spendUsageTrigger)

			managedInsightsTools = append(managedInsightsTools, platformtoolsruntime.ManagedAssistantChatsTools(chatService)...)
			managedInsightsTools = append(managedInsightsTools, platformtoolsruntime.ManagedAssistantUsersTools(organizationsService)...)
			managedInsightsTools = append(managedInsightsTools, platformtoolsruntime.ManagedAssistantRiskTools(riskService)...)
			managedInsightsTools = append(managedInsightsTools, platformtoolsruntime.ManagedAssistantDeploymentsTools(deploymentsService)...)
			managedInsightsTools = append(managedInsightsTools, platformtoolsruntime.ManagedAssistantSkillsTools(skillsService, telemetryrepo.New(chDB))...)
			managedInsightsTools = append(managedInsightsTools, platformtoolsruntime.ManagedAssistantPluginsTools(pluginsSvc)...)
			// One-off fetches on a cold cache; a pooled client would only hold
			// idle connections to the marketing site. Bound the whole request
			// so a stalled marketing-site response can't hang the fetch; the
			// client is dedicated to these speakeasy.com tools, which share the
			// same bound, so a per-client timeout is safe.
			marketingSiteClient := guardianPolicy.Client()
			// Take the larger bound so the shared client stays correct if either
			// tool's timeout is tuned independently later.
			marketingSiteClient.Timeout = max(platformchangelog.FetchTimeout, platformdocs.FetchTimeout)
			managedInsightsTools = append(managedInsightsTools, platformtoolsruntime.ManagedAssistantChangelogTools(marketingSiteClient)...)
			managedInsightsTools = append(managedInsightsTools, platformtoolsruntime.ManagedAssistantDocsTools(marketingSiteClient)...)
			maps.Copy(platformToolsets, platformtools.BuildToolsets(platformtools.ToolsetDependencies{
				AssistantMemoryTools:          memoryTools,
				AssistantSkillTools:           skillTools,
				AssistantTriggerTools:         triggerTools,
				ManagedAssistantInsightsTools: managedInsightsTools,
				PlatformMCPReadTools:          assistant_platform_mcp_adapter.ExternalTools(platformMCPAssistant.Tools, platformMCPAssistant.Authorizer),
			}))

			srv := &http.Server{
				Addr:              c.String("address"),
				Handler:           mux,
				ReadHeaderTimeout: 10 * time.Second,
				// IdleTimeout must exceed the fronting GCLB's backend keepalive
				// timeout so the backend retires an idle connection AFTER the LB
				// would, never before. If the backend closes first the LB can
				// still have an outstanding request on that connection and the
				// client sees a TCP RST - the transient reset this change set is
				// hardening against. GCLB's backend keepalive is a fixed 600s and
				// not configurable, and Google explicitly requires the backend's
				// value to be > 600s, so 620s. No WriteTimeout: it is an absolute
				// deadline on the whole response and would sever the long-lived
				// SSE/MCP streams this mux also serves.
				IdleTimeout: 620 * time.Second,
				BaseContext: func(net.Listener) context.Context {
					return ctx
				},
			}

			sigctx, sigcancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
			defer sigcancel()

			group := pool.New()

			if c.Bool("dev-single-process") {
				workerInterruptCh := make(chan any)
				group.Go(func() {
					<-sigctx.Done()
					close(workerInterruptCh)
				})
				group.Go(func() {
					var piiScanner risk_analysis.PIIScanner = &risk_analysis.StubPIIScanner{}
					if presidioURL := c.String("presidio-analyzer-url"); presidioURL != "" {
						piiScanner = risk_analysis.NewPresidioClient(presidioURL, tracerProvider, meterProvider, logger)
					}

					piScanner := promptinjection.NewScanner(logger, piopenrouter.New(logger, tracerProvider, meterProvider, completionsClient, openrouter.NewJudgeRateLimiter(ratelimit.NewRedisStore(redisClient))).Classify)

					temporalWorker := background.NewTemporalWorker(temporalEnv, logger, tracerProvider, meterProvider, &background.WorkerOptions{
						GuardianPolicy:            guardianPolicy,
						DB:                        db,
						EncryptionClient:          encryptionClient,
						FeatureProvider:           featureFlags,
						AssetStorage:              assetStorage,
						SlackClient:               slackClient,
						ChatMessageWriter:         chatWriter,
						ChatClient:                chatClient,
						OpenRouter:                openRouter,
						OpenRouterSpend:           openRouter,
						K8sClient:                 k8sClient,
						ExpectedTargetCNAME:       c.String("custom-domain-cname"),
						ExpectedARecords:          customDomainARecords,
						GitHubEvidenceToken:       c.String("github-evidence-token"),
						SiteURL:                   siteURL,
						BillingTracker:            billingTracker,
						BillingRepository:         billingRepo,
						StripeClient:              stripeClient,
						RedisClient:               redisClient,
						PosthogClient:             posthogClient,
						FunctionsDeployer:         functionsOrchestrator,
						FunctionsVersion:          runnerVersion,
						RagService:                ragService,
						MCPRegistryClient:         mcpRegistryClient,
						TelemetryLogger:           telemLogger,
						ClickhouseConn:            chDB,
						TelemetryRepo:             telemetryrepo.New(chDB),
						TriggersApp:               triggerApp,
						CacheAdapter:              cache.NewRedisCacheAdapter(redisClient),
						EmailService:              emailService,
						AssistantsCore:            assistantsCore,
						TemporalEnv:               temporalEnv,
						PIIScanner:                piiScanner,
						PIScanner:                 piScanner,
						CustomRuleScanner:         customRulesScanner,
						BuiltinPresets:            builtinPresets,
						ShadowMCPClient:           shadowMCPClient,
						AuditLogger:               auditLogger,
						WorkOSClient:              backgroundWorkOSClient,
						SvixClient:                svixClient,
						ProductFeatures:           productFeatures,
						PluginPublisher:           pluginPublisher,
						Publishers:                publishers,
						TrialEmailsService:        trialEmailsService,
						RiskFingerprinter:         riskFingerprinter,
						DisableRiskRetroReconcile: c.Bool("disable-clickhouse-risk-retro-reconcile"),
					})
					if err := temporalWorker.Run(workerInterruptCh); err != nil {
						logger.ErrorContext(ctx, "temporal worker failed", attr.SlogError(err))
					}
				})
			}

			group.Go(func() {
				<-sigctx.Done()

				logger.InfoContext(ctx, "shutting down server")

				graceCtx, graceCancel := context.WithTimeoutCause(
					context.WithoutCancel(ctx),
					shutdownDrainTimeout,
					errors.New("graceful shutdown timed out"),
				)
				defer graceCancel()

				if err := srv.Shutdown(graceCtx); err != nil {
					if gerr := context.Cause(graceCtx); gerr != nil {
						err = errors.Join(err, gerr)
					}
					logger.ErrorContext(ctx, "failed to shutdown server", attr.SlogError(err))
				}

				// The HTTP server is now fully drained, so no new risk signals are
				// produced. Flush the throttle's queued trailing signals here while the
				// Temporal client is still open - runShutdown closes it concurrently.
				if err := riskSignaler.Shutdown(graceCtx); err != nil {
					logger.ErrorContext(ctx, "flush pending risk signals", attr.SlogError(err))
				}
				if err := efficacySignaler.Shutdown(graceCtx); err != nil {
					logger.ErrorContext(ctx, "flush pending skill efficacy signals", attr.SlogError(err))
				}
				if err := chatAnalysisSignaler.Shutdown(graceCtx); err != nil {
					logger.ErrorContext(ctx, "flush pending chat analysis signals", attr.SlogError(err))
				}
				if err := spendUsageTrigger.Shutdown(graceCtx); err != nil {
					logger.ErrorContext(ctx, "flush pending spend rule usage signals", attr.SlogError(err))
				}
				if err := identityMapRefreshSignaler.Shutdown(graceCtx); err != nil {
					logger.ErrorContext(ctx, "flush pending identity map refresh triggers", attr.SlogError(err))
				}
			})

			tlsEnabled := c.String("ssl-key-file") != "" && c.String("ssl-cert-file") != ""

			{
				controlServer := control.Server{
					Address:          c.String("control-address"),
					Logger:           logger.With(attr.SlogComponent("control")),
					DisableProfiling: false,
				}

				temporals := []*o11y.NamedResource[client.Client]{
					{Name: "default", Resource: temporalEnv.Client()},
				}

				listenAddr := srv.Addr
				if listenAddr == "" {
					listenAddr = ":8080"
				}
				host, port, _ := net.SplitHostPort(listenAddr)
				if host == "" {
					host = "localhost"
				}
				healthzEndpoint := &o11y.HTTPEndpoint{
					URL: &url.URL{
						Scheme: conv.Ternary(tlsEnabled, "https", "http"),
						Host:   net.JoinHostPort(host, port),
						Path:   "/healthz",
					},
					TLSCertificate: nil,
				}
				if tlsEnabled {
					cert, err := os.ReadFile(c.String("ssl-cert-file"))
					if err != nil {
						return fmt.Errorf("failed to read TLS certificate for health check: %w", err)
					}
					healthzEndpoint.TLSCertificate = cert
				}
				shutdown, err := controlServer.Start(c.Context, o11y.NewHealthCheckHandler(
					[]*o11y.NamedResource[*o11y.HTTPEndpoint]{{Name: "api", Resource: healthzEndpoint}},
					[]*o11y.NamedResource[*pgxpool.Pool]{{Name: "default", Resource: db}},
					[]*o11y.NamedResource[*redis.Client]{{Name: "default", Resource: redisClient}},
					temporals,
				))
				if err != nil {
					return fmt.Errorf("failed to start control server: %w", err)
				}

				shutdownFuncs = append(shutdownFuncs, shutdown)
			}

			if tlsEnabled {
				logger.InfoContext(ctx, "server started with tls", attr.SlogServerAddress(c.String("address")))
				if err := srv.ListenAndServeTLS(c.String("ssl-cert-file"), c.String("ssl-key-file")); err != nil && !errors.Is(err, http.ErrServerClosed) {
					logger.ErrorContext(ctx, "server error", attr.SlogError(err))
				}
			} else {
				logger.InfoContext(ctx, "server started", attr.SlogServerAddress(c.String("address")))
				if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
					logger.ErrorContext(ctx, "server error", attr.SlogError(err))
				}
			}

			// ListenAndServe returns ErrServerClosed the instant srv.Shutdown is
			// called, not when the drain finishes. Wait for the drain goroutine to
			// fully complete before cancelling ctx: ctx is the server's BaseContext,
			// so cancelling it here would cancel every in-flight request mid-drain
			// and they would abort with context.Canceled instead of completing.
			group.Wait()
			cancel()

			return nil
		},
		Before: func(ctx *cli.Context) error {
			return loadConfigFromFile(ctx, flags)
		},
		After: func(c *cli.Context) error {
			ctx := context.WithoutCancel(c.Context)
			defer dbClose()
			defer o11y.LogDefer(ctx, PullLogger(c.Context), func() error { return clickhouseShutdown(ctx) })
			return runShutdown(PullLogger(c.Context), c.Context, shutdownFuncs)
		},
	}
}
