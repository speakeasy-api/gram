package gram

import (
	"fmt"
	"net/url"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/urfave/cli/v2"

	"github.com/speakeasy-api/gram/server/internal/assistants"
	"github.com/speakeasy-api/gram/server/internal/assistants/ateclient"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/inv"
	"github.com/speakeasy-api/gram/server/internal/mcp"
)

func assistantATERuntimeFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringSliceFlag{Name: "assistant-ate-egress-host", EnvVars: []string{"GRAM_ASSISTANT_ATE_EGRESS_HOSTS"}, Usage: "Operator-approved extra DNS origins for ATE actor egress; no wildcards or IP addresses. Empty permits only the configured inference, server, and workload broker origins."},
		&cli.BoolFlag{Name: "assistant-ate-enabled", EnvVars: []string{"GRAM_ASSISTANT_ATE_ENABLED"}, Usage: "Enable the ATE Kit assistant runtime backend; never enabled by default."},
		&cli.StringFlag{Name: "assistant-ate-image", EnvVars: []string{"GRAM_ASSISTANT_ATE_IMAGE"}, Usage: "Digest-pinned OCI image for ATE Kit actors (separate from the Kit GKE image)."},
		&cli.StringFlag{Name: "assistant-ate-worker-selector", EnvVars: []string{"GRAM_ASSISTANT_ATE_WORKER_SELECTOR"}, Usage: "Comma-separated key=value worker selector labels for ATE ActorTemplates (e.g. workload-type=ate-assistants)."},
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

func installKitATERuntimeFromCLI(c *cli.Context, core *assistants.ServiceCore, redisClient *redis.Client, serverURL *url.URL) error {
	if !c.Bool("assistant-ate-enabled") {
		return nil
	}

	workerSelector, selectorErr := assistants.ParseKitATEWorkerSelector(c.String("assistant-ate-worker-selector"))

	inv.Require("assistant ATE runtime configuration",
		"deployment server URL is configured", serverURL != nil,
		"assistant-ate-image is set", c.String("assistant-ate-image") != "",
		"assistant-ate-worker-selector is valid", selectorErr == nil && len(workerSelector) > 0,
		"assistant-kit-workload-issuer is set", c.String("assistant-kit-workload-issuer") != "",
		"assistant-kit-workload-jwks-url is set", c.String("assistant-kit-workload-jwks-url") != "",
		"assistant-ate-api-address is set", c.String("assistant-ate-api-address") != "",
		"assistant-ate-router-address is set", c.String("assistant-ate-router-address") != "",
		"assistant-ate-service-ca-file is set", c.String("assistant-ate-service-ca-file") != "",
		"assistant-ate-client-cert-file is set", c.String("assistant-ate-client-cert-file") != "",
		"assistant-ate-client-key-file is set", c.String("assistant-ate-client-key-file") != "",
		"assistant-ate-audience-jwt-file is set", c.String("assistant-ate-audience-jwt-file") != "",
		"assistant-ate-atespace is set", c.String("assistant-ate-atespace") != "",
		"assistant-ate-template is set", c.String("assistant-ate-template") != "",
	)

	authenticationHost, err := mcp.NewAuthenticationHost(c.String("authentication-host-url"), serverURL, c.String("environment"))
	if err != nil {
		return fmt.Errorf("configure ATE authentication host: %w", err)
	}
	issuerBaseURL := authenticationHost.BaseURL()
	if issuerBaseURL == "" {
		issuerBaseURL = (&url.URL{Scheme: serverURL.Scheme, Host: serverURL.Host}).String()
	}

	err = core.InstallKitATERuntime(c.Context, assistants.KitATECompositionConfig{
		Enabled:        true,
		EgressHosts:    c.StringSlice("assistant-ate-egress-host"),
		Atespace:       c.String("assistant-ate-atespace"),
		Template:       c.String("assistant-ate-template"),
		WorkerSelector: workerSelector,
		KitLocalCompositionConfig: assistants.KitLocalCompositionConfig{
			AllowInsecureInference: false,
			MCPResource:            nil,
			MCPIssuer:              nil,
			MCPUserSessionIssuerID: uuid.Nil,
			WorkloadNamespace:      "",
			PersistedMCP:           true,
			Environment:            c.String("environment"),
			Image:                  c.String("assistant-ate-image"),
			Origin:                 &url.URL{Scheme: "https", Host: "ai.speakeasy.com"},
			TurnStream:             chat.NewTurnStream(redisClient),
			WorkloadIssuerURL:      c.String("assistant-kit-workload-issuer"),
			WorkloadJWKSURL:        c.String("assistant-kit-workload-jwks-url"),
			WorkloadIssuerBaseURL:  issuerBaseURL,
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
