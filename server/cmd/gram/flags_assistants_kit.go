package gram

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net"
	"net/url"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/speakeasy-api/gram/server/internal/assistants"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/k8s"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/usersessions/authserver"
	"github.com/urfave/cli/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func assistantKitRuntimeFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{Name: "assistant-kit-gke-ingress-cidr", EnvVars: []string{"GRAM_ASSISTANT_KIT_GKE_INGRESS_CIDR"}, Value: "", Usage: "Server source CIDR permitted to connect to Kit gateway pods."},
		&cli.StringFlag{Name: "assistant-kit-gke-cpu-request", EnvVars: []string{"GRAM_ASSISTANT_KIT_GKE_CPU_REQUEST"}, Value: "100m", Usage: "Kit pod CPU request."},
		&cli.StringFlag{Name: "assistant-kit-gke-cpu-limit", EnvVars: []string{"GRAM_ASSISTANT_KIT_GKE_CPU_LIMIT"}, Value: "1", Usage: "Kit pod CPU limit."},
		&cli.StringFlag{Name: "assistant-kit-gke-memory-request", EnvVars: []string{"GRAM_ASSISTANT_KIT_GKE_MEMORY_REQUEST"}, Value: "128Mi", Usage: "Kit pod memory request."},
		&cli.StringFlag{Name: "assistant-kit-gke-memory-limit", EnvVars: []string{"GRAM_ASSISTANT_KIT_GKE_MEMORY_LIMIT"}, Value: "1Gi", Usage: "Kit pod memory limit."},
		&cli.StringFlag{Name: "assistant-kit-gke-work-size-limit", EnvVars: []string{"GRAM_ASSISTANT_KIT_GKE_WORK_SIZE_LIMIT"}, Value: "1Gi", Usage: "Kit workspace volume size limit."},
		&cli.StringFlag{Name: "assistant-kit-gke-tmp-size-limit", EnvVars: []string{"GRAM_ASSISTANT_KIT_GKE_TMP_SIZE_LIMIT"}, Value: "128Mi", Usage: "Kit temporary volume size limit."},
		&cli.BoolFlag{Name: "assistant-kit-gke-snapshots-enabled", EnvVars: []string{"GRAM_ASSISTANT_KIT_GKE_SNAPSHOTS_ENABLED"}, Usage: "Enable Kit GKE snapshot provisioning for new sessions."},
		&cli.StringFlag{Name: "assistant-kit-mcp-auth-method", EnvVars: []string{"GRAM_ASSISTANT_KIT_MCP_AUTH_METHOD"}, Value: "client_secret_basic", Usage: "Kit workload authentication: client_secret_basic or private_key_jwt."},
		&cli.StringFlag{Name: "assistant-kit-network", EnvVars: []string{"GRAM_ASSISTANT_KIT_NETWORK"}, Usage: "Isolated local container network with a trusted TLS relay for ai.speakeasy.com."},
		&cli.StringFlag{Name: "assistant-kit-image", EnvVars: []string{"GRAM_ASSISTANT_KIT_IMAGE"}, Usage: "Opt new assistant sessions into Kit v3 using an explicit non-latest tag or digest; empty preserves legacy composition."},
		&cli.StringFlag{Name: "assistant-kit-mcp-resource", EnvVars: []string{"GRAM_ASSISTANT_KIT_MCP_RESOURCE"}, Usage: "Exact HTTPS URL of the attached issuer-gated MCP resource to provision for Kit."},
		&cli.StringFlag{Name: "assistant-kit-mcp-issuer", EnvVars: []string{"GRAM_ASSISTANT_KIT_MCP_ISSUER"}, Usage: "Expected OAuth issuer advertised by the configured Kit MCP resource."},
		&cli.StringFlag{Name: "assistant-kit-mcp-issuer-id", EnvVars: []string{"GRAM_ASSISTANT_KIT_MCP_ISSUER_ID"}, Usage: "Persisted user-session issuer UUID for the configured Kit resource."},
		&cli.StringFlag{Name: "assistant-kit-root-trigger-id", EnvVars: []string{"GRAM_ASSISTANT_KIT_ROOT_TRIGGER_ID"}, Usage: "Optional existing root trigger UUID; required when the assistant has multiple roots."},
	}
}

