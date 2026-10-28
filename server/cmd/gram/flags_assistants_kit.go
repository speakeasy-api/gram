package gram

import (
	"encoding/base64"
	"fmt"
	"net"
	"net/url"
	"regexp"

	"github.com/distribution/reference"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/speakeasy-api/gram/server/internal/assistants"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/k8s"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/urfave/cli/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func assistantKitRuntimeFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{Name: "assistant-kit-workload-jwks-url", EnvVars: []string{"GRAM_ASSISTANT_KIT_WORKLOAD_JWKS_URL"}, Usage: "External issuer JWKS URL for local assertions; GKE Kit uses fixed Google JWKS."},
		&cli.StringFlag{Name: "assistant-kit-workload-issuer", EnvVars: []string{"GRAM_ASSISTANT_KIT_WORKLOAD_ISSUER"}, Usage: "External issuer for local assertions; GKE Kit uses fixed Google token verification."},
		&cli.StringFlag{Name: "assistant-kit-workload-subject", EnvVars: []string{"GRAM_ASSISTANT_KIT_WORKLOAD_SUBJECT"}, Usage: "Local operator-projected subject, unique to one cold bootstrap. Supply a fresh external subject and assertion before restarting; subjects cannot be reused or shared between assistants."},
		&cli.StringFlag{Name: "assistant-kit-workload-assertion-file", EnvVars: []string{"GRAM_ASSISTANT_KIT_WORKLOAD_ASSERTION_FILE"}, Usage: "Absolute operator-projected file named assertion in a dedicated directory. Its parent is mounted read-only outside persistent HOME to support atomic rotation."},
		&cli.StringFlag{Name: "assistant-kit-gke-workload-pool-project", EnvVars: []string{"GRAM_ASSISTANT_KIT_GKE_WORKLOAD_POOL_PROJECT"}, Usage: "Google Cloud project ID owning the GKE cluster workload pool, independently of the IAM project."},
		&cli.StringFlag{Name: "assistant-kit-gke-iam-project", EnvVars: []string{"GRAM_ASSISTANT_KIT_GKE_IAM_PROJECT"}, Usage: "Google Cloud project for generation-scoped managed workload service accounts."},
		&cli.StringFlag{Name: "assistant-kit-gke-metadata-image", EnvVars: []string{"GRAM_ASSISTANT_KIT_GKE_METADATA_IMAGE"}, Usage: "Digest-pinned assistant runtime image containing the metadata assertion sidecar."},
		&cli.StringFlag{Name: "assistant-kit-gke-ingress-cidr", EnvVars: []string{"GRAM_ASSISTANT_KIT_GKE_INGRESS_CIDR"}, Value: "", Usage: "Server source CIDR permitted to connect to Kit gateway pods."},
		&cli.StringFlag{Name: "assistant-kit-gke-cpu-request", EnvVars: []string{"GRAM_ASSISTANT_KIT_GKE_CPU_REQUEST"}, Value: "100m", Usage: "Kit pod CPU request."},
		&cli.StringFlag{Name: "assistant-kit-gke-cpu-limit", EnvVars: []string{"GRAM_ASSISTANT_KIT_GKE_CPU_LIMIT"}, Value: "1", Usage: "Kit pod CPU limit."},
		&cli.StringFlag{Name: "assistant-kit-gke-memory-request", EnvVars: []string{"GRAM_ASSISTANT_KIT_GKE_MEMORY_REQUEST"}, Value: "128Mi", Usage: "Kit pod memory request."},
		&cli.StringFlag{Name: "assistant-kit-gke-memory-limit", EnvVars: []string{"GRAM_ASSISTANT_KIT_GKE_MEMORY_LIMIT"}, Value: "1Gi", Usage: "Kit pod memory limit."},
		&cli.StringFlag{Name: "assistant-kit-gke-work-size-limit", EnvVars: []string{"GRAM_ASSISTANT_KIT_GKE_WORK_SIZE_LIMIT"}, Value: "1Gi", Usage: "Kit workspace volume size limit."},
		&cli.StringFlag{Name: "assistant-kit-gke-tmp-size-limit", EnvVars: []string{"GRAM_ASSISTANT_KIT_GKE_TMP_SIZE_LIMIT"}, Value: "128Mi", Usage: "Kit temporary volume size limit."},
		&cli.BoolFlag{Name: "assistant-kit-gke-snapshots-enabled", EnvVars: []string{"GRAM_ASSISTANT_KIT_GKE_SNAPSHOTS_ENABLED"}, Usage: "Enable Kit GKE snapshot provisioning for new sessions."},
		&cli.StringFlag{Name: "assistant-kit-network", EnvVars: []string{"GRAM_ASSISTANT_KIT_NETWORK"}, Usage: "Isolated local container network with a trusted TLS relay for ai.speakeasy.com."},
		&cli.StringFlag{Name: "assistant-kit-image", EnvVars: []string{"GRAM_ASSISTANT_KIT_IMAGE"}, Usage: "Opt new assistant sessions into Kit v3 using an explicit non-latest tag or digest; empty preserves legacy composition."},
		&cli.StringFlag{Name: "assistant-kit-mcp-resource", EnvVars: []string{"GRAM_ASSISTANT_KIT_MCP_RESOURCE"}, Usage: "Exact HTTPS URL of the attached issuer-gated MCP resource to provision for Kit."},
		&cli.StringFlag{Name: "assistant-kit-mcp-issuer", EnvVars: []string{"GRAM_ASSISTANT_KIT_MCP_ISSUER"}, Usage: "Expected OAuth issuer advertised by the configured Kit MCP resource."},
		&cli.StringFlag{Name: "assistant-kit-mcp-issuer-id", EnvVars: []string{"GRAM_ASSISTANT_KIT_MCP_ISSUER_ID"}, Usage: "Persisted user-session issuer UUID for the configured Kit resource."},
	}
}

