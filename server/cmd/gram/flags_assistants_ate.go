package gram

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/urfave/cli/v2"
	goahttp "goa.design/goa/v3/http"
	"google.golang.org/api/idtoken"

	"github.com/speakeasy-api/gram/server/internal/assistants"
	"github.com/speakeasy-api/gram/server/internal/assistants/ateclient"
	"github.com/speakeasy-api/gram/server/internal/atecred"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/inv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/orghost"
)

func assistantATERuntimeFlags() []cli.Flag {
	return []cli.Flag{
		&cli.BoolFlag{Name: "assistant-ate-enabled", EnvVars: []string{"GRAM_ASSISTANT_ATE_ENABLED"}, Usage: "Enable the ATE Kit assistant runtime backend; never enabled by default."},
		&cli.StringFlag{Name: "assistant-ate-image", EnvVars: []string{"GRAM_ASSISTANT_ATE_IMAGE"}, Usage: "Digest-pinned OCI image for ATE Kit actors."},
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
		&cli.StringFlag{Name: "assistant-ate-cred-provider-service-account", EnvVars: []string{"GRAM_ASSISTANT_ATE_CRED_PROVIDER_SERVICE_ACCOUNT"}, Usage: "Expected GCP service account email on Google-signed ID tokens presented by the ATE credential provider."},
		&cli.StringFlag{Name: "assistant-ate-cred-provider-local-token", EnvVars: []string{"GRAM_ASSISTANT_ATE_CRED_PROVIDER_LOCAL_TOKEN"}, Usage: "Static bearer token for the ATE credential provider mint endpoint; allowed only in local environment."},
	}
}

func installKitATERuntimeFromCLI(c *cli.Context, core *assistants.ServiceCore, redisClient *redis.Client, serverURL *url.URL, orgHosts *orghost.Resolver) error {
	if !c.Bool("assistant-ate-enabled") {
		return nil
	}

	workerSelector, selectorErr := assistants.ParseKitATEWorkerSelector(c.String("assistant-ate-worker-selector"))

	inv.Require("assistant ATE runtime configuration",
		"deployment server URL is configured", serverURL != nil,
		"assistant-ate-image is set", c.String("assistant-ate-image") != "",
		"assistant-ate-worker-selector is valid", selectorErr == nil && len(workerSelector) > 0,
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
			Environment:            c.String("environment"),
			Image:                  c.String("assistant-ate-image"),
			Origin:                 nil,
			TurnStream:             chat.NewTurnStream(redisClient),
			WorkloadIssuerURL:      "",
			WorkloadJWKSURL:        "",
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

func mountATECredentialMintEndpointFromCLI(ctx context.Context, logger *slog.Logger, mux goahttp.Muxer, c *cli.Context, core *assistants.ServiceCore, serverURL *url.URL) error {
	sa := strings.TrimSpace(c.String("assistant-ate-cred-provider-service-account"))
	localToken := strings.TrimSpace(c.String("assistant-ate-cred-provider-local-token"))
	atespace := strings.TrimSpace(c.String("assistant-ate-atespace"))
	if !c.Bool("assistant-ate-enabled") && ((sa == "" && localToken == "") || atespace == "") {
		return nil
	}
	base, err := assistantRuntimeConfigFromCLI(c, serverURL)
	if err != nil {
		return err
	}
	if base.Provider == assistants.RuntimeProviderDisabled {
		logger.WarnContext(ctx, "ignoring ATE credential provider mint endpoint settings: the assistant runtime provider is disabled")
		return nil
	}

	var (
		mintURL string
		urlErr  error
	)
	if serverURL != nil {
		mintURL, urlErr = atecred.MintEndpointURL(serverURL)
	}
	authErr := atecred.ValidateServerAuthConfig(c.String("environment"), sa, localToken)

	inv.Require("assistant ATE credential provider mint endpoint configuration",
		"deployment server URL is configured", serverURL != nil && urlErr == nil,
		"assistant-ate-atespace is set", atespace != "",
		"ATE credential provider auth configuration is valid", authErr == nil,
	)

	var validator atecred.IDTokenValidator
	if sa != "" {
		var err error
		validator, err = idtoken.NewValidator(ctx)
		if err != nil {
			return fmt.Errorf("create Google ID token validator for ATE credential provider: %w", err)
		}
	}

	core.SetATEAtespace(atespace)
	auth := atecred.NewAuthenticator(atecred.AuthenticatorConfig{
		Environment:      c.String("environment"),
		ExpectedAudience: mintURL,
		ServiceAccount:   sa,
		LocalToken:       localToken,
		Validator:        validator,
	})
	handler := atecred.NewMintHandler(logger, auth, core)
	o11y.AttachHandler(mux, http.MethodPost, atecred.MintEndpointPath, handler.ServeHTTP)
	return nil
}
