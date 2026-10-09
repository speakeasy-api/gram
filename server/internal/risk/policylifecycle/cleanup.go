package policylifecycle

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
	shadowadmission "github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	// OrphanRepairSeedName identifies the one-time startup repair.
	OrphanRepairSeedName = "orphaned-mcp-risk-policies"

	// OrphanRepairSeedVersion changes only when the repair semantics change.
	OrphanRepairSeedVersion = "2026-10-06-v1"

	// OrphanRepairActorDisplayName is the name the audit feed shows for the repair.
	OrphanRepairActorDisplayName = "Orphaned policy repair"
)

// Actor identifies who caused lifecycle cleanup.
type Actor struct {
	Principal   urn.Principal
	DisplayName *string
	Slug        *string
}

// Cleaner soft-deletes policies whose sole MCP target no longer exists.
type Cleaner struct {
	audit *audit.Logger
}

// NewCleaner creates a lifecycle cleaner.
func NewCleaner(auditLogger *audit.Logger) *Cleaner {
	return &Cleaner{audit: auditLogger}
}

// SoftDeleteForMCPServer tombstones policies owned exclusively by one server.
// The caller must invoke it after tombstoning the server in the same
// transaction, and must take shadowadmission.LockProject before locking the
// server row.
func (c *Cleaner) SoftDeleteForMCPServer(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	projectID uuid.UUID,
	mcpServerID uuid.UUID,
	actor Actor,
) ([]uuid.UUID, error) {
	if err := lockPolicyCleanup(ctx, tx, projectID); err != nil {
		return nil, err
	}

	policies, err := repo.New(tx).ListLifecycleBoundRiskPoliciesByMCPServer(ctx, repo.ListLifecycleBoundRiskPoliciesByMCPServerParams{
		ProjectID:   projectID,
		McpServerID: mcpServerID.String(),
	})
	if err != nil {
		return nil, fmt.Errorf("list lifecycle-bound risk policies: %w", err)
	}

	return c.softDeletePolicies(ctx, tx, organizationID, projectID, policies, actor)
}

// RepairOrphans tombstones lifecycle-bound policies left by server deletions
// that predate transactional cleanup. It is idempotent and commits per project.
func (c *Cleaner) RepairOrphans(ctx context.Context, db *pgxpool.Pool) ([]uuid.UUID, error) {
	projectIDs, err := repo.New(db).ListProjectIDsWithOrphanedLifecycleBoundRiskPolicies(ctx)
	if err != nil {
		return nil, fmt.Errorf("list projects with orphaned risk policies: %w", err)
	}

	var deleted []uuid.UUID
	for _, projectID := range projectIDs {
		projectDeleted, err := c.repairProject(ctx, db, projectID)
		if err != nil {
			return nil, err
		}
		deleted = append(deleted, projectDeleted...)
	}

	return deleted, nil
}

func (c *Cleaner) repairProject(ctx context.Context, db *pgxpool.Pool, projectID uuid.UUID) ([]uuid.UUID, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin orphaned risk policy repair: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	if err := lockPolicyCleanup(ctx, tx, projectID); err != nil {
		return nil, err
	}

	policies, err := repo.New(tx).ListOrphanedLifecycleBoundRiskPoliciesByProject(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list orphaned risk policies: %w", err)
	}
	if len(policies) == 0 {
		return nil, nil
	}

	deleted, err := c.softDeletePolicies(
		ctx,
		tx,
		policies[0].OrganizationID,
		projectID,
		policies,
		Actor{
			Principal:   urn.NewSystemPrincipal(OrphanRepairSeedName),
			DisplayName: conv.PtrEmpty(OrphanRepairActorDisplayName),
			Slug:        nil,
		},
	)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit orphaned risk policy repair: %w", err)
	}
	return deleted, nil
}

// lockPolicyCleanup takes the project locks every risk policy writer takes,
// in the shared order: Shadow MCP admission, then policy mutations, then
// exclusion mutations. A server delete must take the admission lock before it
// locks the server row, because UpdateMcpServer takes them in that order.
// Taking the advisory lock again in the same transaction does not block.
func lockPolicyCleanup(ctx context.Context, tx pgx.Tx, projectID uuid.UUID) error {
	if err := shadowadmission.LockProject(ctx, tx, projectID); err != nil {
		return fmt.Errorf("lock shadow MCP admission: %w", err)
	}
	queries := repo.New(tx)
	if err := queries.LockRiskPolicyMutations(ctx, projectID.String()); err != nil {
		return fmt.Errorf("lock risk policy mutations: %w", err)
	}
	if err := queries.LockRiskExclusionMutations(ctx, projectID.String()); err != nil {
		return fmt.Errorf("lock risk exclusion mutations: %w", err)
	}
	return nil
}

func (c *Cleaner) softDeletePolicies(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	projectID uuid.UUID,
	policies []repo.RiskPolicy,
	actor Actor,
) ([]uuid.UUID, error) {
	queries := repo.New(tx)
	deleted := make([]uuid.UUID, 0, len(policies))
	for _, policy := range policies {
		if err := queries.DeleteRiskPolicy(ctx, repo.DeleteRiskPolicyParams{
			ID:        policy.ID,
			ProjectID: projectID,
		}); err != nil {
			return nil, fmt.Errorf("soft-delete risk policy: %w", err)
		}
		if err := queries.DeleteRiskPolicyBypassRequestsByPolicy(ctx, repo.DeleteRiskPolicyBypassRequestsByPolicyParams{
			RiskPolicyID: policy.ID,
			ProjectID:    projectID,
		}); err != nil {
			return nil, fmt.Errorf("soft-delete risk policy bypass requests: %w", err)
		}
		if _, err := queries.DeleteRiskExclusionsByPolicy(ctx, repo.DeleteRiskExclusionsByPolicyParams{
			RiskPolicyID: uuid.NullUUID{UUID: policy.ID, Valid: true},
			ProjectID:    projectID,
		}); err != nil {
			return nil, fmt.Errorf("delete risk policy exclusions: %w", err)
		}
		if err := authz.ReplaceGrantAudience(ctx, tx, authz.ResourceGrant{
			Resource: authz.Resource{
				OrganizationID: organizationID,
				Scope:          authz.ScopeRiskPolicyEvaluate,
				ResourceID:     policy.ID.String(),
			},
			Principals: nil,
			Selector:   authz.NewSelector(authz.ScopeRiskPolicyEvaluate, policy.ID.String()),
		}); err != nil {
			return nil, fmt.Errorf("clear risk policy audience: %w", err)
		}
		if err := c.audit.LogRiskPolicyDelete(ctx, tx, audit.LogRiskPolicyDeleteEvent{
			OrganizationID:   organizationID,
			ProjectID:        projectID,
			Actor:            actor.Principal,
			ActorDisplayName: actor.DisplayName,
			ActorSlug:        actor.Slug,
			RiskPolicyID:     policy.ID,
			RiskPolicyName:   policy.Name,
		}); err != nil {
			return nil, fmt.Errorf("log risk policy deletion: %w", err)
		}
		deleted = append(deleted, policy.ID)
	}

	return deleted, nil
}
