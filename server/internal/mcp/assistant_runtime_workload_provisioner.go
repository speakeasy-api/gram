package mcp

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/audit"
)

// AssistantRuntimeWorkloadProvisioner is the trusted worker-only subset of MCP
// composition. It has no HTTP handlers and requires no initiating-user context.
// Runtime and workload authority are read from the same durable registrations
// used by the full MCP service; this is not another identity registry.
type AssistantRuntimeWorkloadProvisioner struct{ service *Service }

func NewAssistantRuntimeWorkloadProvisioner(db *pgxpool.Pool, auditLogger *audit.Logger) *AssistantRuntimeWorkloadProvisioner {
	return &AssistantRuntimeWorkloadProvisioner{service: &Service{db: db, auditLogger: auditLogger}}
}

func (p *AssistantRuntimeWorkloadProvisioner) Provision(ctx context.Context, runtimeID uuid.UUID, c AssistantRuntimeWorkloadClientConfig) (ProvisionedAssistantWorkloadClient, error) {
	return p.service.ProvisionAssistantRuntimeWorkloadClient(ctx, runtimeID, c)
}
