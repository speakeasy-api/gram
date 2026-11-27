package gram

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/urfave/cli/v2"

	"github.com/speakeasy-api/gram/server/internal/assistants"
	"github.com/speakeasy-api/gram/server/internal/assistants/ateclient"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/inv"
	"github.com/speakeasy-api/gram/server/internal/orghost"
)

func assistantATERuntimeFlags() []cli.Flag {
	return []cli.Flag{
		&cli.BoolFlag{Name: "assistant-ate-enabled", EnvVars: []string{"GRAM_ASSISTANT_ATE_ENABLED"}, Usage: "Enable the ATE Kit assistant runtime backend; never enabled by default."},
		&cli.StringFlag{Name: "assistant-ate-image", EnvVars: []string{"GRAM_ASSISTANT_ATE_IMAGE"}, Usage: "Digest-pinned OCI image for ATE Kit actors (separate from the Kit GKE image)."},
		&cli.StringFlag{Name: "assistant-ate-worker-selector", EnvVars: []string{"GRAM_ASSISTANT_ATE_WORKER_SELECTOR"}, Usage: "Comma-separated key=value worker selector labels for ATE ActorTemplates (e.g. workload-type=ate-assistants)."},
		&cli.StringFlag{Name: "assistant-ate-issuer-url", EnvVars: []string{"GRAM_ASSISTANT_ATE_ISSUER_URL"}, Usage: "Public HTTPS ATE actor JWT issuer URL (matches ate-api-server ACTOR_JWT_ISSUER)."},
		&cli.StringFlag{Name: "assistant-ate-jwks-url", EnvVars: []string{"GRAM_ASSISTANT_ATE_JWKS_URL"}, Usage: "Optional explicit HTTPS JWKS URL for ATE actor JWTs; defaults to <assistant-ate-issuer-url>/openid/v1/jwks."},
		&cli.StringFlag{Name: "assistant-ate-api-address", EnvVars: []string{"GRAM_ASSISTANT_ATE_API_ADDRESS"}, Usage: "ATE control API host:port, authenticated with TLS and an audience-bound JWT."},
		&cli.StringFlag{Name: "assistant-ate-router-address", EnvVars: []string{"GRAM_ASSISTANT_ATE_ROUTER_ADDRESS"}, Usage: "ATE authenticated actor router host:port."},
		&cli.StringFlag{Name: "assistant-ate-api-server-name", EnvVars: []string{"GRAM_ASSISTANT_ATE_API_SERVER_NAME"}, Usage: "Optional TLS server name for the control API."},
		&cli.StringFlag{Name: "assistant-ate-router-server-name", EnvVars: []string{"GRAM_ASSISTANT_ATE_ROUTER_SERVER_NAME"}, Usage: "Optional TLS server name for the actor router."},
		&cli.StringFlag{Name: "assistant-ate-service-ca-file", EnvVars: []string{"GRAM_ASSISTANT_ATE_SERVICE_CA_FILE"}, Usage: "Trusted ATE service CA PEM file."},
		&cli.StringFlag{Name: "assistant-ate-client-cert-file", EnvVars: []string{"GRAM_ASSISTANT_ATE_CLIENT_CERT_FILE"}, Usage: "Rotating router mTLS client certificate PEM file."},
		&cli.StringFlag{Name: "assistant-ate-client-key-file", EnvVars: []string{"GRAM_ASSISTANT_ATE_CLIENT_KEY_FILE"}, Usage: "Rotating router mTLS client private-key PEM file."},
		&cli.StringFlag{Name: "assistant-ate-audience-jwt-file", EnvVars: []string{"GRAM_ASSISTANT_ATE_AUDIENCE_JWT_FILE"}, Usage: "Rotating audience-bound ATE control-plane JWT file; never copied into actors."},
		&cli.StringFlag{Name: "assistant-ate-atespace", EnvVars: []string{"GRAM_ASSISTANT_ATE_ATESPACE"}, Usage: "Explicit pre-created ATE atespace for Kit thread actors."},
		&cli.StringFlag{Name: "assistant-ate-template", EnvVars: []string{"GRAM_ASSISTANT_ATE_TEMPLATE"}, Usage: "Explicit secret-free ActorTemplate base name (digest-versioned per image digest)."},
	}
}

