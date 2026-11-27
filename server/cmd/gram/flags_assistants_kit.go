package gram

import (
	"fmt"
	"log/slog"
	"net/url"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/speakeasy-api/gram/server/internal/assistants"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/orghost"
	"github.com/urfave/cli/v2"
)

func assistantKitRuntimeFlags() []cli.Flag {
	return append([]cli.Flag{
		&cli.StringFlag{Name: "assistant-kit-workload-jwks-url", EnvVars: []string{"GRAM_ASSISTANT_KIT_WORKLOAD_JWKS_URL"}, Usage: "External issuer JWKS URL for local Kit workload assertions."},
		&cli.StringFlag{Name: "assistant-kit-workload-issuer", EnvVars: []string{"GRAM_ASSISTANT_KIT_WORKLOAD_ISSUER"}, Usage: "External issuer URL for local Kit workload assertions."},
		&cli.StringFlag{Name: "assistant-kit-workload-subject", EnvVars: []string{"GRAM_ASSISTANT_KIT_WORKLOAD_SUBJECT"}, Usage: "Local operator-projected subject, unique to one runtime generation. Supply a fresh external subject and assertion before retiring and recreating; subjects cannot be reused or shared between assistants."},
		&cli.StringFlag{Name: "assistant-kit-workload-assertion-file", EnvVars: []string{"GRAM_ASSISTANT_KIT_WORKLOAD_ASSERTION_FILE"}, Usage: "Absolute operator-projected file named assertion in a dedicated directory. Its parent is mounted read-only outside persistent HOME to support atomic rotation."},
		&cli.StringFlag{Name: "assistant-kit-network", EnvVars: []string{"GRAM_ASSISTANT_KIT_NETWORK"}, Usage: "Isolated local container network with a trusted TLS relay for ai.speakeasy.com."},
		&cli.StringFlag{Name: "assistant-kit-image", EnvVars: []string{"GRAM_ASSISTANT_KIT_IMAGE"}, Usage: "Opt new local assistant sessions into Kit v3 using an explicit non-latest tag or digest; local development only (deployed Kit assistants use --assistant-ate-enabled)."},
	}, assistantATERuntimeFlags()...)
}

func installKitLocalRuntimeFromCLI(logger *slog.Logger, c *cli.Context, core *assistants.ServiceCore, _ *pgxpool.Pool, _ *audit.Logger, redisClient *redis.Client, serverURL *url.URL, orgHosts *orghost.Resolver) error {
	if c.Bool("assistant-ate-enabled") {
		base, err := assistantRuntimeConfigFromCLI(c, serverURL)
		if err != nil {
			return err
		}
		if base.Provider == assistants.RuntimeProviderDisabled {
			logger.WarnContext(c.Context, "ignoring ATE Kit settings: the assistant runtime provider is disabled")
			return nil
		}
		return installKitATERuntimeFromCLI(c, core, redisClient, serverURL, orgHosts)
	}
	if c.String("assistant-kit-image") == "" {
		return nil
	}
	base, err := assistantRuntimeConfigFromCLI(c, serverURL)
	if err != nil {
		return err
	}
	// Partitions can share Kit settings while one of them runs with
	// assistants off, so the settings are ignored rather than rejected.
	if base.Provider == assistants.RuntimeProviderDisabled {
		logger.WarnContext(c.Context, "ignoring Kit settings: the assistant runtime provider is disabled")
		return nil
	}
	if base.Provider != assistants.RuntimeProviderLocal || c.String("environment") != "local" {
		return fmt.Errorf("local Kit requires --assistant-runtime-provider=local and --environment=local; deployed Kit assistants use ATE (--assistant-ate-enabled)")
	}
	if serverURL == nil {
		return fmt.Errorf("kit requires the deployment server URL")
	}
	authenticationHost, err := mcp.NewAuthenticationHost(c.String("authentication-host-url"), serverURL, c.String("environment"))
	if err != nil {
		return fmt.Errorf("configure Kit authentication host: %w", err)
	}
	// Without an authentication host, shared issuers are served from the server
	// URL, so that origin is the workload issuer and must pass the same checks.
	issuerBaseURL := authenticationHost.BaseURL()
	if issuerBaseURL == "" {
		issuerBaseURL = (&url.URL{Scheme: serverURL.Scheme, Host: serverURL.Host}).String()
	}
	config := assistants.KitLocalCompositionConfig{
		OrgHosts: orgHosts, MCPUserSessionIssuerID: uuid.Nil,
		Environment: c.String("environment"), Image: c.String("assistant-kit-image"), ExtraCACertFile: c.String("assistant-runtime-local-ca-file"),
		Origin: &url.URL{Scheme: "https", Host: "ai.speakeasy.com"}, Network: c.String("assistant-kit-network"), AllowInsecureInference: false,
		TurnStream:            chat.NewTurnStream(redisClient),
		WorkloadJWKSURL:       c.String("assistant-kit-workload-jwks-url"),
		WorkloadIssuerURL:     c.String("assistant-kit-workload-issuer"),
		WorkloadSubject:       c.String("assistant-kit-workload-subject"),
		WorkloadAssertionFile: c.String("assistant-kit-workload-assertion-file"),
		WorkloadIssuerBaseURL: issuerBaseURL,
	}

	if err := core.InstallKitLocalRuntime(config); err != nil {
		return fmt.Errorf("install Kit runtime: %w", err)
	}
	return nil
}
