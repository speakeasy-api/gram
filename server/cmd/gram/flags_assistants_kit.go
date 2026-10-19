package gram

import (
	"context"
	"fmt"
	"net/url"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/speakeasy-api/gram/server/internal/assistants"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/urfave/cli/v2"
)

func assistantKitRuntimeFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{Name: "assistant-kit-image", EnvVars: []string{"GRAM_ASSISTANT_KIT_IMAGE"}, Usage: "Opt in to local Kit v3 using this exact tagged image; empty leaves legacy runtime composition unchanged."},
		&cli.StringFlag{Name: "assistant-kit-mcp-resource", EnvVars: []string{"GRAM_ASSISTANT_KIT_MCP_RESOURCE"}, Usage: "Exact HTTPS URL of the attached issuer-gated MCP resource to provision for local Kit."},
		&cli.StringFlag{Name: "assistant-kit-mcp-issuer", EnvVars: []string{"GRAM_ASSISTANT_KIT_MCP_ISSUER"}, Usage: "Expected OAuth issuer advertised by the configured Kit MCP resource."},
		&cli.StringFlag{Name: "assistant-kit-mcp-issuer-id", EnvVars: []string{"GRAM_ASSISTANT_KIT_MCP_ISSUER_ID"}, Usage: "Persisted user-session issuer UUID for the configured local Kit resource."},
		&cli.StringFlag{Name: "assistant-kit-root-trigger-id", EnvVars: []string{"GRAM_ASSISTANT_KIT_ROOT_TRIGGER_ID"}, Usage: "Optional existing root trigger UUID; required when the assistant has multiple roots."},
		&cli.BoolFlag{Name: "assistant-kit-allow-insecure-inference", EnvVars: []string{"GRAM_ASSISTANT_KIT_ALLOW_INSECURE_INFERENCE"}, Usage: "Explicit local-only HTTP inference opt-in. MCP machine authentication still requires HTTPS."},
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
	if base.Provider != assistants.RuntimeProviderLocal || c.String("environment") != "local" {
		return fmt.Errorf("assistant-kit-image currently requires the local runtime provider and local environment")
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
	provisioner := mcp.NewAssistantRuntimeWorkloadProvisioner(db, auditLogger)
	return core.InstallKitLocalRuntime(assistants.KitLocalCompositionConfig{
		Environment: c.String("environment"), Image: c.String("assistant-kit-image"), ExtraCACertFile: c.String("assistant-runtime-local-ca-file"),
		Origin: base.Local.ServerURL, AllowInsecureInference: c.Bool("assistant-kit-allow-insecure-inference"),
		MCPResource: resource, MCPIssuer: issuer, MCPUserSessionIssuerID: issuerID, RootTriggerID: root, TurnStream: chat.NewTurnStream(redisClient),
		ProvisionMCP: func(ctx context.Context, r assistants.KitMCPProvisionRequest) (assistants.KitMCPProvisionResult, error) {
			var zero assistants.KitMCPProvisionResult
			// Lifecycle admission already holds the shared assistant fence. Do not
			// acquire it again on another connection while rotating startup credentials.
			tx, err := db.Begin(ctx)
			if err != nil {
				return zero, err
			}
			defer func() { _ = tx.Rollback(ctx) }()
			if _, err := mcp.RevokeAssistantRuntimeWorkloadClients(ctx, tx, r.OrganizationID, r.ProjectID, r.RuntimeID); err != nil {
				return zero, err
			}
			if err := tx.Commit(ctx); err != nil {
				return zero, err
			}
			client, err := provisioner.Provision(ctx, r.RuntimeID, mcp.AssistantRuntimeWorkloadClientConfig{UserSessionIssuerID: r.UserSessionIssuerID, TriggerID: r.TriggerID, AuthMethod: "client_secret_basic"})
			if err != nil {
				return zero, err
			}
			if client.RuntimeID != r.RuntimeID || client.TriggerID != r.TriggerID {
				return zero, fmt.Errorf("Kit workload registration returned a mismatched runtime binding")
			}
			return assistants.KitMCPProvisionResult{ClientID: client.ClientID, Secret: client.Secret}, nil
		},
	})
}