func installKitATERuntimeFromCLI(c *cli.Context, core *assistants.ServiceCore, redisClient *redis.Client, serverURL *url.URL, orgHosts *orghost.Resolver) error {
	if !c.Bool("assistant-ate-enabled") {
		return nil
	}

	workerSelector, selectorErr := assistants.ParseKitATEWorkerSelector(c.String("assistant-ate-worker-selector"))
	issuerURL := strings.TrimSpace(c.String("assistant-ate-issuer-url"))
	jwksURL := strings.TrimSpace(c.String("assistant-ate-jwks-url"))
	if jwksURL == "" && issuerURL != "" {
		jwksURL = strings.TrimRight(issuerURL, "/") + "/openid/v1/jwks"
	}

	inv.Require("assistant ATE runtime configuration",
		"deployment server URL is configured", serverURL != nil,
		"assistant-ate-image is set", c.String("assistant-ate-image") != "",
		"assistant-ate-worker-selector is valid", selectorErr == nil && len(workerSelector) > 0,
		"assistant-ate-issuer-url is set", issuerURL != "",
		"assistant-ate-api-address is set", c.String("assistant-ate-api-address") != "",
		"assistant-ate-router-address is set", c.String("assistant-ate-router-address") != "",
		"assistant-ate-service-ca-file is set", c.String("assistant-ate-service-ca-file") != "",
		"assistant-ate-client-cert-file is set", c.String("assistant-ate-client-cert-file") != "",
		"assistant-ate-client-key-file is set", c.String("assistant-ate-client-key-file") != "",
		"assistant-ate-audience-jwt-file is set", c.String("assistant-ate-audience-jwt-file") != "",
		"assistant-ate-atespace is set", c.String("assistant-ate-atespace") != "",
		"assistant-ate-template is set", c.String("assistant-ate-template") != "",
	)

	err := core.InstallKitATERuntime(c.Context, assistants.KitATECompositionConfig{
		Enabled:        true,
		Atespace:       c.String("assistant-ate-atespace"),
		Template:       c.String("assistant-ate-template"),
		WorkerSelector: workerSelector,
		KitLocalCompositionConfig: assistants.KitLocalCompositionConfig{
			OrgHosts:               orgHosts,
			AllowInsecureInference: false,
			MCPUserSessionIssuerID: uuid.Nil,
			WorkloadNamespace:      "",
			Environment:            c.String("environment"),
			Image:                  c.String("assistant-ate-image"),
			Origin:                 nil,
			TurnStream:             chat.NewTurnStream(redisClient),
			WorkloadIssuerURL:      issuerURL,
			WorkloadJWKSURL:        jwksURL,
			WorkloadIssuerBaseURL:  "",
			WorkloadSubject:        c.String("assistant-kit-workload-subject"),
			WorkloadAssertionFile:  c.String("assistant-kit-workload-assertion-file"),
			Network:                c.String("assistant-kit-network"),
			ExtraCACertFile:        c.String("assistant-runtime-local-ca-file"),
		},
		ClientConfig: ateclient.Config{
			Atespace:             c.String("assistant-ate-atespace"),
			ConnectTimeout:       0,
			ConnectRetryInterval: 0,
			ConnectRetryTimeout:  0,
			RPCTimeout:           0,
			APIAddress:           c.String("assistant-ate-api-address"),
			RouterAddress:        c.String("assistant-ate-router-address"),
			APIServerName:        c.String("assistant-ate-api-server-name"),
			RouterServerName:     c.String("assistant-ate-router-server-name"),
			ServiceCAFile:        c.String("assistant-ate-service-ca-file"),
			ClientCertFile:       c.String("assistant-ate-client-cert-file"),
			ClientKeyFile:        c.String("assistant-ate-client-key-file"),
			AudienceJWTFile:      c.String("assistant-ate-audience-jwt-file"),
		},
	})
	if err != nil {
		return fmt.Errorf("install assistant ATE runtime: %w", err)
	}
	return nil
}