func installKitLocalRuntimeFromCLI(c *cli.Context, core *assistants.ServiceCore, db *pgxpool.Pool, auditLogger *audit.Logger, redisClient *redis.Client, serverURL *url.URL) error {
	if c.String("assistant-kit-image") == "" {
		return nil
	}
	authMethod := c.String("assistant-kit-mcp-auth-method")
	if authMethod != "client_secret_basic" && authMethod != "private_key_jwt" {
		return fmt.Errorf("invalid assistant-kit-mcp-auth-method")
	}
	base, err := assistantRuntimeConfigFromCLI(c, serverURL)
	if err != nil {
		return err
	}
	if base.Provider == assistants.RuntimeProviderLocal && c.String("environment") != "local" {
		return fmt.Errorf("local Kit requires the local environment")
	}
	var provision assistants.KitGKEProvisionInput
	if base.Provider == assistants.RuntimeProviderGKE {
		provision, err = kitGKEProvisionFromCLI(c)
		if err != nil {
			return err
		}
	}
	resource, err := url.Parse(c.String("assistant-kit-mcp-resource"))
	if err != nil {
		return fmt.Errorf("parse Kit MCP resource: %w", err)
	}
	issuer, err := url.Parse(c.String("assistant-kit-mcp-issuer"))
	if err != nil {
		return fmt.Errorf("parse Kit MCP issuer: %w", err)
	}
	issuerID, err := uuid.Parse(c.String("assistant-kit-mcp-issuer-id"))
	if err != nil {
		return fmt.Errorf("assistant-kit-mcp-issuer-id must identify the configured resource's issuer")
	}
	var root uuid.UUID
	if raw := c.String("assistant-kit-root-trigger-id"); raw != "" {
		root, err = uuid.Parse(raw)
		if err != nil {
			return fmt.Errorf("invalid assistant-kit-root-trigger-id")
		}
	}
	if serverURL == nil {
		return fmt.Errorf("Kit requires the deployment server URL")
	}
	authenticationHost, err := mcp.NewAuthenticationHost(c.String("authentication-host-url"), serverURL, c.String("environment"))
	if err != nil {
		return fmt.Errorf("configure Kit authentication host: %w", err)
	}
	platformHosts, err := parsePlatformHosts(c, authenticationHost)
	if err != nil {
		return err
	}
	hosts := authserver.Hosts{ServerURL: serverURL.String(), AuthenticationHostBaseURL: authenticationHost.BaseURL(), PlatformHosts: platformHosts}
	var localOrigin *url.URL
	if base.Provider == assistants.RuntimeProviderLocal && c.String("environment") == "local" {
		localOrigin = base.Local.ServerURL
	}
	provisioner := mcp.NewAssistantRuntimeWorkloadProvisioner(db, auditLogger)
	config := assistants.KitLocalCompositionConfig{
		Environment: c.String("environment"), Image: c.String("assistant-kit-image"), ExtraCACertFile: c.String("assistant-runtime-local-ca-file"),
		Origin: &url.URL{Scheme: "https", Host: "ai.speakeasy.com"}, Network: c.String("assistant-kit-network"), AllowInsecureInference: false,
		MCPResource: resource, MCPIssuer: issuer, MCPUserSessionIssuerID: issuerID, RootTriggerID: root, TurnStream: chat.NewTurnStream(redisClient),
		ProvisionMCP: func(ctx context.Context, r assistants.KitMCPProvisionRequest) (assistants.KitMCPProvisionResult, error) {
			var zero assistants.KitMCPProvisionResult
			if err := validateKitMCPBinding(ctx, db, r, resource, issuer, hosts, localOrigin); err != nil {
				return zero, err
			}
			// Lifecycle admission already holds the shared assistant fence. Do not
			// acquire it again on another connection while rotating startup credentials.
			tx, err := db.Begin(ctx)
			if err != nil {
				return zero, fmt.Errorf("configure Kit workload: %w", err)
			}
			defer func() { _ = tx.Rollback(ctx) }()
			if _, err := mcp.RevokeAssistantRuntimeWorkloadClients(ctx, tx, r.OrganizationID, r.ProjectID, r.RuntimeID); err != nil {
				return zero, fmt.Errorf("configure Kit workload: %w", err)
			}
			if err := tx.Commit(ctx); err != nil {
				return zero, fmt.Errorf("configure Kit workload: %w", err)
			}
			workloadConfig, privatePEM, err := kitWorkloadClientCredentials(authMethod)
			if err != nil {
				return zero, err
			}
			workloadConfig.UserSessionIssuerID = r.UserSessionIssuerID
			workloadConfig.TriggerID = r.TriggerID
			client, err := provisioner.Provision(ctx, r.RuntimeID, workloadConfig)
			if err != nil {
				return zero, fmt.Errorf("configure Kit workload: %w", err)
			}
			if client.RuntimeID != r.RuntimeID || client.TriggerID != r.TriggerID {
				return zero, fmt.Errorf("kit workload registration returned a mismatched runtime binding")
			}
			return assistants.KitMCPProvisionResult{ClientID: client.ClientID, Secret: client.Secret, AuthMethod: authMethod, PrivateKeyPEM: privatePEM}, nil
		},
	}
	if base.Provider == assistants.RuntimeProviderLocal {
		err = core.InstallKitLocalRuntime(config)
	} else {
		caCert, decodeErr := base64.StdEncoding.DecodeString(c.String("assistant-runtime-gke-cluster-ca"))
		if decodeErr != nil {
			return fmt.Errorf("decode --assistant-runtime-gke-cluster-ca: %w", decodeErr)
		}
		if len(caCert) == 0 || c.String("assistant-runtime-gke-cluster-endpoint") == "" {
			return fmt.Errorf("Kit GKE requires assistant runtime cluster endpoint and CA")
		}
		base.GKE.Dynamic, err = k8s.NewRemoteDynamicClient(c.Context, c.String("assistant-runtime-gke-cluster-endpoint"), caCert)
		if err != nil {
			return fmt.Errorf("build Kit assistant cluster client: %w", err)
		}
		err = core.InstallKitGKERuntime(assistants.KitGKECompositionConfig{KitLocalCompositionConfig: config, GKE: base.GKE, Provision: provision})
	}
	if err != nil {
		return fmt.Errorf("install Kit runtime: %w", err)
	}
	return nil
}