func installKitLocalRuntimeFromCLI(c *cli.Context, core *assistants.ServiceCore, db *pgxpool.Pool, auditLogger *audit.Logger, redisClient *redis.Client, serverURL *url.URL) error {
	if c.String("assistant-kit-image") == "" {
		return nil
	}
	base, err := assistantRuntimeConfigFromCLI(c, serverURL)
	if err != nil {
		return err
	}
	if base.Provider == assistants.RuntimeProviderLocal && c.String("environment") != "local" {
		return fmt.Errorf("local Kit requires the local environment")
	}
	var provision assistants.KitGKEProvisionInput
	var workloadPoolProject string
	if base.Provider == assistants.RuntimeProviderGKE {
		for _, name := range []string{"assistant-kit-workload-issuer", "assistant-kit-workload-jwks-url", "assistant-kit-workload-subject", "assistant-kit-workload-assertion-file"} {
			if c.String(name) != "" {
				return fmt.Errorf("%s is local-only", name)
			}
		}
		workloadPoolProject, err = kitGKEWorkloadPoolProject(c.String("assistant-kit-gke-workload-pool-project"))
		if err != nil {
			return err
		}
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
	var issuerID uuid.UUID
	if raw := c.String("assistant-kit-mcp-issuer-id"); raw != "" {
		issuerID, err = uuid.Parse(raw)
	}
	if err != nil {
		return fmt.Errorf("assistant-kit-mcp-issuer-id must identify the configured resource's issuer")
	}

	if serverURL == nil {
		return fmt.Errorf("kit requires the deployment server URL")
	}
	authenticationHost, err := mcp.NewAuthenticationHost(c.String("authentication-host-url"), serverURL, c.String("environment"))
	if err != nil {
		return fmt.Errorf("configure Kit authentication host: %w", err)
	}
	config := assistants.KitLocalCompositionConfig{
		PersistedMCP: base.Provider != assistants.RuntimeProviderLocal,
		Environment:  c.String("environment"), Image: c.String("assistant-kit-image"), ExtraCACertFile: c.String("assistant-runtime-local-ca-file"),
		Origin: &url.URL{Scheme: "https", Host: "ai.speakeasy.com"}, Network: c.String("assistant-kit-network"), AllowInsecureInference: false,
		MCPResource: resource, MCPIssuer: issuer, MCPUserSessionIssuerID: issuerID, TurnStream: chat.NewTurnStream(redisClient),
		WorkloadJWKSURL:       c.String("assistant-kit-workload-jwks-url"),
		WorkloadIssuerURL:     c.String("assistant-kit-workload-issuer"),
		WorkloadSubject:       c.String("assistant-kit-workload-subject"),
		WorkloadAssertionFile: c.String("assistant-kit-workload-assertion-file"),
		WorkloadNamespace:     base.GKE.Namespace,
		WorkloadIssuerBaseURL: authenticationHost.BaseURL(),
	}

	if base.Provider == assistants.RuntimeProviderLocal {
		err = core.InstallKitLocalRuntime(config)
	} else {
		caCert, decodeErr := base64.StdEncoding.DecodeString(c.String("assistant-runtime-gke-cluster-ca"))
		if decodeErr != nil {
			return fmt.Errorf("decode --assistant-runtime-gke-cluster-ca: %w", decodeErr)
		}
		if len(caCert) == 0 || c.String("assistant-runtime-gke-cluster-endpoint") == "" {
			return fmt.Errorf("kit GKE requires assistant runtime cluster endpoint and CA")
		}
		base.GKE.Dynamic, err = k8s.NewRemoteDynamicClient(c.Context, c.String("assistant-runtime-gke-cluster-endpoint"), caCert, k8s.RemoteClientOptions{QPS: 0, Burst: 0})
		if err != nil {
			return fmt.Errorf("build Kit assistant cluster client: %w", err)
		}
		err = core.InstallKitGKERuntime(assistants.KitGKECompositionConfig{KitLocalCompositionConfig: config, GKE: base.GKE, Provision: provision, Snapshots: nil, IAMProject: c.String("assistant-kit-gke-iam-project"), WorkloadPoolProject: workloadPoolProject})
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
		return out, fmt.Errorf("kit GKE does not permit local network, CA, or insecure inference options")
	}
	if !regexp.MustCompile(`^[a-z][a-z0-9-]{4,28}[a-z0-9]$`).MatchString(c.String("assistant-kit-gke-iam-project")) {
		return out, fmt.Errorf("assistant-kit-gke-iam-project must be an explicit Google Cloud project ID")
	}
	out.MetadataImage = c.String("assistant-kit-gke-metadata-image")
	named, imageErr := reference.ParseNormalizedNamed(out.MetadataImage)
	_, digested := named.(reference.Digested)
	if imageErr != nil || !digested {
		return out, fmt.Errorf("assistant-kit-gke-metadata-image must be a digest-pinned assistant runtime image")
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
		Claims:   nil,
		Requests: corev1.ResourceList{corev1.ResourceCPU: quantities["cpu-request"], corev1.ResourceMemory: quantities["memory-request"]},
		Limits:   corev1.ResourceList{corev1.ResourceCPU: quantities["cpu-limit"], corev1.ResourceMemory: quantities["memory-limit"]},
	}
	out.WorkSizeLimit = c.String("assistant-kit-gke-work-size-limit")
	out.TmpSizeLimit = c.String("assistant-kit-gke-tmp-size-limit")
	out.SnapshotsEnabled = c.Bool("assistant-kit-gke-snapshots-enabled")
	return out, nil
}

func kitGKEWorkloadPoolProject(project string) (string, error) {
	if !regexp.MustCompile(`^[a-z][a-z0-9-]{4,28}[a-z0-9]$`).MatchString(project) {
		return "", fmt.Errorf("assistant-kit-gke-workload-pool-project must be an explicit Google Cloud project ID")
	}
	return project, nil
}
