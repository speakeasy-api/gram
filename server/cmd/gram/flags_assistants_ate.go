package gram

import (
	"fmt"
	"net/url"

	"github.com/redis/go-redis/v9"
	"github.com/speakeasy-api/gram/server/internal/assistants"
	"github.com/speakeasy-api/gram/server/internal/assistants/ateclient"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/urfave/cli/v2"
)

func assistantATERuntimeFlags() []cli.Flag {
	return []cli.Flag{
		&cli.BoolFlag{Name: "assistant-ate-enabled", EnvVars: []string{"GRAM_ASSISTANT_ATE_ENABLED"}, Usage: "Explicitly enable ATE only in a named dev preview; never enabled by default."},
		&cli.StringFlag{Name: "assistant-ate-preview-id", EnvVars: []string{"GRAM_ASSISTANT_ATE_PREVIEW_ID"}, Usage: "Required DNS-label identifier of the isolated development preview."},
		&cli.StringFlag{Name: "assistant-ate-api-address", EnvVars: []string{"GRAM_ASSISTANT_ATE_API_ADDRESS"}, Usage: "ATE control API host:port, authenticated with TLS and an audience-bound JWT."},
		&cli.StringFlag{Name: "assistant-ate-router-address", EnvVars: []string{"GRAM_ASSISTANT_ATE_ROUTER_ADDRESS"}, Usage: "ATE authenticated actor router host:port."},
		&cli.StringFlag{Name: "assistant-ate-api-server-name", EnvVars: []string{"GRAM_ASSISTANT_ATE_API_SERVER_NAME"}, Usage: "Optional TLS server name for the control API."},
		&cli.StringFlag{Name: "assistant-ate-router-server-name", EnvVars: []string{"GRAM_ASSISTANT_ATE_ROUTER_SERVER_NAME"}, Usage: "Optional TLS server name for the actor router."},
		&cli.StringFlag{Name: "assistant-ate-service-ca-file", EnvVars: []string{"GRAM_ASSISTANT_ATE_SERVICE_CA_FILE"}, Usage: "Trusted ATE service CA PEM file."},
		&cli.StringFlag{Name: "assistant-ate-client-cert-file", EnvVars: []string{"GRAM_ASSISTANT_ATE_CLIENT_CERT_FILE"}, Usage: "Rotating control-plane client certificate PEM file."},
		&cli.StringFlag{Name: "assistant-ate-client-key-file", EnvVars: []string{"GRAM_ASSISTANT_ATE_CLIENT_KEY_FILE"}, Usage: "Rotating control-plane client private-key PEM file."},
		&cli.StringFlag{Name: "assistant-ate-audience-jwt-file", EnvVars: []string{"GRAM_ASSISTANT_ATE_AUDIENCE_JWT_FILE"}, Usage: "Rotating audience-bound ATE control-plane JWT file; never copied into actors."},
		&cli.StringFlag{Name: "assistant-ate-atespace", EnvVars: []string{"GRAM_ASSISTANT_ATE_ATESPACE"}, Usage: "Explicit preview ATE atespace."},
		&cli.StringFlag{Name: "assistant-ate-template", EnvVars: []string{"GRAM_ASSISTANT_ATE_TEMPLATE"}, Usage: "Explicit secret-free template running the pinned native Kit bootstrap image."},
	}
}

func installKitATERuntimeFromCLI(c *cli.Context, core *assistants.ServiceCore, redisClient *redis.Client, serverURL *url.URL) error {
	if err := assistants.ValidateKitATEPreview(c.String("environment"), c.Bool("assistant-ate-enabled"), c.String("assistant-ate-preview-id")); err != nil {
		return err
	}
	if !c.Bool("assistant-ate-enabled") {
		return nil
	}
	if serverURL == nil {
		return fmt.Errorf("ATE requires the deployment server URL")
	}
	authenticationHost, err := mcp.NewAuthenticationHost(c.String("authentication-host-url"), serverURL, c.String("environment"))
	if err != nil {
		return err
	}
	issuerBaseURL := authenticationHost.BaseURL()
	if issuerBaseURL == "" {
		issuerBaseURL = (&url.URL{Scheme: serverURL.Scheme, Host: serverURL.Host}).String()
	}
	return core.InstallKitATERuntime(c.Context, assistants.KitATECompositionConfig{
		Enabled: true, PreviewID: c.String("assistant-ate-preview-id"), Atespace: c.String("assistant-ate-atespace"), Template: c.String("assistant-ate-template"),
		KitLocalCompositionConfig: assistants.KitLocalCompositionConfig{
			PersistedMCP: true, Environment: c.String("environment"), Image: c.String("assistant-kit-image"),
			Origin: &url.URL{Scheme: "https", Host: "ai.speakeasy.com"}, TurnStream: chat.NewTurnStream(redisClient),
			WorkloadIssuerURL: c.String("assistant-kit-workload-issuer"), WorkloadJWKSURL: c.String("assistant-kit-workload-jwks-url"), WorkloadIssuerBaseURL: issuerBaseURL,
			WorkloadSubject: c.String("assistant-kit-workload-subject"), WorkloadAssertionFile: c.String("assistant-kit-workload-assertion-file"),
			Network: c.String("assistant-kit-network"), ExtraCACertFile: c.String("assistant-runtime-local-ca-file"),
		},
		ClientConfig: ateclient.Config{
			APIAddress: c.String("assistant-ate-api-address"), RouterAddress: c.String("assistant-ate-router-address"),
			APIServerName: c.String("assistant-ate-api-server-name"), RouterServerName: c.String("assistant-ate-router-server-name"),
			ServiceCAFile: c.String("assistant-ate-service-ca-file"), ClientCertFile: c.String("assistant-ate-client-cert-file"), ClientKeyFile: c.String("assistant-ate-client-key-file"), AudienceJWTFile: c.String("assistant-ate-audience-jwt-file"),
		},
	})
}