// kitGKEProvisionFromCLI validates production-only knobs before constructing a cluster client.
func kitGKEProvisionFromCLI(c *cli.Context) (assistants.KitGKEProvisionInput, error) {
	var out assistants.KitGKEProvisionInput
	if c.String("assistant-kit-network") != "" || c.String("assistant-runtime-local-ca-file") != "" || c.Bool("assistant-kit-allow-insecure-inference") {
		return out, fmt.Errorf("Kit GKE does not permit local network, CA, or insecure inference options")
	}
	out.IngressCIDR = c.String("assistant-kit-gke-ingress-cidr")
	if _, _, err := net.ParseCIDR(out.IngressCIDR); err != nil {
		return out, fmt.Errorf("assistant-kit-gke-ingress-cidr must be an explicit CIDR")
	}
	quantities := make(map[string]resource.Quantity)
	for _, name := range []string{"cpu-request", "cpu-limit", "memory-request", "memory-limit", "work-size-limit", "tmp-size-limit"} {
		quantity, err := resource.ParseQuantity(c.String("assistant-kit-gke-" + name))
		if err != nil || quantity.Sign() <= 0 {
			return out, fmt.Errorf("assistant-kit-gke-%s must be a positive Kubernetes quantity", name)
		}
		quantities[name] = quantity
	}
	for _, name := range []string{"cpu", "memory"} {
		request, limit := quantities[name+"-request"], quantities[name+"-limit"]
		if request.Cmp(limit) > 0 {
			return out, fmt.Errorf("assistant-kit-gke-%s-request must not exceed its limit", name)
		}
	}
	//nolint:exhaustive // Kit pod budgets intentionally specify CPU and memory only.
	out.Resources = corev1.ResourceRequirements{
		Requests: corev1.ResourceList{corev1.ResourceCPU: quantities["cpu-request"], corev1.ResourceMemory: quantities["memory-request"]},
		Limits:   corev1.ResourceList{corev1.ResourceCPU: quantities["cpu-limit"], corev1.ResourceMemory: quantities["memory-limit"]},
	}
	out.WorkSizeLimit = c.String("assistant-kit-gke-work-size-limit")
	out.TmpSizeLimit = c.String("assistant-kit-gke-tmp-size-limit")
	out.SnapshotsEnabled = c.Bool("assistant-kit-gke-snapshots-enabled")
	return out, nil
}

// kitWorkloadClientCredentials runs for every runtime startup. Only the public
// JWKS reaches persisted workload registration; the private key is bootstrap-only.
func kitWorkloadClientCredentials(method string) (mcp.AssistantRuntimeWorkloadClientConfig, []byte, error) {
	c := mcp.AssistantRuntimeWorkloadClientConfig{AuthMethod: method, UserSessionIssuerID: uuid.Nil, TriggerID: uuid.Nil, Secret: "", JWKS: nil, JWKSURI: ""}
	switch method {
	case "client_secret_basic":
		return c, nil, nil
	case "private_key_jwt":
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return c, nil, fmt.Errorf("generate Kit workload key: %w", err)
		}
		der, err := x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			return c, nil, fmt.Errorf("encode Kit workload key: %w", err)
		}
		c.JWKS, err = json.Marshal(map[string]any{"keys": []any{map[string]string{
			"kty": "EC", "crv": "P-256", "use": "sig", "alg": "ES256",
			"x": base64.RawURLEncoding.EncodeToString(key.X.FillBytes(make([]byte, 32))),
			"y": base64.RawURLEncoding.EncodeToString(key.Y.FillBytes(make([]byte, 32))),
		}}})
		if err != nil {
			return c, nil, fmt.Errorf("encode Kit workload public key: %w", err)
		}
		return c, pem.EncodeToMemory(&pem.Block{Headers: nil, Type: "PRIVATE KEY", Bytes: der}), nil
	default:
		return c, nil, fmt.Errorf("invalid assistant-kit-mcp-auth-method")
	}
}
